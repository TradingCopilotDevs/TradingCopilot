package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	"strconv"
	"strings"
	"time"

	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"
)

type Repository interface {
	ListAppSettingsByKeys(ctx context.Context, keys []string) ([]domainsettings.AppSetting, error)
	ListAppSettings(ctx context.Context) ([]domainsettings.AppSetting, error)
	SaveAppSetting(ctx context.Context, setting *domainsettings.AppSetting) error
	ListSecretsExcludingKind(ctx context.Context, kind domainkernel.SecretKind) ([]domainsettings.Secret, error)
	FindSecret(ctx context.Context, kind domainkernel.SecretKind, name string) (*domainsettings.Secret, bool, error)
	CreateSecret(ctx context.Context, secret *domainsettings.Secret) error
	UpdateSecret(ctx context.Context, secret *domainsettings.Secret) error
	DeleteSecret(ctx context.Context, kind domainkernel.SecretKind, name string) error
}

type Transactor interface {
	WithTx(ctx context.Context, fn func(Repository) error) error
}

type SecurityService interface {
	EncryptSecret(value string) (string, error)
	DecryptSecret(value string) (string, error)
}

type ProxyTester interface {
	TestProxy(ctx context.Context, cfg domainsettings.ProxyConfig) (map[string]domainsettings.ProxyTestResult, error)
}

type Usecase struct {
	repo        Repository
	security    SecurityService
	settings    RuntimeSettings
	proxyTester ProxyTester
	tx          Transactor
	writeEnv    EnvWriter
	now         func() time.Time
}

type RuntimeSettings struct {
	DatabaseURL                  string
	RedisURL                     string
	PublicBaseURL                string
	DefaultMarketProvider        string
	MarketRealtimeProvider       string
	MarketRealtimeCompatProvider string
	MarketRealtimeCacheTTL       time.Duration
	MeetingMaxRounds             int
	MeetingDailyTokenBudget      int
	ToolResultLimit              int
	SQLStatementTimeoutMillis    int
	LogDir                       string
	LogLevel                     string
	LogRotationMode              string
	LogRotationSizeMB            int
	LogRotationTotalSizeMB       int
	LogRotationMaxAgeDays        int
	RuntimeEnvFile               string
}

type EnvWriter func(map[string]any, string) error

func NewUsecase(repo Repository, security SecurityService, settings RuntimeSettings, tx Transactor, writeEnv ...EnvWriter) Usecase {
	writer := func(map[string]any, string) error { return nil }
	if len(writeEnv) > 0 && writeEnv[0] != nil {
		writer = writeEnv[0]
	}
	return Usecase{
		repo:     repo,
		security: security,
		settings: settings,
		tx:       tx,
		writeEnv: writer,
		now:      time.Now,
	}
}

func (u Usecase) WithProxyTester(tester ProxyTester) Usecase {
	u.proxyTester = tester
	return u
}

type RuntimeEnvItem struct {
	Key             string
	Value           any
	Description     string
	Sensitive       bool
	EditableInUI    bool
	RestartRequired bool
}

type SecretDefinition struct {
	Kind        domainkernel.SecretKind
	Name        string
	Title       string
	Purpose     string
	RequiredFor string
	Placeholder string
}

type ProxyTestReport struct {
	Results   map[string]domainsettings.ProxyTestResult
	CheckedAt time.Time
}

var editableRuntimeEnvKeys = map[string]struct{}{
	"PUBLIC_BASE_URL":                   {},
	"DEFAULT_MARKET_PROVIDER":           {},
	"MARKET_REALTIME_PROVIDER":          {},
	"MARKET_REALTIME_COMPAT_PROVIDER":   {},
	"MARKET_REALTIME_CACHE_TTL_SECONDS": {},
	"MEETING_MAX_ROUNDS":                {},
	"MEETING_DAILY_TOKEN_BUDGET":        {},
	"TOOL_RESULT_LIMIT":                 {},
	"SQL_STATEMENT_TIMEOUT_MS":          {},
	"LOG_DIR":                           {},
	"LOG_LEVEL":                         {},
	"LOG_ROTATION_MODE":                 {},
	"LOG_ROTATION_SIZE_MB":              {},
	"LOG_ROTATION_TOTAL_SIZE_MB":        {},
	"LOG_ROTATION_MAX_AGE_DAYS":         {},
}

