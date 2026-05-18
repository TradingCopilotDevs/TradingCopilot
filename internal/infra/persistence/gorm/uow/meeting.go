package uow

import (
	"context"

	appmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/meeting"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
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
