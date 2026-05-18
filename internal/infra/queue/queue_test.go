package queue

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
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
	})

	if err := mux.ProcessTask(context.Background(), asynq.NewTask(TypeRunMeeting, []byte(`{"meeting_id":"bad"}`))); err == nil {
		t.Fatal("expected malformed payload error")
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
