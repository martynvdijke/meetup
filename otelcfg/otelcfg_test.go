package otelcfg

import "testing"

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT",
		"OTEL_SERVICE_NAME",
		"OTEL_EXPORTER_OTLP_HEADERS",
	} {
		t.Setenv(k, "")
	}
}

func TestResolveDefaults(t *testing.T) {
	clearEnv(t)
	c := Resolve(Stored{})
	if c.Enabled() {
		t.Fatal("no endpoint should be disabled")
	}
	if c.ServiceName != DefaultServiceName {
		t.Fatalf("service name %q", c.ServiceName)
	}
	for name, src := range map[string]string{
		"endpoint": c.EndpointSource, "service": c.ServiceNameSource, "headers": c.HeadersSource,
	} {
		if src != "default" {
			t.Fatalf("%s source %q", name, src)
		}
	}
}

func TestResolveStored(t *testing.T) {
	clearEnv(t)
	c := Resolve(Stored{Endpoint: "http://collector:4318", ServiceName: "meetup-db", Headers: "a=b"})
	if !c.Enabled() || c.Endpoint != "http://collector:4318" || c.ServiceName != "meetup-db" || c.Headers != "a=b" {
		t.Fatalf("config %+v", c)
	}
	for name, src := range map[string]string{
		"endpoint": c.EndpointSource, "service": c.ServiceNameSource, "headers": c.HeadersSource,
	} {
		if src != "db" {
			t.Fatalf("%s source %q", name, src)
		}
	}
}

func TestResolveEnvWins(t *testing.T) {
	clearEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://env:4318")
	t.Setenv("OTEL_SERVICE_NAME", "env-name")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "x=y")
	c := Resolve(Stored{Endpoint: "http://db:4318", ServiceName: "db-name", Headers: "db=1"})
	if c.Endpoint != "http://env:4318" || c.ServiceName != "env-name" || c.Headers != "x=y" {
		t.Fatalf("env should win: %+v", c)
	}
	for name, src := range map[string]string{
		"endpoint": c.EndpointSource, "service": c.ServiceNameSource, "headers": c.HeadersSource,
	} {
		if src != "env" {
			t.Fatalf("%s source %q", name, src)
		}
	}
}

func TestResolveSignalSpecificEndpoint(t *testing.T) {
	clearEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://traces:4318")
	c := Resolve(Stored{Endpoint: "http://db:4318"})
	if c.Endpoint != "http://traces:4318" || c.EndpointSource != "env" {
		t.Fatalf("signal endpoint %+v", c)
	}
}

func TestRestartRequired(t *testing.T) {
	clearEnv(t)
	stored := Stored{Endpoint: "http://db:4318", ServiceName: "meetup-db"}
	applied := Resolve(stored)
	if RestartRequired(stored, applied) {
		t.Fatal("same config should not require a restart")
	}
	changed := Stored{Endpoint: "http://other:4318", ServiceName: "meetup-db"}
	if !RestartRequired(changed, applied) {
		t.Fatal("changed endpoint should require a restart")
	}
	// Environment overrides mean stored changes do not affect the outcome.
	clearEnv(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://env:4318")
	applied = Resolve(stored)
	if RestartRequired(changed, applied) {
		t.Fatal("env-overridden endpoint change should not require a restart")
	}
}
