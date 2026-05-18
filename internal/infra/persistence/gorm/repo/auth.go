package repo

import (
	"context"
	domainauth "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/auth"

	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/gorm"
)

type AuthRepository struct {
	db *gorm.DB
}

func NewAuthRepository(db *gorm.DB) AuthRepository {
	return AuthRepository{db: db}
}

func (r AuthRepository) CountAdmins(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&persistmodel.AdminUser{}).Count(&count).Error
	return count, err
}

func (r AuthRepository) CreateAdmin(ctx context.Context, user *domainauth.AdminUser) error {
	row := adminUserToModel(*user)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*user = adminUserFromModel(row)
	return nil
}

func (r AuthRepository) FindAdminByUsername(ctx context.Context, username string) (*domainauth.AdminUser, error) {
	var row persistmodel.AdminUser
	if err := r.db.WithContext(ctx).Where("username = ?", username).First(&row).Error; err != nil {
		return nil, err
	}
	user := adminUserFromModel(row)
	return &user, nil
}

func adminUserFromModel(row persistmodel.AdminUser) domainauth.AdminUser {
	return domainauth.AdminUser{
		ID:           row.ID,
		Username:     row.Username,
		PasswordHash: row.PasswordHash,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func adminUserToModel(row domainauth.AdminUser) persistmodel.AdminUser {
	return persistmodel.AdminUser{
		ID:           row.ID,
		Username:     row.Username,
		PasswordHash: row.PasswordHash,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}
