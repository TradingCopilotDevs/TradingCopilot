package uow

import (
	"context"

	appmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/app/market"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/gorm"
)

type MarketUnitOfWork struct {
	db       *gorm.DB
	settings config.Settings
}

func NewMarketUnitOfWork(db *gorm.DB, settings config.Settings) MarketUnitOfWork {
	return MarketUnitOfWork{db: db, settings: settings}
}

func (u MarketUnitOfWork) WithTx(ctx context.Context, fn func(appmarket.Repository) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(gormrepo.NewMarketRepository(tx))
	})
}
