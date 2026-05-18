package model

import (
	"time"

	"gorm.io/datatypes"
)

type AiProvider struct {
	ID             uint   `gorm:"primaryKey"`
	Name           string `gorm:"size:128;uniqueIndex"`
	BaseURL        string `gorm:"size:512"`
	APIKeySecretID *uint
	APIKeySecret   *Secret `gorm:"foreignKey:APIKeySecretID"`
	DefaultModel   string  `gorm:"size:128"`
	Enabled        bool    `gorm:"default:true"`
	CreatedAt      time.Time
	Models         []AiProviderModel `gorm:"foreignKey:ProviderID"`
}

func (AiProvider) TableName() string { return "ai_providers" }

type AiProviderModel struct {
	ID          uint           `gorm:"primaryKey"`
	ProviderID  uint           `gorm:"index;uniqueIndex:uq_ai_provider_model"`
	Provider    *AiProvider    `gorm:"foreignKey:ProviderID;constraint:OnDelete:CASCADE"`
	ModelID     string         `gorm:"size:256;index;uniqueIndex:uq_ai_provider_model"`
	DisplayName string         `gorm:"size:256"`
	OwnedBy     *string        `gorm:"size:128"`
	Enabled     bool           `gorm:"default:true"`
	Raw         datatypes.JSON `gorm:"type:json;default:'{}'"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (AiProviderModel) TableName() string { return "ai_provider_models" }

type AgentRole struct {
	ID             uint   `gorm:"primaryKey"`
	Key            string `gorm:"size:64;uniqueIndex"`
	Name           string `gorm:"size:128"`
	Responsibility string `gorm:"type:text"`
	PromptTemplate string `gorm:"type:text"`
	ProviderID     *uint
	Provider       *AiProvider    `gorm:"foreignKey:ProviderID"`
	Model          *string        `gorm:"size:128"`
	ToolNames      datatypes.JSON `gorm:"type:json;default:'[]'"`
	SkillNames     datatypes.JSON `gorm:"type:json;default:'[]'"`
	Enabled        bool           `gorm:"default:true"`
	SortOrder      int            `gorm:"default:100"`
}

func (AgentRole) TableName() string { return "agent_roles" }

type PromptTemplate struct {
	ID        uint           `gorm:"primaryKey"`
	Key       string         `gorm:"size:96;uniqueIndex"`
	Title     string         `gorm:"size:160"`
	Content   string         `gorm:"type:text"`
	Variables datatypes.JSON `gorm:"type:json;default:'{}'"`
	CreatedAt time.Time
}

func (PromptTemplate) TableName() string { return "prompt_templates" }
