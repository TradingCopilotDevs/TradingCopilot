package tracing

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestSetupDisabledConfiguresTraceContextPropagator(t *testing.T) {
	shutdown, err := Setup(context.Background(), config.Settings{OTELTracesExporter: "none"}, "TradingCopilot/test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	ctx := otel.GetTextMapPropagator().Extract(context.Background(), propagation.MapCarrier{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	})
	spanCtx := oteltrace.SpanContextFromContext(ctx)
	if !spanCtx.IsValid() || spanCtx.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace context not extracted: %s valid=%v", spanCtx.TraceID(), spanCtx.IsValid())
	}
}

func TestSamplerFromSettingsValidatesRatio(t *testing.T) {
	if _, err := samplerFromSettings(config.Settings{OTELTracesSampler: "traceidratio", OTELTracesSamplerArg: "0.25"}); err != nil {
		t.Fatal(err)
	}
	if _, err := samplerFromSettings(config.Settings{OTELTracesSampler: "traceidratio", OTELTracesSamplerArg: "1.25"}); err == nil {
		t.Fatal("expected sampler ratio above 1 to fail")
	}
	if _, err := samplerFromSettings(config.Settings{OTELTracesSampler: "unsupported"}); err == nil {
		t.Fatal("expected unsupported sampler to fail")
	}
}

func TestLoadOTELSettings(t *testing.T) {
	t.Setenv("TC_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	t.Setenv("OTEL_SERVICE_NAME", "TradingCopilot/custom")
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4318/v1/traces")
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	t.Setenv("OTEL_TRACES_SAMPLER", "traceidratio")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.5")

	settings := config.Load()
	if settings.OTELServiceName != "TradingCopilot/custom" ||
		settings.OTELTracesExporter != "otlp" ||
		settings.OTELExporterOTLPEndpoint != "http://otel-collector:4318/v1/traces" ||
		settings.OTELExporterOTLPProtocol != "http/protobuf" ||
		settings.OTELTracesSampler != "traceidratio" ||
		settings.OTELTracesSamplerArg != "0.5" {
		t.Fatalf("OTEL settings not loaded: %+v", settings)
	}
}
