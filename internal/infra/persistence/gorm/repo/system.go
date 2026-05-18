package repo

import (
	"context"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"

	"gorm.io/gorm"
)

type SystemRepository struct {
	settings SettingsRepository
	db       *gorm.DB
}

func NewSystemRepository(db *gorm.DB) SystemRepository {
	return SystemRepository{settings: NewSettingsRepository(db), db: db}
}

func (r SystemRepository) FindAppSetting(ctx context.Context, key string) (*domainsettings.AppSetting, bool, error) {
	return r.settings.FindAppSetting(ctx, key)
}

func (r SystemRepository) SaveAppSetting(ctx context.Context, setting *domainsettings.AppSetting) error {
	return r.settings.SaveAppSetting(ctx, setting)
}

func (r SystemRepository) DeleteAppSetting(ctx context.Context, key string) error {
	return r.settings.DeleteAppSetting(ctx, key)
}
