package uow

import (
	"context"

	appai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/ai"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/gorm"
)

type AIUnitOfWork struct {
	db *gorm.DB
}

func NewAIUnitOfWork(db *gorm.DB) AIUnitOfWork {
	return AIUnitOfWork{db: db}
}

func (u AIUnitOfWork) WithTx(ctx context.Context, fn func(appai.Repository) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(gormrepo.NewAIRepository(tx))
	})
}
