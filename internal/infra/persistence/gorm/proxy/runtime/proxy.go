package runtimeproxy

import (
	"encoding/json"
	"fmt"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"
	"net/http"
	"strings"
	"time"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	proxyruntime "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/proxy/runtime"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
	"gorm.io/gorm"
)

const (
	SecretNameProxyURL = proxyruntime.SecretNameProxyURL

	SettingEnabledAI       = proxyruntime.SettingEnabledAI
	SettingEnabledTelegram = proxyruntime.SettingEnabledTelegram
	SettingEnabledMarket   = proxyruntime.SettingEnabledMarket
	SettingEnabledWeb      = proxyruntime.SettingEnabledWeb
	SettingNoProxy         = proxyruntime.SettingNoProxy
	SettingRevision        = proxyruntime.SettingRevision

	ModuleAI       = proxyruntime.ModuleAI
	ModuleTelegram = proxyruntime.ModuleTelegram
	ModuleMarket   = proxyruntime.ModuleMarket
	ModuleWeb      = proxyruntime.ModuleWeb
)

var DefaultNoProxy = proxyruntime.DefaultNoProxy

type Config = proxyruntime.Config

func Load(db *gorm.DB, settings config.Settings) Config {
	sec := security.New(settings)
	return LoadWithSecurity(db, sec)
}

func LoadWithSecurity(db *gorm.DB, sec security.Service) Config {
	cfg := Config{NoProxy: append([]string{}, proxyruntime.DefaultNoProxy...)}
	var secret domainsettings.Secret
	if result := db.Where("kind = ? AND name = ?", domainkernel.SecretKindApp, proxyruntime.SecretNameProxyURL).Limit(1).Find(&secret); result.Error == nil && result.RowsAffected > 0 {
		if value, err := sec.DecryptSecret(secret.EncryptedValue); err == nil {
			cfg.ProxyURL = strings.TrimSpace(value)
		}
	}
	cfg.EnabledAI = boolSetting(db, proxyruntime.SettingEnabledAI)
	cfg.EnabledTelegram = boolSetting(db, proxyruntime.SettingEnabledTelegram)
	cfg.EnabledMarket = boolSetting(db, proxyruntime.SettingEnabledMarket)
	cfg.EnabledWeb = boolSetting(db, proxyruntime.SettingEnabledWeb)
	if values, ok := stringListSetting(db, proxyruntime.SettingNoProxy); ok {
		cfg.NoProxy = proxyruntime.NormalizeNoProxy(values)
	}
	if revision, updatedAt, ok := stringSettingWithUpdatedAt(db, proxyruntime.SettingRevision); ok {
		cfg.Revision = revision
		cfg.UpdatedAt = updatedAt
	}
	return cfg
}

func Revision(db *gorm.DB) string {
	value, _, _ := stringSettingWithUpdatedAt(db, proxyruntime.SettingRevision)
	return value
}

func NewHTTPClient(db *gorm.DB, settings config.Settings, module string, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cfg := Load(db, settings)
	return proxyruntime.HTTPClientForConfig(cfg, module, timeout)
}

func HTTPClientForConfig(cfg Config, module string, timeout time.Duration) *http.Client {
	return proxyruntime.HTTPClientForConfig(cfg, module, timeout)
}

func ValidateProxyURL(raw string) error {
	return proxyruntime.ValidateProxyURL(raw)
}

func ShouldBypass(host string, rules []string) bool {
	return proxyruntime.ShouldBypass(host, rules)
}

func boolSetting(db *gorm.DB, key string) bool {
	value, ok := scalarSetting(db, key)
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return fmt.Sprint(value) == "true"
	}
}

func stringListSetting(db *gorm.DB, key string) ([]string, bool) {
	value, ok := scalarSetting(db, key)
	if !ok {
		return nil, false
	}
	switch typed := value.(type) {
	case []string:
		return typed, true
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		return out, true
	default:
		return strings.Split(fmt.Sprint(value), ","), true
	}
}

func stringSettingWithUpdatedAt(db *gorm.DB, key string) (string, time.Time, bool) {
	var setting domainsettings.AppSetting
	result := db.Where("key = ?", key).Limit(1).Find(&setting)
	if result.Error != nil || result.RowsAffected == 0 {
		return "", time.Time{}, false
	}
	value := scalarFromJSON(setting.Value)
	return strings.TrimSpace(fmt.Sprint(value)), setting.UpdatedAt, true
}

func scalarSetting(db *gorm.DB, key string) (any, bool) {
	var setting domainsettings.AppSetting
	result := db.Where("key = ?", key).Limit(1).Find(&setting)
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, false
	}
	return scalarFromJSON(setting.Value), true
}

func scalarFromJSON(raw []byte) any {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	if obj, ok := value.(map[string]any); ok {
		if inner, exists := obj["value"]; exists {
			return inner
		}
	}
	return value
}
