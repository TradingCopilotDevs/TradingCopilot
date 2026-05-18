package proxy

import (
	"context"
	"fmt"
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	"net/http"
	"net/url"
	"strings"
	"time"

	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/ai/client"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	marketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/marketdata"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	infratelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/telegram"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"gorm.io/gorm"
)

const webSearchProbeURL = "https://news.google.com/rss/search"

type Tester struct {
	db       *gorm.DB
	settings config.Settings
	security security.Service
}

func NewTester(db *gorm.DB, settings config.Settings, sec security.Service) Tester {
	return Tester{db: db, settings: settings, security: sec}
}

func (t Tester) TestProxy(ctx context.Context, cfg domainsettings.ProxyConfig) (map[string]domainsettings.ProxyTestResult, error) {
	runtimeCfg := runtimeConfig(cfg)
	return map[string]domainsettings.ProxyTestResult{
		"ai":       t.testAI(ctx, runtimeCfg),
		"telegram": t.testTelegram(ctx, runtimeCfg),
		"market":   t.testMarket(runtimeCfg),
		"web":      t.testWeb(runtimeCfg),
	}, nil
}

func runtimeConfig(cfg domainsettings.ProxyConfig) runtimeproxy.Config {
	return runtimeproxy.Config{
		ProxyURL:        cfg.ProxyURL,
		EnabledAI:       cfg.EnabledAI,
		EnabledTelegram: cfg.EnabledTelegram,
		EnabledMarket:   cfg.EnabledMarket,
		EnabledWeb:      cfg.EnabledWeb,
		NoProxy:         cfg.NoProxy,
		Revision:        cfg.Revision,
		UpdatedAt:       cfg.UpdatedAt,
	}
}

func proxyTestResult(status string, detail string, started time.Time) domainsettings.ProxyTestResult {
	return domainsettings.ProxyTestResult{Status: status, Detail: detail, DurationMS: time.Since(started).Milliseconds()}
}

func (t Tester) testAI(_ context.Context, cfg runtimeproxy.Config) domainsettings.ProxyTestResult {
	started := time.Now()
	if !cfg.EnabledAI {
		return proxyTestResult("skipped", "AI proxy is disabled.", started)
	}
	var row persistmodel.AiProvider
	if err := t.db.Preload("APIKeySecret").Where("enabled = ? AND api_key_secret_id IS NOT NULL", true).Order("id").First(&row).Error; err != nil {
		return proxyTestResult("skipped", "No enabled AI provider with API key.", started)
	}
	provider := aiProviderFromModel(row)
	_, err := ai.Client{Provider: provider, Security: t.security, Settings: t.settings, HTTPClient: runtimeproxy.HTTPClientForConfig(cfg, runtimeproxy.ModuleAI, 10*time.Second)}.ListModels()
	if err != nil {
		return proxyTestResult("error", err.Error(), started)
	}
	return proxyTestResult("ok", "AI provider model list succeeded.", started)
}

func (t Tester) testTelegram(ctx context.Context, cfg runtimeproxy.Config) domainsettings.ProxyTestResult {
	started := time.Now()
	if !cfg.EnabledTelegram {
		return proxyTestResult("skipped", "Telegram proxy is disabled.", started)
	}
	tokenSecret, ok := t.secret(domainkernel.SecretKindTelegram, "bot_token")
	if !ok {
		return proxyTestResult("skipped", "Telegram Bot token is not configured.", started)
	}
	token, err := t.security.DecryptSecret(tokenSecret.EncryptedValue)
	if err != nil {
		return proxyTestResult("error", err.Error(), started)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(infratelegram.BotAPIBase, "/")+"/bot"+strings.TrimSpace(token)+"/getMe", nil)
	resp, err := runtimeproxy.HTTPClientForConfig(cfg, runtimeproxy.ModuleTelegram, 10*time.Second).Do(req)
	if err != nil {
		return proxyTestResult("error", err.Error(), started)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return proxyTestResult("error", fmt.Sprintf("Telegram Bot getMe returned HTTP %d", resp.StatusCode), started)
	}
	return proxyTestResult("ok", "Telegram Bot getMe succeeded.", started)
}

func (t Tester) testMarket(cfg runtimeproxy.Config) domainsettings.ProxyTestResult {
	started := time.Now()
	if !cfg.EnabledMarket {
		return proxyTestResult("skipped", "Market proxy is disabled.", started)
	}
	marketdata.ApplyRuntimeProxy(t.db, t.settings)
	_, err := marketdata.RefreshQuotesWithProvider(t.db, []string{"600519"}, "adata", 0)
	if err != nil {
		return proxyTestResult("error", err.Error(), started)
	}
	return proxyTestResult("ok", "Market quote request succeeded.", started)
}

func (t Tester) testWeb(cfg runtimeproxy.Config) domainsettings.ProxyTestResult {
	started := time.Now()
	if !cfg.EnabledWeb {
		return proxyTestResult("skipped", "Web Search proxy is disabled.", started)
	}
	endpoint, err := url.Parse(webSearchProbeURL)
	if err != nil {
		return proxyTestResult("error", err.Error(), started)
	}
	query := endpoint.Query()
	query.Set("q", "A shares")
	query.Set("hl", "zh-CN")
	query.Set("gl", "CN")
	query.Set("ceid", "CN:zh-Hans")
	endpoint.RawQuery = query.Encode()
	req, _ := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	resp, err := runtimeproxy.HTTPClientForConfig(cfg, runtimeproxy.ModuleWeb, 10*time.Second).Do(req)
	if err != nil {
		return proxyTestResult("error", err.Error(), started)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return proxyTestResult("error", fmt.Sprintf("Web search probe returned HTTP %d", resp.StatusCode), started)
	}
	return proxyTestResult("ok", "Web search request succeeded.", started)
}

func (t Tester) secret(kind domainkernel.SecretKind, name string) (domainsettings.Secret, bool) {
	var row persistmodel.Secret
	if result := t.db.Where("kind = ? AND name = ?", kind, name).Limit(1).Find(&row); result.Error != nil || result.RowsAffected == 0 {
		return domainsettings.Secret{}, false
	}
	return secretFromModel(row), true
}

func aiProviderFromModel(row persistmodel.AiProvider) domainai.Provider {
	var secret *domainsettings.Secret
	if row.APIKeySecret != nil {
		converted := secretFromModel(*row.APIKeySecret)
		secret = &converted
	}
	return domainai.Provider{
		ID:             row.ID,
		Name:           row.Name,
		BaseURL:        row.BaseURL,
		APIKeySecretID: row.APIKeySecretID,
		APIKeySecret:   secret,
		DefaultModel:   row.DefaultModel,
		Enabled:        row.Enabled,
		CreatedAt:      row.CreatedAt,
	}
}

func secretFromModel(row persistmodel.Secret) domainsettings.Secret {
	return domainsettings.Secret{
		ID:             row.ID,
		Kind:           row.Kind,
		Name:           row.Name,
		EncryptedValue: row.EncryptedValue,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}
