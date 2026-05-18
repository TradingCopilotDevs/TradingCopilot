package meeting

import (
	"context"
	"testing"
	"time"

	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/meeting"
)

func TestRuntimeEntrypointsDelegateThroughMeetingUsecase(t *testing.T) {
	ctx := context.Background()
	service := &fakeRuntimeService{}
	usecase := NewUsecase(nil, service, Settings{})

	if err := usecase.RunOnce(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if service.runOnceID != 42 {
		t.Fatalf("RunOnce delegated id = %d, want 42", service.runOnceID)
	}

	recovered, err := usecase.RecoverQueued(ctx, RecoverySettings{MeetingStaleAfter: time.Minute, MeetingAutoRequeueLimit: 3}, 7, time.Second, "redis", nil)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 2 {
		t.Fatalf("RecoverQueued returned %d, want 2", recovered)
	}
	if service.recoveryRunner != "redis" || service.recoveryLimit != 7 {
		t.Fatalf("RecoverQueued delegated runner=%q limit=%d", service.recoveryRunner, service.recoveryLimit)
	}
}

type fakeRuntimeService struct {
	runOnceID      uint
	recoveryRunner string
	recoveryLimit  int
}

func (s *fakeRuntimeService) JSON(any) domainkernel.JSON { return nil }
func (s *fakeRuntimeService) TagsFromJSON([]byte) []string {
	return nil
}
func (s *fakeRuntimeService) StartLocalRun(context.Context, uint) bool { return true }
func (s *fakeRuntimeService) CancelLocalRun(uint)                      {}
func (s *fakeRuntimeService) Recap(context.Context, *domainmeeting.Meeting, string) error {
	return nil
}
func (s *fakeRuntimeService) RunOnce(_ context.Context, meetingID uint) error {
	s.runOnceID = meetingID
	return nil
}
func (s *fakeRuntimeService) RecoverQueued(_ context.Context, _ RecoverySettings, limit int, _ time.Duration, runner string, _ func(*domainmeeting.Meeting) error) (int, error) {
	s.recoveryRunner = runner
	s.recoveryLimit = limit
	return 2, nil
}
