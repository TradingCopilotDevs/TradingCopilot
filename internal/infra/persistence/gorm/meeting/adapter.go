package meeting

import (
	"context"
	appmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/app/meeting"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	infratelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/telegram"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"gorm.io/gorm"
)

type LocalMeetingStarter func(db *gorm.DB, meetingID uint) bool

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) Service {
	return Service{db: db}
}

func (s Service) JSON(value any) domainkernel.JSON { return JSON(value) }

func (s Service) TagsFromJSON(raw []byte) []string { return TagsFromJSON(raw) }

func (s Service) StartLocalRun(ctx context.Context, meetingID uint) bool {
	return StartLocalRun(s.db.WithContext(ctx), meetingID)
}

func (s Service) CancelLocalRun(meetingID uint) { CancelLocalRun(meetingID) }

func (s Service) Recap(ctx context.Context, meeting *domainmeeting.Meeting, requestedBy string) error {
	return Recap(s.db.WithContext(ctx), meeting, requestedBy)
}

func (s Service) RunOnce(ctx context.Context, meetingID uint) error {
	return RunOnceWithContext(ctx, s.db.WithContext(ctx), meetingID)
}

func (s Service) RecoverQueued(ctx context.Context, settings appmeeting.RecoverySettings, limit int, olderThan time.Duration, runner string, dispatch func(*domainmeeting.Meeting) error) (int, error) {
	return RecoverQueuedMeetings(s.db.WithContext(ctx), config.Settings{
		MeetingStaleAfter:       settings.MeetingStaleAfter,
		MeetingAutoRequeueLimit: settings.MeetingAutoRequeueLimit,
	}, limit, olderThan, runner, dispatch)
}

func RunOnceWithContext(ctx context.Context, db *gorm.DB, meetingID uint) error {
	return RunMeetingOnceWithContext(ctx, db, meetingID)
}

func StartLocalRun(db *gorm.DB, meetingID uint) bool {
	return StartLocalMeetingRun(db, meetingID)
}
func CancelLocalRun(meetingID uint) { CancelLocalMeetingRun(meetingID) }
func Recap(db *gorm.DB, meeting *domainmeeting.Meeting, requestedBy string) error {
	return RecapMeeting(db, meeting, requestedBy)
}
func NotifyFinished(db *gorm.DB, meeting *domainmeeting.Meeting, settings config.Settings, sec security.Service) {
	NotifyMeetingFinished(db, meeting, settings, sec)
}
func RunTelegramBotListener(ctx context.Context, db *gorm.DB, settings config.Settings, sec security.Service, dispatch infratelegram.MeetingDispatcher) {
	infratelegram.RunTelegramBotListener(ctx, db, settings, sec, dispatch)
}
