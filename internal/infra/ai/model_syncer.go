package ai

import (
	"context"
	domainai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/ai"
	"net/http"

	aiclient "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/ai/client"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
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
