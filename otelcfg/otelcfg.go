// Package otelcfg resolves the OpenTelemetry configuration from the standard
// OTEL_* environment variables and the admin-managed database settings.
//
// Environment variables always win. Stored settings are read once at startup,
// so saving new values in the admin UI requires a restart to take effect.
package otelcfg

import "os"

// DefaultServiceName is used when neither the environment nor the stored
// settings provide a service name.
const DefaultServiceName = "meetup"

// Stored is the OTel configuration persisted through the admin UI.
type Stored struct {
	Endpoint    string
	ServiceName string
	Headers     string
}

// Config is a fully resolved OTel configuration. The Source fields report
// where each value came from: "env", "db" or "default".
type Config struct {
	Endpoint    string
	ServiceName string
	Headers     string

	EndpointSource    string
	ServiceNameSource string
	HeadersSource     string
}

// Enabled reports whether telemetry export is configured.
func (c Config) Enabled() bool { return c.Endpoint != "" }

var endpointEnvKeys = []string{
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
}

// EnvEndpoint returns the first configured OTLP endpoint environment variable.
func EnvEndpoint() string {
	for _, k := range endpointEnvKeys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return ""
}

// EnvServiceName returns OTEL_SERVICE_NAME when set.
func EnvServiceName() string { return os.Getenv("OTEL_SERVICE_NAME") }

// EnvHeaders returns OTEL_EXPORTER_OTLP_HEADERS when set.
func EnvHeaders() string { return os.Getenv("OTEL_EXPORTER_OTLP_HEADERS") }

// Resolve combines stored settings with the environment. Environment values
// win over stored values, which win over the defaults.
func Resolve(stored Stored) Config {
	c := Config{
		ServiceName:       DefaultServiceName,
		EndpointSource:    "default",
		ServiceNameSource: "default",
		HeadersSource:     "default",
	}
	if stored.Endpoint != "" {
		c.Endpoint = stored.Endpoint
		c.EndpointSource = "db"
	}
	if stored.ServiceName != "" {
		c.ServiceName = stored.ServiceName
		c.ServiceNameSource = "db"
	}
	if stored.Headers != "" {
		c.Headers = stored.Headers
		c.HeadersSource = "db"
	}
	if v := EnvEndpoint(); v != "" {
		c.Endpoint = v
		c.EndpointSource = "env"
	}
	if v := EnvServiceName(); v != "" {
		c.ServiceName = v
		c.ServiceNameSource = "env"
	}
	if v := EnvHeaders(); v != "" {
		c.Headers = v
		c.HeadersSource = "env"
	}
	return c
}

var applied Config

// MarkApplied records the configuration the running process started with.
func MarkApplied(c Config) { applied = c }

// Applied returns the configuration the running process started with. It is
// the zero value until setup has run.
func Applied() Config { return applied }

// RestartRequired reports whether resolving the stored settings would change
// the configuration compared to the one the process is running with.
func RestartRequired(stored Stored, appliedCfg Config) bool {
	return Resolve(stored) != appliedCfg
}
