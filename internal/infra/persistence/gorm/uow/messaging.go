package uow

import (
	"context"

	appmessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/messaging"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/gorm"
)

type MessagingUnitOfWork struct {
	db       *gorm.DB
	settings config.Settings
}

func NewMessagingUnitOfWork(db *gorm.DB, settings config.Settings) MessagingUnitOfWork {
	return MessagingUnitOfWork{db: db, settings: settings}
}

func (u MessagingUnitOfWork) WithTx(ctx context.Context, fn func(appmessaging.Repository) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(gormrepo.NewMessagingRepository(tx))
	})
}
