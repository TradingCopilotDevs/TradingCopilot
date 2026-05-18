package uow

import (
	"context"

	appwake "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/wake"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/gorm"
)

type WakeUnitOfWork struct {
	db       *gorm.DB
	settings config.Settings
}

func NewWakeUnitOfWork(db *gorm.DB, settings config.Settings) WakeUnitOfWork {
	return WakeUnitOfWork{db: db, settings: settings}
}

func (u WakeUnitOfWork) WithTx(ctx context.Context, fn func(appwake.Repository) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(gormrepo.NewWakeRepository(tx))
	})
}
