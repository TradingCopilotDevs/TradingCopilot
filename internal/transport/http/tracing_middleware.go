package httptransport

import (
	"net/http"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

const httpTracerName = "github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http"

func traceRequests(next http.Handler) http.Handler {
	tracer := otel.Tracer(httpTracerName)
	propagator := propagation.TraceContext{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path)
		defer span.End()

		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r.WithContext(ctx))

		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		span.SetAttributes(
			attribute.String("http.request.method", r.Method),
			attribute.String("url.path", r.URL.Path),
			attribute.String("url.query", r.URL.RawQuery),
			attribute.Int("http.response.status_code", status),
			attribute.Int("http.response.body.size", recorder.bytes),
		)
		if status >= http.StatusInternalServerError {
			span.SetAttributes(attribute.String("error.type", "http.status."+strconv.Itoa(status)))
		}
	})
}
