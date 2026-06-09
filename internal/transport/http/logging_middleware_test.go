package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestHTTPLogGroupAndNoise(t *testing.T) {
	cases := []struct {
		path  string
		group string
		noise bool
	}{
		{path: "/api/meetings", group: "meetings", noise: false},
		{path: "/api/message-subscriptions", group: "messaging", noise: false},
		{path: "/api/logs", group: "logs", noise: true},
		{path: "/assets/index.js", group: "static", noise: true},
		{path: "/logs", group: "frontend", noise: true},
	}
	for _, tt := range cases {
		group := logGroupForPath(tt.path)
		if group != tt.group {
			t.Fatalf("%s group = %s, want %s", tt.path, group, tt.group)
		}
		if noise := logNoise(tt.path, group); noise != tt.noise {
			t.Fatalf("%s noise = %v, want %v", tt.path, noise, tt.noise)
		}
	}
}

func TestTraceRequestsExtractsTraceContext(t *testing.T) {
	var traceID string
	handler := traceRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		spanCtx := trace.SpanContextFromContext(r.Context())
		if spanCtx.IsValid() {
			traceID = spanCtx.TraceID().String()
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if traceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace id = %q", traceID)
	}
}
