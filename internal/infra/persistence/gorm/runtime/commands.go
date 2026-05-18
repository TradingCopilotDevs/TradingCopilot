package runtime

import (
	"context"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/connect"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/queue"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"gorm.io/gorm"
)

func OpenMigratedDB(settings config.Settings) (*gorm.DB, error) {
	db, err := database.Open(settings)
	if err != nil {
		return nil, err
	}
	if settings.AutoCreateTables {
		if err := database.AutoMigrate(db); err != nil {
			return nil, err
		}
	}
	return db, nil
}

func NewMigrated(settings config.Settings) (Runner, error) {
	db, err := OpenMigratedDB(settings)
	if err != nil {
		return Runner{}, err
	}
	return New(settings, db, security.New(settings)), nil
}

func Migrate(settings config.Settings) error {
	db, err := database.Open(settings)
	if err != nil {
		return err
	}
	return database.AutoMigrate(db)
}

func RunWorker(settings config.Settings) error {
	return jobs.RunWorker(settings)
}

func RunScheduler(settings config.Settings) error {
	return jobs.RunScheduler(settings)
}

func (r Runner) RunMessageSubscriptionListenerCommand(ctx context.Context) error {
	r.RunMessageSubscriptionListener(ctx)
	return nil
}
