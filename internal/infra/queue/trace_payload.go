package queue

import (
	"context"
	"encoding/json"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const (
	queueTracerName      = "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/queue"
	traceEnvelopeVersion = 1
)

type traceTaskEnvelope struct {
	TraceEnvelopeVersion int             `json:"traceEnvelopeVersion,omitempty"`
	TraceParent          string          `json:"traceparent,omitempty"`
	TraceState           string          `json:"tracestate,omitempty"`
	Payload              json.RawMessage `json:"payload,omitempty"`
}

func encodeTaskPayload(ctx context.Context, payload any) []byte {
	var raw []byte
	if payload != nil {
		raw, _ = json.Marshal(payload)
	}
	return encodeTaskPayloadBytes(ctx, raw)
}

func encodeTaskPayloadBytes(ctx context.Context, raw []byte) []byte {
	carrier := propagation.MapCarrier{}
	if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.IsValid() {
		otel.GetTextMapPropagator().Inject(ctx, carrier)
	}
	traceParent := strings.TrimSpace(carrier.Get("traceparent"))
	traceState := strings.TrimSpace(carrier.Get("tracestate"))
	if traceParent == "" && traceState == "" {
		return raw
	}
	envelope, err := json.Marshal(traceTaskEnvelope{
		TraceEnvelopeVersion: traceEnvelopeVersion,
		TraceParent:          traceParent,
		TraceState:           traceState,
		Payload:              raw,
	})
	if err != nil {
		return raw
	}
	return envelope
}

func beginTaskSpan(ctx context.Context, taskType string, raw []byte) (context.Context, []byte, trace.Span) {
	ctx, payload := extractTaskPayload(ctx, raw)
	ctx, span := otel.Tracer(queueTracerName).Start(ctx, "queue "+taskType,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "asynq"),
			attribute.String("messaging.operation.name", "process"),
			attribute.String("messaging.destination.name", taskType),
			attribute.String("messaging.message.type", taskType),
		),
	)
	return ctx, payload, span
}

func finishTaskSpan(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

func extractTaskPayload(ctx context.Context, raw []byte) (context.Context, []byte) {
	if len(raw) == 0 {
		return ctx, raw
	}
	var envelope traceTaskEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil || envelope.TraceEnvelopeVersion != traceEnvelopeVersion {
		return ctx, raw
	}
	carrier := propagation.MapCarrier{}
	if strings.TrimSpace(envelope.TraceParent) != "" {
		carrier.Set("traceparent", envelope.TraceParent)
	}
	if strings.TrimSpace(envelope.TraceState) != "" {
		carrier.Set("tracestate", envelope.TraceState)
	}
	if len(carrier) > 0 {
		ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)
	}
	return ctx, envelope.Payload
}