var SecretDefinitions = []SecretDefinition{
	{Kind: domainkernel.SecretKindMarketData, Name: "tushare_token", Title: "Tushare Token", Purpose: "Optional credential for Tushare-compatible market data.", RequiredFor: "Required only when the market data source is switched to Tushare.", Placeholder: "Enter Tushare token"},
}

func (u Usecase) RuntimeEnv(ctx context.Context) ([]RuntimeEnvItem, error) {
	overrides, err := u.runtimeEnvOverrides(ctx)
	if err != nil {
		return nil, err
	}
	return []RuntimeEnvItem{
		{Key: "APP_SECRET_KEY", Value: "configured", Description: "Backend master key used to encrypt stored secrets.", Sensitive: true},
		{Key: "JWT_SECRET_KEY", Value: "configured", Description: "JWT signing key.", Sensitive: true},
		{Key: "DATABASE_URL", Value: u.settings.DatabaseURL, Description: "Database connection string.", Sensitive: true},
		{Key: "REDIS_URL", Value: u.settings.RedisURL, Description: "Background task queue connection."},
		{Key: "PUBLIC_BASE_URL", Value: runtimeEnvValue(overrides, "PUBLIC_BASE_URL", u.settings.PublicBaseURL), Description: "Public frontend URL.", EditableInUI: true},
		{Key: "DEFAULT_MARKET_PROVIDER", Value: runtimeEnvValue(overrides, "DEFAULT_MARKET_PROVIDER", u.settings.DefaultMarketProvider), Description: "Historical/research source: adata or tushare.", EditableInUI: true},
		{Key: "MARKET_REALTIME_PROVIDER", Value: runtimeEnvValue(overrides, "MARKET_REALTIME_PROVIDER", u.settings.MarketRealtimeProvider), Description: "Realtime market source: adata.", EditableInUI: true},
		{Key: "MARKET_REALTIME_COMPAT_PROVIDER", Value: runtimeEnvValue(overrides, "MARKET_REALTIME_COMPAT_PROVIDER", firstNonEmptyString(u.settings.MarketRealtimeCompatProvider, "tencent")), Description: "ETF/LOF realtime compatibility source: tencent, sina, or disabled.", EditableInUI: true},
		{Key: "MARKET_REALTIME_CACHE_TTL_SECONDS", Value: runtimeEnvValue(overrides, "MARKET_REALTIME_CACHE_TTL_SECONDS", int(u.settings.MarketRealtimeCacheTTL.Seconds())), Description: "Realtime quote cache TTL in seconds.", EditableInUI: true},
		{Key: "MEETING_MAX_ROUNDS", Value: runtimeEnvValue(overrides, "MEETING_MAX_ROUNDS", u.settings.MeetingMaxRounds), Description: "Default maximum rounds.", EditableInUI: true},
		{Key: "MEETING_DAILY_TOKEN_BUDGET", Value: runtimeEnvValue(overrides, "MEETING_DAILY_TOKEN_BUDGET", u.settings.MeetingDailyTokenBudget), Description: "Daily token budget limit for meetings.", EditableInUI: true},
		{Key: "TOOL_RESULT_LIMIT", Value: runtimeEnvValue(overrides, "TOOL_RESULT_LIMIT", u.settings.ToolResultLimit), Description: "Default AI tool query limit.", EditableInUI: true},
		{Key: "SQL_STATEMENT_TIMEOUT_MS", Value: runtimeEnvValue(overrides, "SQL_STATEMENT_TIMEOUT_MS", u.settings.SQLStatementTimeoutMillis), Description: "Timeout for restricted read-only SQL tool queries.", EditableInUI: true},
		{Key: "LOG_DIR", Value: runtimeEnvValue(overrides, "LOG_DIR", u.settings.LogDir), Description: "Directory for structured application log files.", EditableInUI: true, RestartRequired: true},
		{Key: "LOG_LEVEL", Value: runtimeEnvValue(overrides, "LOG_LEVEL", u.settings.LogLevel), Description: "Minimum structured log level: debug, info, warn, error.", EditableInUI: true, RestartRequired: true},
		{Key: "LOG_ROTATION_MODE", Value: runtimeEnvValue(overrides, "LOG_ROTATION_MODE", u.settings.LogRotationMode), Description: "Log rotation mode: size or time.", EditableInUI: true, RestartRequired: true},
		{Key: "LOG_ROTATION_SIZE_MB", Value: runtimeEnvValue(overrides, "LOG_ROTATION_SIZE_MB", u.settings.LogRotationSizeMB), Description: "Size-mode active log file limit in MB.", EditableInUI: true, RestartRequired: true},
		{Key: "LOG_ROTATION_TOTAL_SIZE_MB", Value: runtimeEnvValue(overrides, "LOG_ROTATION_TOTAL_SIZE_MB", u.settings.LogRotationTotalSizeMB), Description: "Size-mode total log directory limit in MB.", EditableInUI: true, RestartRequired: true},
		{Key: "LOG_ROTATION_MAX_AGE_DAYS", Value: runtimeEnvValue(overrides, "LOG_ROTATION_MAX_AGE_DAYS", u.settings.LogRotationMaxAgeDays), Description: "Time-mode log retention window in days.", EditableInUI: true, RestartRequired: true},
	}, nil
}

