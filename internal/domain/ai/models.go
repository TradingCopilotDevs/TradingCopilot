package ai

import (
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
)

type Provider struct {
	ID             uint
	Name           string
	BaseURL        string
	APIKeySecretID *uint
	APIKeySecret   *domainsettings.Secret
	DefaultModel   string
	Enabled        bool
	CreatedAt      time.Time
}

type ProviderModel struct {
	ID          uint
	ProviderID  uint
	Provider    *Provider
	ModelID     string
	DisplayName string
	OwnedBy     *string
	Enabled     bool
	Raw         kernel.JSON
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type AgentRole struct {
	ID             uint
	Key            string
	Name           string
	Responsibility string
	PromptTemplate string
	ProviderID     *uint
	Provider       *Provider
	Model          *string
	ToolNames      kernel.JSON
	SkillNames     kernel.JSON
	Enabled        bool
	SortOrder      int
}

type PromptTemplate struct {
	ID        uint
	Key       string
	Title     string
	Content   string
	Variables kernel.JSON
	CreatedAt time.Time
}
