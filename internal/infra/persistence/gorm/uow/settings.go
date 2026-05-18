package uow

import (
	"context"

	appsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/settings"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/gorm"
)

type SettingsUnitOfWork struct {
	db *gorm.DB
}

func NewSettingsUnitOfWork(db *gorm.DB) SettingsUnitOfWork {
	return SettingsUnitOfWork{db: db}
}

func (u SettingsUnitOfWork) WithTx(ctx context.Context, fn func(appsettings.Repository) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(gormrepo.NewSettingsRepository(tx))
	})
}