func (u Usecase) SecretDefinitions(context.Context) []SecretDefinition {
	return SecretDefinitions
}

func (u Usecase) ListSecrets(ctx context.Context) ([]domainsettings.Secret, error) {
	return u.repo.ListSecretsExcludingKind(ctx, domainkernel.SecretKindTelegram)
}

func (u Usecase) SaveSecret(ctx context.Context, kind domainkernel.SecretKind, name string, value string) (*domainsettings.Secret, error) {
	encrypted, err := u.security.EncryptSecret(value)
	if err != nil {
		return nil, err
	}
	var saved *domainsettings.Secret
	if err := u.tx.WithTx(ctx, func(repo Repository) error {
		row, found, err := repo.FindSecret(ctx, kind, name)
		if err != nil {
			return err
		}
		if !found {
			row = &domainsettings.Secret{Kind: kind, Name: name, EncryptedValue: encrypted}
			if err := repo.CreateSecret(ctx, row); err != nil {
				return err
			}
			saved = row
			return nil
		}
		row.EncryptedValue = encrypted
		if err := repo.UpdateSecret(ctx, row); err != nil {
			return err
		}
		saved = row
		return nil
	}); err != nil {
		return nil, err
	}
	return saved, nil
}

func (u Usecase) ListAppSettings(ctx context.Context) ([]domainsettings.AppSetting, error) {
	return u.repo.ListAppSettings(ctx)
}

func (u Usecase) UpsertAppSetting(ctx context.Context, key string, value any, description *string) (*domainsettings.AppSetting, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("setting key is required")
	}
	if err := validateEditableRuntimeSetting(key, value); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	row := domainsettings.AppSetting{
		Key:         key,
		Value:       domainkernel.JSON(raw),
		Description: description,
		UpdatedAt:   u.now(),
	}
	if err := u.tx.WithTx(ctx, func(repo Repository) error {
		return repo.SaveAppSetting(ctx, &row)
	}); err != nil {
		return nil, err
	}
	if _, ok := editableRuntimeEnvKeys[key]; ok {
		if envValue := settingScalar(value); envValue != nil {
			if err := u.writeEnv(map[string]any{key: envValue}, u.settings.RuntimeEnvFile); err != nil {
				return nil, err
			}
		}
	}
	return &row, nil
}

