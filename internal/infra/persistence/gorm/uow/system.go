package uow

import (
	"context"

	appsystem "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/system"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/gorm"
)

type SystemUnitOfWork struct {
	db *gorm.DB
}

func NewSystemUnitOfWork(db *gorm.DB) SystemUnitOfWork {
	return SystemUnitOfWork{db: db}
}

func (u SystemUnitOfWork) WithTx(ctx context.Context, fn func(appsystem.Repository) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(gormrepo.NewSystemRepository(tx))
	})
}
