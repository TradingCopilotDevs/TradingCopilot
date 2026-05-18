package uow

import (
	"context"

	appresearch "github.com/TradingCopilotDevs/TradingCopilot/internal/app/research"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/gorm"
)

type ResearchUnitOfWork struct {
	db *gorm.DB
}

func NewResearchUnitOfWork(db *gorm.DB) ResearchUnitOfWork {
	return ResearchUnitOfWork{db: db}
}

func (u ResearchUnitOfWork) WithTx(ctx context.Context, fn func(appresearch.Repository) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(gormrepo.NewResearchRepository(tx))
	})
}