func (u Usecase) LoadProxyConfig(ctx context.Context) (domainsettings.ProxyConfig, error) {
	cfg := domainsettings.ProxyConfig{NoProxy: domainsettings.NormalizeNoProxy(nil)}
	if secret, found, err := u.repo.FindSecret(ctx, domainkernel.SecretKindApp, domainsettings.SecretNameProxyURL); err != nil {
		return domainsettings.ProxyConfig{}, err
	} else if found {
		if value, err := u.security.DecryptSecret(secret.EncryptedValue); err == nil {
			cfg.ProxyURL = strings.TrimSpace(value)
		}
	}

	keys := []string{
		domainsettings.SettingEnabledAI,
		domainsettings.SettingEnabledTelegram,
		domainsettings.SettingEnabledMarket,
		domainsettings.SettingEnabledWeb,
		domainsettings.SettingNoProxy,
		domainsettings.SettingRevision,
	}
	rows, err := u.repo.ListAppSettingsByKeys(ctx, keys)
	if err != nil {
		return domainsettings.ProxyConfig{}, err
	}
	settingsByKey := map[string]domainsettings.AppSetting{}
	for _, row := range rows {
		settingsByKey[row.Key] = row
	}
	cfg.EnabledAI = boolStoredSetting(settingsByKey[domainsettings.SettingEnabledAI].Value)
	cfg.EnabledTelegram = boolStoredSetting(settingsByKey[domainsettings.SettingEnabledTelegram].Value)
	cfg.EnabledMarket = boolStoredSetting(settingsByKey[domainsettings.SettingEnabledMarket].Value)
	cfg.EnabledWeb = boolStoredSetting(settingsByKey[domainsettings.SettingEnabledWeb].Value)
	if noProxy, ok := stringListStoredSetting(settingsByKey[domainsettings.SettingNoProxy].Value); ok {
		cfg.NoProxy = domainsettings.NormalizeNoProxy(noProxy)
	}
	if row, ok := settingsByKey[domainsettings.SettingRevision]; ok {
		cfg.Revision = strings.TrimSpace(fmt.Sprint(storedSettingScalar(row.Value)))
		cfg.UpdatedAt = row.UpdatedAt
	}
	return cfg, nil
}

