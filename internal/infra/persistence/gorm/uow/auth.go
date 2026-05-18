package uow

import (
	"context"

	appauth "github.com/TradingCopilotDevs/TradingCopilot/internal/app/auth"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/gorm"
)

type AuthUnitOfWork struct {
	db *gorm.DB
}

func NewAuthUnitOfWork(db *gorm.DB) AuthUnitOfWork {
	return AuthUnitOfWork{db: db}
}

func (u AuthUnitOfWork) WithTx(ctx context.Context, fn func(appauth.AdminRepository) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(gormrepo.NewAuthRepository(tx))
	})
}
