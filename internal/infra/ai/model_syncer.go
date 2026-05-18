package ai

import (
	"context"
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	"net/http"

	aiclient "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/ai/client"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
)

type ModelSyncer struct {
	settings config.Settings
	security security.Service
	client   *http.Client
}

func NewModelSyncer(settings config.Settings, sec security.Service, client *http.Client) ModelSyncer {
	return ModelSyncer{settings: settings, security: sec, client: client}
}

func (s ModelSyncer) ListModels(_ context.Context, provider domainai.Provider) ([]map[string]any, error) {
	return aiclient.Client{
		Provider:   provider,
		Security:   s.security,
		Settings:   s.settings,
		HTTPClient: s.client,
	}.ListModels()
}
