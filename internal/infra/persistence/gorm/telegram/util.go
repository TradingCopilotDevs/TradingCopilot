package telegram

import (
	"context"
	"encoding/json"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	"time"

	appsystem "github.com/TradingCopilotDevs/TradingCopilot/internal/app/system"
	apptelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/app/telegram"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	gormuow "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/uow"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"gorm.io/gorm"
)

var appTZ = time.FixedZone("Asia/Shanghai", 8*3600)

func dbContext(db *gorm.DB) context.Context {
	if db != nil && db.Statement != nil && db.Statement.Context != nil {
		return db.Statement.Context
	}
	return context.Background()
}

func JSON(v any) domainkernel.JSON {
	b, _ := json.Marshal(v)
	return domainkernel.JSON(b)
}

func JSONList(values []string) domainkernel.JSON {
	return JSON(values)
}

func createMeeting(db *gorm.DB, topic string, triggerSource string) (*domainmeeting.Meeting, error) {
	if triggerSource == "" {
		triggerSource = "manual"
	}
	repo := gormrepo.NewMeetingRepository(db)
	meeting := domainmeeting.Meeting{Topic: topic, TriggerSource: triggerSource, Status: domainkernel.MeetingQueued, Tags: JSONList(nil)}
	if err := repo.Create(dbContext(db), &meeting); err != nil {
		return nil, err
	}
	_, err := appendEvent(db, meeting.ID, domainkernel.EventSystem, nil, "Meeting submitted for execution.", map[string]any{"status": "queued"})
	return &meeting, err
}

func appendEvent(db *gorm.DB, meetingID uint, eventType domainkernel.MeetingEventType, roleKey *string, content string, payload map[string]any) (*domainmeeting.Event, error) {
	event := domainmeeting.Event{MeetingID: meetingID, Type: eventType, RoleKey: roleKey, Content: content, Payload: JSON(payload)}
	if err := gormrepo.NewMeetingRepository(db).AppendEvent(dbContext(db), &event); err != nil {
		return nil, err
	}
	return &event, nil
}

func systemUsecase(db *gorm.DB) appsystem.Usecase {
	return appsystem.NewUsecase(gormrepo.NewSystemRepository(db), gormuow.NewSystemUnitOfWork(db))
}

func telegramUsecase(db *gorm.DB, settings config.Settings, sec security.Service) apptelegram.Usecase {
	return apptelegram.NewUsecase(gormrepo.NewTelegramRepository(db), NewService(db, settings), sec, gormuow.NewTelegramUnitOfWork(db, settings))
}
