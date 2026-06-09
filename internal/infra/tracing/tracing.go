package tracing

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const (
	defaultExporterTimeout = 5 * time.Second
)

func Setup(ctx context.Context, settings config.Settings, defaultServiceName string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	exporterName := strings.ToLower(strings.TrimSpace(settings.OTELTracesExporter))
	if exporterName == "" || exporterName == "none" || exporterName == "noop" {
		return func(context.Context) error { return nil }, nil
	}
	if exporterName != "otlp" {
		return nil, fmt.Errorf("unsupported OTEL_TRACES_EXPORTER %q", settings.OTELTracesExporter)
	}
	sampler, err := samplerFromSettings(settings)
	if err != nil {
		return nil, err
	}
	exporter, err := newOTLPExporter(ctx, settings)
	if err != nil {
		return nil, err
	}
	serviceName := strings.TrimSpace(settings.OTELServiceName)
	if serviceName == "" {
		serviceName = strings.TrimSpace(defaultServiceName)
	}
	if serviceName == "" {
		serviceName = firstNonEmpty(settings.AppName, "TradingCopilot")
	}
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes("",
		attribute.String("service.name", serviceName),
		attribute.String("deployment.environment", strings.TrimSpace(settings.AppEnv)),
	))
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

func newOTLPExporter(ctx context.Context, settings config.Settings) (sdktrace.SpanExporter, error) {
	protocol := strings.ToLower(strings.TrimSpace(settings.OTELExporterOTLPProtocol))
	if protocol == "" {
		protocol = "grpc"
	}
	endpoint := strings.TrimSpace(settings.OTELExporterOTLPEndpoint)
	ctx, cancel := context.WithTimeout(ctx, defaultExporterTimeout)
	defer cancel()
	switch protocol {
	case "grpc":
		options := []otlptracegrpc.Option{otlptracegrpc.WithTimeout(defaultExporterTimeout)}
		if endpoint != "" {
			options = append(options, otlptracegrpc.WithEndpointURL(endpoint))
		}
		return otlptracegrpc.New(ctx, options...)
	case "http", "http/protobuf":
		options := []otlptracehttp.Option{otlptracehttp.WithTimeout(defaultExporterTimeout)}
		if endpoint != "" {
			options = append(options, otlptracehttp.WithEndpointURL(endpoint))
		}
		return otlptracehttp.New(ctx, options...)
	default:
		return nil, fmt.Errorf("unsupported OTEL_EXPORTER_OTLP_PROTOCOL %q", settings.OTELExporterOTLPProtocol)
	}
}

func samplerFromSettings(settings config.Settings) (sdktrace.Sampler, error) {
	name := strings.ToLower(strings.TrimSpace(settings.OTELTracesSampler))
	if name == "" {
		name = "always_on"
	}
	switch name {
	case "always_on", "alwayson", "on":
		return sdktrace.AlwaysSample(), nil
	case "always_off", "alwaysoff", "off":
		return sdktrace.NeverSample(), nil
	case "traceidratio", "trace_id_ratio", "traceidratiobased":
		ratio, err := samplerRatio(settings.OTELTracesSamplerArg)
		if err != nil {
			return nil, err
		}
		return sdktrace.TraceIDRatioBased(ratio), nil
	case "parentbased_traceidratio", "parent_based_traceidratio", "parentbased_trace_id_ratio":
		ratio, err := samplerRatio(settings.OTELTracesSamplerArg)
		if err != nil {
			return nil, err
		}
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio)), nil
	case "parentbased_always_on", "parent_based_always_on":
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil
	case "parentbased_always_off", "parent_based_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample()), nil
	default:
		return nil, fmt.Errorf("unsupported OTEL_TRACES_SAMPLER %q", settings.OTELTracesSampler)
	}
}

func samplerRatio(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 1, nil
	}
	ratio, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid OTEL_TRACES_SAMPLER_ARG %q", value)
	}
	if ratio < 0 || ratio > 1 {
		return 0, errors.New("OTEL_TRACES_SAMPLER_ARG must be between 0 and 1")
	}
	return ratio, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
