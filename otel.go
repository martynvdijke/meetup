package main

// Package-level OpenTelemetry wiring for the meetup HTTP server.
//
// Telemetry is opt-in. The OTLP endpoint, service name and headers come from
// the standard OTEL_* environment variables, falling back to the settings
// stored by the admin UI in the database. With no endpoint configured the app
// keeps the global no-op providers and no exporter is created, so there is no
// overhead and no connection spam to a non-existent collector.
//
// Supported via the OTLP/HTTP exporters' built-in env handling:
//
//	OTEL_EXPORTER_OTLP_ENDPOINT           e.g. http://collector:4318
//	OTEL_EXPORTER_OTLP_{TRACES,METRICS,LOGS}_ENDPOINT
//	OTEL_EXPORTER_OTLP_HEADERS            e.g. "Authorization=Bearer%20..."
//	OTEL_EXPORTER_OTLP_INSECURE / _CERTIFICATE / _COMPRESSION / _TIMEOUT
//	OTEL_SERVICE_NAME, OTEL_RESOURCE_ATTRIBUTES
//	OTEL_TRACES_SAMPLER, OTEL_TRACES_SAMPLER_ARG
import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"meetup/db"
	"meetup/otelcfg"
)

// otelConfig resolves the effective configuration: environment variables
// first, then the admin-managed database settings, then defaults.
func otelConfig() otelcfg.Config {
	stored := otelcfg.Stored{}
	if s, err := db.GetOTelSettings(); err != nil {
		log.Printf("OpenTelemetry: could not read stored settings: %v", err)
	} else {
		stored = otelcfg.Stored{Endpoint: s.Endpoint, ServiceName: s.ServiceName, Headers: s.Headers}
	}
	return otelcfg.Resolve(stored)
}

func otelServiceName() string {
	if c := otelcfg.Applied(); c.ServiceName != "" {
		return c.ServiceName
	}
	return otelcfg.DefaultServiceName
}

// setupOTel configures traces, metrics and logs against the OTLP/HTTP
// exporters and installs the global providers. The returned shutdown function
// flushes and stops every provider and is always safe to call. When no OTLP
// endpoint is configured no providers are installed and shutdown is a no-op.
func setupOTel(ctx context.Context) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	cfg := otelConfig()
	otelcfg.MarkApplied(cfg)

	// Hand database-only values to the OTLP exporters through the standard
	// environment variables. Environment-set values are left untouched so
	// signal-specific endpoints keep working as documented by the OTel SDK.
	if cfg.EndpointSource == "db" {
		_ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", cfg.Endpoint)
	}
	if cfg.HeadersSource == "db" {
		_ = os.Setenv("OTEL_EXPORTER_OTLP_HEADERS", cfg.Headers)
	}
	if cfg.ServiceNameSource == "db" {
		_ = os.Setenv("OTEL_SERVICE_NAME", cfg.ServiceName)
	}

	if !cfg.Enabled() {
		log.Printf("OpenTelemetry: no OTLP endpoint configured, telemetry disabled")
		return noop, nil
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(otelServiceName()),
			semconv.ServiceVersion(Version),
		),
	)
	if err != nil {
		return noop, fmt.Errorf("build resource: %w", err)
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	// ── Traces ──
	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		return noop, fmt.Errorf("trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// ── Metrics ──
	metricExp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return noop, fmt.Errorf("metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
	)
	otel.SetMeterProvider(mp)

	// Go runtime metrics (go.* / process.*) via the global meter provider.
	if err := runtime.Start(runtime.WithMinimumReadMemStatsInterval(15 * time.Second)); err != nil {
		log.Printf("OpenTelemetry: runtime metrics unavailable: %v", err)
	}

	// ── Logs ──
	logExp, err := otlploghttp.New(ctx)
	if err != nil {
		_ = mp.Shutdown(ctx)
		_ = tp.Shutdown(ctx)
		return noop, fmt.Errorf("log exporter: %w", err)
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
	)

	// Bridge the stdlib logger into OTLP while keeping the existing stderr
	// output: every log.Printf line also becomes an OTel log record.
	otelLog := slog.NewLogLogger(
		otelslog.NewHandler(otelServiceName(), otelslog.WithLoggerProvider(lp)),
		slog.LevelInfo,
	)
	log.SetOutput(io.MultiWriter(os.Stderr, otelLog.Writer()))

	log.Printf("OpenTelemetry: exporting to %s", cfg.Endpoint)

	return func(ctx context.Context) error {
		log.SetOutput(os.Stderr)
		return errors.Join(lp.Shutdown(ctx), mp.Shutdown(ctx), tp.Shutdown(ctx))
	}, nil
}
