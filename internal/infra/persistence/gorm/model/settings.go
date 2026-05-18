package model

import (
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	"time"

	"gorm.io/datatypes"
)

type Secret struct {
	ID             uint                    `gorm:"primaryKey"`
	Kind           domainkernel.SecretKind `gorm:"size:32;index;uniqueIndex:uq_secret_kind_name"`
	Name           string                  `gorm:"size:128;index;uniqueIndex:uq_secret_kind_name"`
	EncryptedValue string                  `gorm:"type:text"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (Secret) TableName() string { return "secrets" }

type AppSetting struct {
	Key         string         `gorm:"size:128;primaryKey"`
	Value       datatypes.JSON `gorm:"type:json;default:'{}'"`
	Description *string        `gorm:"type:text"`
	UpdatedAt   time.Time
}

func (AppSetting) TableName() string { return "app_settings" }
