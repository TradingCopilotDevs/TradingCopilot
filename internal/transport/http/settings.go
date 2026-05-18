package httptransport

import (
	"fmt"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	"net/http"
	"strconv"
	"time"

	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http/jsonapi"
	"github.com/go-chi/chi/v5"
)

func (s *Server) runtimeEnv(w http.ResponseWriter, r *http.Request) {
	items, err := s.settingsUsecase.RuntimeEnv(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "runtime-env-load-failed", "Runtime env load failed", err.Error(), "")
		return
	}
	resources := make([]jsonapi.Resource, 0, len(items))
	for _, item := range items {
		resources = append(resources, jsonapi.NewResource("runtime-env-vars", item.Key, map[string]any{
			"key":             item.Key,
			"value":           item.Value,
			"description":     item.Description,
			"sensitive":       item.Sensitive,
			"editableInUi":    item.EditableInUI,
			"restartRequired": item.RestartRequired,
		}))
	}
	writeResourceCollection(w, r, resources, 100, 500)
}

func (s *Server) secretDefinitions(w http.ResponseWriter, r *http.Request) {
	definitions := s.settingsUsecase.SecretDefinitions(r.Context())
	resources := make([]jsonapi.Resource, 0, len(definitions))
	for _, definition := range definitions {
		id := fmt.Sprintf("%s:%s", definition.Kind, definition.Name)
		resources = append(resources, jsonapi.NewResource("secret-definitions", id, map[string]any{
			"kind":        definition.Kind,
			"name":        definition.Name,
			"title":       definition.Title,
			"purpose":     definition.Purpose,
			"requiredFor": definition.RequiredFor,
			"placeholder": definition.Placeholder,
		}))
	}
	writeResourceCollection(w, r, resources, 100, 500)
}

func (s *Server) listSecrets(w http.ResponseWriter, r *http.Request) {
	rows, err := s.settingsUsecase.ListSecrets(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "secrets-load-failed", "Secrets load failed", err.Error(), "")
		return
	}
	resources := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		resources = append(resources, secretResource(row))
	}
	writeResourceCollection(w, r, resources, 100, 500)
}

func (s *Server) saveSecret(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Kind  domainkernel.SecretKind `json:"kind"`
		Name  string                  `json:"name"`
		Value string                  `json:"value"`
	}
	if !decodeJSONAPIRequest(w, r, &payload) {
		return
	}
	row, err := s.settingsUsecase.SaveSecret(r.Context(), payload.Kind, payload.Name, payload.Value)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "secret-save-failed", "Secret save failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, secretResource(*row))
}

func secretResource(row domainsettings.Secret) jsonapi.Resource {
	return jsonapi.NewResource("secrets", strconv.FormatUint(uint64(row.ID), 10), map[string]any{
		"kind":      row.Kind,
		"name":      row.Name,
		"createdAt": row.CreatedAt,
		"updatedAt": row.UpdatedAt,
	})
}

func (s *Server) listAppSettings(w http.ResponseWriter, r *http.Request) {
	rows, err := s.settingsUsecase.ListAppSettings(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "app-settings-load-failed", "App settings load failed", err.Error(), "")
		return
	}
	resources := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		resources = append(resources, appSettingResource(row))
	}
	writeResourceCollection(w, r, resources, 100, 500)
}

func (s *Server) upsertAppSetting(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	var payload struct {
		Value       any     `json:"value"`
		Description *string `json:"description"`
	}
	if !decodeJSONAPIRequest(w, r, &payload) {
		return
	}
	row, err := s.settingsUsecase.UpsertAppSetting(r.Context(), key, payload.Value, payload.Description)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "app-setting-save-failed", "App setting save failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, appSettingResource(*row))
}

func appSettingResource(row domainsettings.AppSetting) jsonapi.Resource {
	return jsonapi.NewResource("app-settings", row.Key, map[string]any{
		"key":         row.Key,
		"value":       row.Value,
		"description": row.Description,
		"updatedAt":   row.UpdatedAt,
	})
}

func (s *Server) proxySettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.settingsUsecase.LoadProxyConfig(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "proxy-load-failed", "Proxy load failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, proxyResource(cfg))
}

func (s *Server) saveProxySettings(w http.ResponseWriter, r *http.Request) {
	var payload proxyPayload
	if !decodeJSONAPIRequest(w, r, &payload) {
		return
	}
	cfg, err := s.settingsUsecase.SaveProxyConfig(r.Context(), payload.config())
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "proxy-save-failed", "Proxy save failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, proxyResource(cfg))
}

func (s *Server) testProxySettings(w http.ResponseWriter, r *http.Request) {
	report, err := s.settingsUsecase.TestProxyConfig(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "proxy-test-failed", "Proxy test failed", err.Error(), "")
		return
	}
	results := map[string]any{}
	for key, result := range report.Results {
		results[key] = map[string]any{
			"status":      result.Status,
			"detail":      result.Detail,
			"duration_ms": result.DurationMS,
		}
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("proxy-tests", "current", map[string]any{
		"results":   results,
		"checkedAt": report.CheckedAt,
	}))
}

type proxyPayload struct {
	ProxyURL        string   `json:"proxyUrl"`
	EnabledAI       bool     `json:"enabledAi"`
	EnabledTelegram bool     `json:"enabledTelegram"`
	EnabledMarket   bool     `json:"enabledMarket"`
	EnabledWeb      bool     `json:"enabledWeb"`
	NoProxy         []string `json:"noProxy"`
}

func (p proxyPayload) config() domainsettings.ProxyConfig {
	cfg := domainsettings.ProxyConfig{
		ProxyURL:        p.ProxyURL,
		EnabledAI:       p.EnabledAI,
		EnabledTelegram: p.EnabledTelegram,
		EnabledMarket:   p.EnabledMarket,
		EnabledWeb:      p.EnabledWeb,
		NoProxy:         p.NoProxy,
	}
	return cfg
}

func proxyResource(cfg domainsettings.ProxyConfig) jsonapi.Resource {
	attrs := map[string]any{
		"proxyUrl":        cfg.ProxyURL,
		"enabledAi":       cfg.EnabledAI,
		"enabledTelegram": cfg.EnabledTelegram,
		"enabledMarket":   cfg.EnabledMarket,
		"enabledWeb":      cfg.EnabledWeb,
		"noProxy":         cfg.NoProxy,
		"revision":        cfg.Revision,
	}
	if !cfg.UpdatedAt.IsZero() {
		attrs["updatedAt"] = cfg.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return jsonapi.NewResource("proxy-settings", "current", attrs)
}
