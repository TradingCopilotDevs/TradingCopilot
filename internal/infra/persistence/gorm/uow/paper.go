package uow

import (
	"context"

	apppaper "github.com/TradingCopilotDevs/TradingCopilot/internal/app/paper"
	infrapaper "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/paper"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/gorm"
)

type PaperUnitOfWork struct {
	db *gorm.DB
}

func NewPaperUnitOfWork(db *gorm.DB) PaperUnitOfWork {
	return PaperUnitOfWork{db: db}
}

func (u PaperUnitOfWork) WithTx(ctx context.Context, fn func(apppaper.Repository, apppaper.Service) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(gormrepo.NewPaperRepository(tx), infrapaper.NewService(tx))
	})
}
