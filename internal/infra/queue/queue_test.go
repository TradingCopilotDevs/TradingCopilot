package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	appmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/app/market"
	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestServeMuxDispatchesRunMeetingPayload(t *testing.T) {
	called := false
	mux := NewServeMux(Handlers{
		RunMeeting: func(_ context.Context, payload RunMeetingPayload) error {
			called = true
			if payload.MeetingID != 42 {
				t.Fatalf("meeting id = %d, want 42", payload.MeetingID)
			}
			return nil
		},
		EvaluateWakePlans:     func(context.Context) error { return nil },
		RunPaperMaintenance:   func(context.Context) error { return nil },
		RecoverQueuedMeetings: func(context.Context) error { return nil },
		RunBackupRestoreDrill: func(context.Context) error { return nil },
	})

	if err := mux.ProcessTask(context.Background(), asynq.NewTask(TypeRunMeeting, []byte(`{"meeting_id":42}`))); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("run meeting handler was not called")
	}
}

func TestServeMuxRejectsMalformedRunMeetingPayload(t *testing.T) {
	mux := NewServeMux(Handlers{
		RunMeeting: func(context.Context, RunMeetingPayload) error {
			return errors.New("handler should not run")
		},
		EvaluateWakePlans:     func(context.Context) error { return nil },
		RunPaperMaintenance:   func(context.Context) error { return nil },
		RecoverQueuedMeetings: func(context.Context) error { return nil },
		RunBackupRestoreDrill: func(context.Context) error { return nil },
	})

	if err := mux.ProcessTask(context.Background(), asynq.NewTask(TypeRunMeeting, []byte(`{"meeting_id":"bad"}`))); err == nil {
		t.Fatal("expected malformed payload error")
	}
}

func TestServeMuxDispatchesMarketTaskPayload(t *testing.T) {
	called := false
	mux := NewServeMux(Handlers{
		RunMeeting:            func(context.Context, RunMeetingPayload) error { return nil },
		EvaluateWakePlans:     func(context.Context) error { return nil },
		RunPaperMaintenance:   func(context.Context) error { return nil },
		RecoverQueuedMeetings: func(context.Context) error { return nil },
		RunBackupRestoreDrill: func(context.Context) error { return nil },
		MarketTasks: MarketTaskHandlers{
			Run: func(_ context.Context, payload appmarket.Task) error {
				called = true
				if payload.Action != appmarket.TaskRefreshQuote || payload.Code != "600519" {
					t.Fatalf("market task payload = %+v", payload)
				}
				return nil
			},
		},
	})

	if err := mux.ProcessTask(context.Background(), asynq.NewTask(TypeRunMarketTask, []byte(`{"action":"refresh_quote","code":"600519"}`))); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("market task handler was not called")
	}
}

func TestServeMuxExtractsTraceEnvelope(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(oteltrace.NewNoopTracerProvider())
	})

	parentCtx, parent := otel.Tracer("test").Start(context.Background(), "producer")
	parentTraceID := parent.SpanContext().TraceID().String()
	taskPayload := encodeTaskPayload(parentCtx, RunMeetingPayload{MeetingID: 42})
	parent.End()

	if !strings.Contains(string(taskPayload), "traceparent") {
		t.Fatalf("expected trace envelope, got %s", taskPayload)
	}
	var envelope traceTaskEnvelope
	if err := json.Unmarshal(taskPayload, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.TraceEnvelopeVersion != traceEnvelopeVersion || !strings.Contains(string(envelope.Payload), `"meeting_id":42`) {
		t.Fatalf("unexpected trace envelope: %+v payload=%s", envelope, envelope.Payload)
	}

	var childTraceID string
	mux := NewServeMux(Handlers{
		RunMeeting: func(ctx context.Context, payload RunMeetingPayload) error {
			childTraceID = oteltrace.SpanContextFromContext(ctx).TraceID().String()
			if payload.MeetingID != 42 {
				t.Fatalf("meeting id = %d, want 42", payload.MeetingID)
			}
			return nil
		},
		EvaluateWakePlans:     func(context.Context) error { return nil },
		RunPaperMaintenance:   func(context.Context) error { return nil },
		RecoverQueuedMeetings: func(context.Context) error { return nil },
		RunBackupRestoreDrill: func(context.Context) error { return nil },
	})

	if err := mux.ProcessTask(context.Background(), asynq.NewTask(TypeRunMeeting, taskPayload)); err != nil {
		t.Fatal(err)
	}
	if childTraceID != parentTraceID {
		t.Fatalf("worker trace id = %q, want parent trace id %q", childTraceID, parentTraceID)
	}
}

func TestServeMuxDispatchesBackupRestoreDrillTask(t *testing.T) {
	called := false
	mux := NewServeMux(Handlers{
		RunMeeting:            func(context.Context, RunMeetingPayload) error { return nil },
		EvaluateWakePlans:     func(context.Context) error { return nil },
		RunPaperMaintenance:   func(context.Context) error { return nil },
		RecoverQueuedMeetings: func(context.Context) error { return nil },
		RunBackupRestoreDrill: func(context.Context) error {
			called = true
			return nil
		},
	})

	if err := mux.ProcessTask(context.Background(), asynq.NewTask(TypeRunBackupRestoreDrill, nil)); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("backup restore drill handler was not called")
	}
}

func TestPeriodicTaskIDCoalescesByTaskType(t *testing.T) {
	job := PeriodicJob{Type: TypeRecoverQueuedMeetings, Interval: time.Minute}
	first := PeriodicTaskID(job, time.Date(2026, 5, 16, 12, 0, 10, 0, time.UTC))
	sameWindow := PeriodicTaskID(job, time.Date(2026, 5, 16, 12, 0, 59, 0, time.UTC))
	nextWindow := PeriodicTaskID(job, time.Date(2026, 5, 16, 12, 1, 0, 0, time.UTC))
	if first != "periodic:"+TypeRecoverQueuedMeetings {
		t.Fatalf("periodic task ID = %q, want stable task-type ID", first)
	}
	if first != sameWindow || first != nextWindow {
		t.Fatalf("periodic task ID must not change across windows: %q %q %q", first, sameWindow, nextWindow)
	}
}

func TestPeriodicEnqueueOptionsAvoidExpiredBacklogNoise(t *testing.T) {
	job := PeriodicJob{Type: TypeRunPaperMaintenance, Interval: 10 * time.Second}
	opts := periodicEnqueueOptions(job, time.Date(2026, 5, 16, 12, 0, 10, 0, time.UTC))
	seen := map[asynq.OptionType]asynq.Option{}
	for _, opt := range opts {
		seen[opt.Type()] = opt
	}
	if got := seen[asynq.TaskIDOpt].Value(); got != "periodic:"+TypeRunPaperMaintenance {
		t.Fatalf("task ID option = %v, want stable periodic ID", got)
	}
	for _, forbidden := range []asynq.OptionType{asynq.DeadlineOpt, asynq.MaxRetryOpt} {
		if _, ok := seen[forbidden]; ok {
			t.Fatalf("periodic enqueue options must not include %v", forbidden)
		}
	}
	for _, required := range []asynq.OptionType{asynq.RetentionOpt, asynq.TimeoutOpt} {
		if _, ok := seen[required]; !ok {
			t.Fatalf("periodic enqueue options missing %v", required)
		}
	}
}
