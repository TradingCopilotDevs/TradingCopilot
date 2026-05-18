package repo

import (
	"context"
	"errors"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
	"time"

	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type SettingsRepository struct {
	db *gorm.DB
}

func NewSettingsRepository(db *gorm.DB) SettingsRepository {
	return SettingsRepository{db: db}
}

func (r SettingsRepository) ListAppSettingsByKeys(ctx context.Context, keys []string) ([]domainsettings.AppSetting, error) {
	var rows []persistmodel.AppSetting
	if err := r.db.WithContext(ctx).Where("key IN ?", keys).Find(&rows).Error; err != nil {
		return nil, err
	}
	return appSettingsToDomain(rows), nil
}

func (r SettingsRepository) ListAppSettings(ctx context.Context) ([]domainsettings.AppSetting, error) {
	var rows []persistmodel.AppSetting
	if err := r.db.WithContext(ctx).Order("key").Find(&rows).Error; err != nil {
		return nil, err
	}
	return appSettingsToDomain(rows), nil
}

func (r SettingsRepository) FindAppSetting(ctx context.Context, key string) (*domainsettings.AppSetting, bool, error) {
	var row persistmodel.AppSetting
	err := r.db.WithContext(ctx).First(&row, "key = ?", key).Error
	if err == nil {
		out := appSettingFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r SettingsRepository) SaveAppSetting(ctx context.Context, setting *domainsettings.AppSetting) error {
	row := persistmodel.AppSetting{
		Key:         setting.Key,
		Value:       datatypes.JSON(setting.Value),
		Description: setting.Description,
		UpdatedAt:   setting.UpdatedAt,
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "description", "updated_at"}),
	}).Create(&row).Error
}

func (r SettingsRepository) DeleteAppSetting(ctx context.Context, key string) error {
	return r.db.WithContext(ctx).Delete(&persistmodel.AppSetting{}, "key = ?", key).Error
}

func (r SettingsRepository) ListSecretsExcludingKind(ctx context.Context, kind domainkernel.SecretKind) ([]domainsettings.Secret, error) {
	var rows []persistmodel.Secret
	if err := r.db.WithContext(ctx).Where("kind <> ?", kind).Order("kind, name").Find(&rows).Error; err != nil {
		return nil, err
	}
	return settingsSecretsToDomain(rows), nil
}

func (r SettingsRepository) FindSecret(ctx context.Context, kind domainkernel.SecretKind, name string) (*domainsettings.Secret, bool, error) {
	var row persistmodel.Secret
	err := r.db.WithContext(ctx).Where("kind = ? AND name = ?", kind, name).First(&row).Error
	if err == nil {
		out := settingsSecretFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r SettingsRepository) CreateSecret(ctx context.Context, secret *domainsettings.Secret) error {
	row := settingsSecretToModel(*secret)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*secret = settingsSecretFromModel(row)
	return nil
}

func (r SettingsRepository) UpdateSecret(ctx context.Context, secret *domainsettings.Secret) error {
	row := settingsSecretToModel(*secret)
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return err
	}
	*secret = settingsSecretFromModel(row)
	return nil
}

func (r SettingsRepository) DeleteSecret(ctx context.Context, kind domainkernel.SecretKind, name string) error {
	return r.db.WithContext(ctx).Where("kind = ? AND name = ?", kind, name).Delete(&persistmodel.Secret{}).Error
}

func (r SettingsRepository) TryAcquireLease(ctx context.Context, key string, value domainkernel.JSON, description string, now time.Time, cutoff time.Time, claimable func(domainsettings.AppSetting) bool) (bool, error) {
	var setting persistmodel.AppSetting
	err := r.db.WithContext(ctx).First(&setting, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row := persistmodel.AppSetting{Key: key, Value: datatypes.JSON(value), Description: &description, UpdatedAt: now}
		result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row)
		if result.Error != nil {
			return false, result.Error
		}
		if result.RowsAffected > 0 {
			return true, nil
		}
		if err := r.db.WithContext(ctx).First(&setting, "key = ?", key).Error; err != nil {
			return false, err
		}
	} else if err != nil {
		return false, err
	}
	domainSetting := domainsettings.AppSetting{Key: setting.Key, Value: domainkernel.JSON(setting.Value), Description: setting.Description, UpdatedAt: setting.UpdatedAt}
	if !claimable(domainSetting) {
		return false, nil
	}
	result := r.db.WithContext(ctx).Model(&persistmodel.AppSetting{}).
		Where("key = ? AND (updated_at = ? OR updated_at <= ? OR description = ?)", key, setting.UpdatedAt, cutoff, description).
		Updates(map[string]any{"value": datatypes.JSON(value), "description": description, "updated_at": now})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func appSettingsToDomain(rows []persistmodel.AppSetting) []domainsettings.AppSetting {
	out := make([]domainsettings.AppSetting, 0, len(rows))
	for _, row := range rows {
		out = append(out, appSettingFromModel(row))
	}
	return out
}

func appSettingFromModel(row persistmodel.AppSetting) domainsettings.AppSetting {
	return domainsettings.AppSetting{
		Key:         row.Key,
		Value:       domainkernel.JSON(row.Value),
		Description: row.Description,
		UpdatedAt:   row.UpdatedAt,
	}
}

func settingsSecretsToDomain(rows []persistmodel.Secret) []domainsettings.Secret {
	out := make([]domainsettings.Secret, 0, len(rows))
	for _, row := range rows {
		out = append(out, settingsSecretFromModel(row))
	}
	return out
}

func settingsSecretFromModel(row persistmodel.Secret) domainsettings.Secret {
	return domainsettings.Secret{
		ID:             row.ID,
		Kind:           row.Kind,
		Name:           row.Name,
		EncryptedValue: row.EncryptedValue,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func settingsSecretToModel(row domainsettings.Secret) persistmodel.Secret {
	return persistmodel.Secret{
		ID:             row.ID,
		Kind:           row.Kind,
		Name:           row.Name,
		EncryptedValue: row.EncryptedValue,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}
