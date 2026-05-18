package uow

import (
	"context"

	appmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/app/meeting"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/gorm"
)

type MeetingUnitOfWork struct {
	db *gorm.DB
}

func NewMeetingUnitOfWork(db *gorm.DB) MeetingUnitOfWork {
	return MeetingUnitOfWork{db: db}
}

func (u MeetingUnitOfWork) WithTx(ctx context.Context, fn func(appmeeting.Repository) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(gormrepo.NewMeetingRepository(tx))
	})
}
