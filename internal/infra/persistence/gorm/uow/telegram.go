package uow

import (
	"context"

	apptelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/app/telegram"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/gorm"
)

type TelegramUnitOfWork struct {
	db       *gorm.DB
	settings config.Settings
}

func NewTelegramUnitOfWork(db *gorm.DB, settings config.Settings) TelegramUnitOfWork {
	return TelegramUnitOfWork{db: db, settings: settings}
}

func (u TelegramUnitOfWork) WithTx(ctx context.Context, fn func(apptelegram.Repository) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(gormrepo.NewTelegramRepository(tx))
	})
}
