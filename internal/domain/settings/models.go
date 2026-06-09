package settings

import (
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
)

type Secret struct {
	ID             uint
	Kind           kernel.SecretKind
	Name           string
	EncryptedValue string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type AppSetting struct {
	Key         string
	Value       kernel.JSON
	Description *string
	UpdatedAt   time.Time
}

const (
	SettingAICostRates       = "AI_COST_RATES"
	SettingAIDailyCostBudget = "AI_DAILY_COST_BUDGET"
)