func (u Usecase) SaveProxyConfig(ctx context.Context, cfg domainsettings.ProxyConfig) (domainsettings.ProxyConfig, error) {
	cfg.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
	if cfg.ProxyURL != "" {
		if err := domainsettings.ValidateProxyURL(cfg.ProxyURL); err != nil {
			return domainsettings.ProxyConfig{}, err
		}
	} else if cfg.AnyModuleEnabled() {
		return domainsettings.ProxyConfig{}, errors.New("proxy_url is required when any proxy module is enabled")
	}

	now := u.now()
	revision := strconv.FormatInt(now.UnixNano(), 10)
	noProxy := domainsettings.NormalizeNoProxy(cfg.NoProxy)
	if err := u.tx.WithTx(ctx, func(repo Repository) error {
		if cfg.ProxyURL == "" {
			if err := repo.DeleteSecret(ctx, domainkernel.SecretKindApp, domainsettings.SecretNameProxyURL); err != nil {
				return err
			}
		} else {
			encrypted, err := u.security.EncryptSecret(cfg.ProxyURL)
			if err != nil {
				return err
			}
			row, found, err := repo.FindSecret(ctx, domainkernel.SecretKindApp, domainsettings.SecretNameProxyURL)
			if err != nil {
				return err
			}
			if !found {
				row = &domainsettings.Secret{Kind: domainkernel.SecretKindApp, Name: domainsettings.SecretNameProxyURL, EncryptedValue: encrypted}
				if err := repo.CreateSecret(ctx, row); err != nil {
					return err
				}
			} else {
				row.EncryptedValue = encrypted
				if err := repo.UpdateSecret(ctx, row); err != nil {
					return err
				}
			}
		}

		values := map[string]any{
			domainsettings.SettingEnabledAI:       cfg.EnabledAI,
			domainsettings.SettingEnabledTelegram: cfg.EnabledTelegram,
			domainsettings.SettingEnabledMarket:   cfg.EnabledMarket,
			domainsettings.SettingEnabledWeb:      cfg.EnabledWeb,
			domainsettings.SettingNoProxy:         noProxy,
			domainsettings.SettingRevision:        revision,
		}
		for key, value := range values {
			raw, err := json.Marshal(map[string]any{"value": value})
			if err != nil {
				return err
			}
			description := "runtime outbound proxy setting"
			row := domainsettings.AppSetting{Key: key, Value: domainkernel.JSON(raw), Description: &description, UpdatedAt: now}
			if err := repo.SaveAppSetting(ctx, &row); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return domainsettings.ProxyConfig{}, err
	}
	cfg.NoProxy = noProxy
	cfg.Revision = revision
	cfg.UpdatedAt = now
	return cfg, nil
}

func (u Usecase) TestProxyConfig(ctx context.Context) (ProxyTestReport, error) {
	cfg, err := u.LoadProxyConfig(ctx)
	if err != nil {
		return ProxyTestReport{}, err
	}
	checkedAt := u.now()
	if u.proxyTester == nil {
		return ProxyTestReport{
			CheckedAt: checkedAt,
			Results: map[string]domainsettings.ProxyTestResult{
				"ai":       {Status: "skipped", Detail: "Proxy tester is not configured."},
				"telegram": {Status: "skipped", Detail: "Proxy tester is not configured."},
				"market":   {Status: "skipped", Detail: "Proxy tester is not configured."},
				"web":      {Status: "skipped", Detail: "Proxy tester is not configured."},
			},
		}, nil
	}
	results, err := u.proxyTester.TestProxy(ctx, cfg)
	if err != nil {
		return ProxyTestReport{}, err
	}
	return ProxyTestReport{Results: results, CheckedAt: checkedAt}, nil
}

func (u Usecase) runtimeEnvOverrides(ctx context.Context) (map[string]any, error) {
	keys := []string{"PUBLIC_BASE_URL", "DEFAULT_MARKET_PROVIDER", "MARKET_REALTIME_PROVIDER", "MARKET_REALTIME_COMPAT_PROVIDER", "MARKET_REALTIME_CACHE_TTL_SECONDS", "MEETING_MAX_ROUNDS", "MEETING_DAILY_TOKEN_BUDGET", "TOOL_RESULT_LIMIT", "SQL_STATEMENT_TIMEOUT_MS", "LOG_DIR", "LOG_LEVEL", "LOG_ROTATION_MODE", "LOG_ROTATION_SIZE_MB", "LOG_ROTATION_TOTAL_SIZE_MB", "LOG_ROTATION_MAX_AGE_DAYS"}
	rows, err := u.repo.ListAppSettingsByKeys(ctx, keys)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	for _, row := range rows {
		var value any
		if err := json.Unmarshal(row.Value, &value); err != nil {
			continue
		}
		if obj, ok := value.(map[string]any); ok {
			if inner, exists := obj["value"]; exists {
				value = inner
			}
		}
		out[row.Key] = value
	}
	return out, nil
}

func settingScalar(value any) any {
	if obj, ok := value.(map[string]any); ok {
		if inner, exists := obj["value"]; exists {
			return inner
		}
	}
	return value
}

func validateEditableRuntimeSetting(key string, value any) error {
	if key == "MARKET_REALTIME_COMPAT_PROVIDER" {
		switch strings.ToLower(strings.TrimSpace(fmt.Sprint(settingScalar(value)))) {
		case "tencent", "sina", "disabled":
			return nil
		default:
			return errors.New("MARKET_REALTIME_COMPAT_PROVIDER must be tencent, sina, or disabled")
		}
	}
	if key != "MEETING_DAILY_TOKEN_BUDGET" {
		return nil
	}
	budget, ok := intScalar(settingScalar(value))
	if !ok || (budget != -1 && budget <= 0) {
		return errors.New("MEETING_DAILY_TOKEN_BUDGET must be -1 for unlimited or a positive integer")
	}
	return nil
}

func intScalar(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		parsed := int(typed)
		return parsed, typed == float64(parsed)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		return parsed, err == nil
	default:
		return 0, false
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func storedSettingScalar(raw []byte) any {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return settingScalar(value)
}

func boolStoredSetting(raw []byte) bool {
	value := storedSettingScalar(raw)
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return fmt.Sprint(value) == "true"
	}
}

func stringListStoredSetting(raw []byte) ([]string, bool) {
	value := storedSettingScalar(raw)
	switch typed := value.(type) {
	case nil:
		return nil, false
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

func runtimeEnvValue(overrides map[string]any, key string, fallback any) any {
	value, ok := overrides[key]
	if !ok || value == nil {
		return fallback
	}
	if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
		return fallback
	}
	return value
}
