package main

// OpenTelemetry wiring for the meetup HTTP server.
//
// Telemetry is opt-in and driven entirely by the standard OTEL_* environment
// variables: with no OTLP endpoint configured the app keeps the global no-op
// providers and no exporter is created, so there is no overhead and no
// connection spam to a non-existent collector.
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
)

// otelEnvKeys are the env vars that opt the app into OTLP export.
var otelEnvKeys = []string{
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
}

func otelEndpointConfigured() bool {
	for _, k := range otelEnvKeys {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

func otelEndpoint() string {
	for _, k := range otelEnvKeys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

func otelServiceName() string {
	return getEnv("OTEL_SERVICE_NAME", "meetup")
}

// setupOTel configures traces, metrics and logs against the OTLP/HTTP
// exporters and installs the global providers. The returned shutdown function
// flushes and stops every provider and is always safe to call. When no OTLP
// endpoint is configured no providers are installed and shutdown is a no-op.
func setupOTel(ctx context.Context) (func(context.Context) error, error) {
	noop := func(context.Context) error { return nil }
	if !otelEndpointConfigured() {
		log.Printf("OpenTelemetry: no OTLP endpoint set, telemetry disabled")
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

	log.Printf("OpenTelemetry: exporting to %s", otelEndpoint())

	return func(ctx context.Context) error {
		log.SetOutput(os.Stderr)
		return errors.Join(lp.Shutdown(ctx), mp.Shutdown(ctx), tp.Shutdown(ctx))
	}, nil
}
