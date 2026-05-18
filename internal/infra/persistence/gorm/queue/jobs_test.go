package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	"testing"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/connect"
	"github.com/glebarez/sqlite"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRecoverQueuedMeetingsMarksRepeatedStaleRunFailed(t *testing.T) {
	db := newJobsTestDB(t)
	old := time.Now().Add(-time.Hour)
	runID := "stale-run"
	meeting := domainmeeting.Meeting{
		Topic:            "stale",
		Status:           domainkernel.MeetingRunning,
		TriggerSource:    "manual",
		StartedAt:        &old,
		HeartbeatAt:      &old,
		RunID:            &runID,
		AutoRequeueCount: 2,
	}
	if err := db.Create(&meeting).Error; err != nil {
		t.Fatal(err)
	}
	settings := config.Settings{MeetingStaleAfter: time.Second, MeetingAutoRequeueLimit: 2}
	recovered, err := RecoverQueuedMeetings(db, settings, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered mismatch: %d", recovered)
	}
	if err := db.First(&meeting, meeting.ID).Error; err != nil {
		t.Fatal(err)
	}
	if meeting.Status != domainkernel.MeetingFailed || meeting.CompletedAt == nil || meeting.RunID != nil {
		t.Fatalf("expected stale meeting failed, got %+v", meeting)
	}
	var event domainmeeting.Event
	if err := db.Where("meeting_id = ? AND type = ?", meeting.ID, domainkernel.EventError).First(&event).Error; err != nil {
		t.Fatal(err)
	}
}

func TestWorkerServeMuxConsumesRecoverQueuedMeetingsTask(t *testing.T) {
	db := newJobsTestDB(t)
	old := time.Now().Add(-time.Hour)
	runID := "stale-run"
	meeting := domainmeeting.Meeting{
		Topic:            "stale",
		Status:           domainkernel.MeetingRunning,
		TriggerSource:    "manual",
		StartedAt:        &old,
		HeartbeatAt:      &old,
		RunID:            &runID,
		AutoRequeueCount: 2,
	}
	if err := db.Create(&meeting).Error; err != nil {
		t.Fatal(err)
	}
	settings := config.Settings{MeetingStaleAfter: time.Second, MeetingAutoRequeueLimit: 2}
	mux := NewServeMux(db, settings)
	if err := mux.ProcessTask(context.Background(), asynq.NewTask(TypeRecoverQueuedMeetings, nil)); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&meeting, meeting.ID).Error; err != nil {
		t.Fatal(err)
	}
	if meeting.Status != domainkernel.MeetingFailed {
		t.Fatalf("expected worker-consumed recovery task to fail stale meeting, got %+v", meeting)
	}
}

func TestWorkerMeetingDispatchUsesConfiguredRedisEnqueuer(t *testing.T) {
	db := newJobsTestDB(t)
	meeting := domainmeeting.Meeting{
		Topic:         "message-triggered",
		Status:        domainkernel.MeetingQueued,
		TriggerSource: "message_subscription_refilter",
	}
	if err := db.Create(&meeting).Error; err != nil {
		t.Fatal(err)
	}
	var enqueued []uint
	meetingUsecase := newMeetingUsecase(db, config.Settings{MeetingDispatchMode: "redis"}, func(meetingID uint) error {
		enqueued = append(enqueued, meetingID)
		return nil
	})
	if err := dispatchMessageSubscriptionMeetings(context.Background(), meetingUsecase, []*domainmeeting.Meeting{&meeting}); err != nil {
		t.Fatal(err)
	}
	if len(enqueued) != 1 || enqueued[0] != meeting.ID {
		t.Fatalf("enqueued meeting IDs = %+v, want [%d]", enqueued, meeting.ID)
	}
	var event domainmeeting.Event
	if err := db.Where("meeting_id = ? AND content = ?", meeting.ID, "Meeting job submitted to Redis worker.").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["runner"] != "redis" || payload["status"] != "queued" {
		t.Fatalf("dispatch event payload = %+v, want redis queued", payload)
	}
}

func TestWorkerServeMuxRejectsMalformedRunMeetingPayload(t *testing.T) {
	db := newJobsTestDB(t)
	mux := NewServeMux(db, config.Settings{})
	if err := mux.ProcessTask(context.Background(), asynq.NewTask(TypeRunMeeting, []byte(`{"meeting_id":"bad"}`))); err == nil {
		t.Fatal("expected malformed run_meeting payload error")
	}
	payload, _ := json.Marshal(RunMeetingPayload{MeetingID: 999999})
	if err := mux.ProcessTask(context.Background(), asynq.NewTask(TypeRunMeeting, payload)); err == nil {
		t.Fatal("expected missing meeting processing error")
	}
}

func TestExistingPeriodicTaskErrorIsIdempotent(t *testing.T) {
	for _, err := range []error{
		asynq.ErrTaskIDConflict,
		fmt.Errorf("wrapped: %w", asynq.ErrTaskIDConflict),
		asynq.ErrDuplicateTask,
	} {
		if !isExistingPeriodicTask(err) {
			t.Fatalf("expected %v to be idempotent", err)
		}
	}
	if isExistingPeriodicTask(fmt.Errorf("redis unavailable")) {
		t.Fatal("unexpected transient enqueue error treated as idempotent")
	}
}

func newJobsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}
