package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	"strconv"
	"strings"
	"time"

	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
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
	AIDailyCostBudget            float64
	ToolResultLimit              int
	SQLStatementTimeoutMillis    int
	LogDir                       string
	LogLevel                     string
	LogRotationMode              string
	LogRotationSizeMB            int
	LogRotationTotalSizeMB       int
	LogRotationMaxAgeDays        int
	BackupArchiveProvider        string
	BackupArchiveS3Bucket        string
	BackupArchiveS3Region        string
	BackupArchiveS3Endpoint      string
	BackupArchiveS3AccessKeyID   string
	BackupArchiveS3SecretKey     string
	BackupArchiveS3Prefix        string
	BackupArchiveOSSBucket       string
	BackupArchiveOSSRegion       string
	BackupArchiveOSSEndpoint     string
	BackupArchiveOSSAccessKey    string
	BackupArchiveOSSSecretKey    string
	BackupArchiveOSSPrefix       string
	BackupRestoreDrillInterval   time.Duration
	OTELServiceName              string
	OTELTracesExporter           string
	OTELExporterOTLPEndpoint     string
	OTELExporterOTLPProtocol     string
	OTELTracesSampler            string
	OTELTracesSamplerArg         string
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
	"PUBLIC_BASE_URL":                       {},
	"DEFAULT_MARKET_PROVIDER":               {},
	"MARKET_REALTIME_PROVIDER":              {},
	"MARKET_REALTIME_COMPAT_PROVIDER":       {},
	"MARKET_REALTIME_CACHE_TTL_SECONDS":     {},
	"MEETING_MAX_ROUNDS":                    {},
	"MEETING_DAILY_TOKEN_BUDGET":            {},
	domainsettings.SettingAIDailyCostBudget: {},
	"TOOL_RESULT_LIMIT":                     {},
	"SQL_STATEMENT_TIMEOUT_MS":              {},
	"LOG_DIR":                               {},
	"LOG_LEVEL":                             {},
	"LOG_ROTATION_MODE":                     {},
	"LOG_ROTATION_SIZE_MB":                  {},
	"LOG_ROTATION_TOTAL_SIZE_MB":            {},
	"LOG_ROTATION_MAX_AGE_DAYS":             {},
	"BACKUP_ARCHIVE_PROVIDER":               {},
	"BACKUP_ARCHIVE_S3_BUCKET":              {},
	"BACKUP_ARCHIVE_S3_REGION":              {},
	"BACKUP_ARCHIVE_S3_ENDPOINT":            {},
	"BACKUP_ARCHIVE_S3_ACCESS_KEY_ID":       {},
	"BACKUP_ARCHIVE_S3_SECRET_ACCESS_KEY":   {},
	"BACKUP_ARCHIVE_S3_PREFIX":              {},
	"BACKUP_ARCHIVE_OSS_BUCKET":             {},
	"BACKUP_ARCHIVE_OSS_REGION":             {},
	"BACKUP_ARCHIVE_OSS_ENDPOINT":           {},
	"BACKUP_ARCHIVE_OSS_ACCESS_KEY_ID":      {},
	"BACKUP_ARCHIVE_OSS_ACCESS_KEY_SECRET":  {},
	"BACKUP_ARCHIVE_OSS_PREFIX":             {},
	"BACKUP_RESTORE_DRILL_INTERVAL_HOURS":   {},
	"OTEL_SERVICE_NAME":                     {},
	"OTEL_TRACES_EXPORTER":                  {},
	"OTEL_EXPORTER_OTLP_ENDPOINT":           {},
	"OTEL_EXPORTER_OTLP_PROTOCOL":           {},
	"OTEL_TRACES_SAMPLER":                   {},
	"OTEL_TRACES_SAMPLER_ARG":               {},
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
		{Key: domainsettings.SettingAIDailyCostBudget, Value: runtimeEnvValue(overrides, domainsettings.SettingAIDailyCostBudget, u.settings.AIDailyCostBudget), Description: "Daily AI cost budget; -1 means unlimited.", EditableInUI: true},
		{Key: "TOOL_RESULT_LIMIT", Value: runtimeEnvValue(overrides, "TOOL_RESULT_LIMIT", u.settings.ToolResultLimit), Description: "Default AI tool query limit.", EditableInUI: true},
		{Key: "SQL_STATEMENT_TIMEOUT_MS", Value: runtimeEnvValue(overrides, "SQL_STATEMENT_TIMEOUT_MS", u.settings.SQLStatementTimeoutMillis), Description: "Timeout for restricted read-only SQL tool queries.", EditableInUI: true},
		{Key: "LOG_DIR", Value: runtimeEnvValue(overrides, "LOG_DIR", u.settings.LogDir), Description: "Directory for structured application log files.", EditableInUI: true, RestartRequired: true},
		{Key: "LOG_LEVEL", Value: runtimeEnvValue(overrides, "LOG_LEVEL", u.settings.LogLevel), Description: "Minimum structured log level: debug, info, warn, error.", EditableInUI: true, RestartRequired: true},
		{Key: "LOG_ROTATION_MODE", Value: runtimeEnvValue(overrides, "LOG_ROTATION_MODE", u.settings.LogRotationMode), Description: "Log rotation mode: size or time.", EditableInUI: true, RestartRequired: true},
		{Key: "LOG_ROTATION_SIZE_MB", Value: runtimeEnvValue(overrides, "LOG_ROTATION_SIZE_MB", u.settings.LogRotationSizeMB), Description: "Size-mode active log file limit in MB.", EditableInUI: true, RestartRequired: true},
		{Key: "LOG_ROTATION_TOTAL_SIZE_MB", Value: runtimeEnvValue(overrides, "LOG_ROTATION_TOTAL_SIZE_MB", u.settings.LogRotationTotalSizeMB), Description: "Size-mode total log directory limit in MB.", EditableInUI: true, RestartRequired: true},
		{Key: "LOG_ROTATION_MAX_AGE_DAYS", Value: runtimeEnvValue(overrides, "LOG_ROTATION_MAX_AGE_DAYS", u.settings.LogRotationMaxAgeDays), Description: "Time-mode log retention window in days.", EditableInUI: true, RestartRequired: true},
		{Key: "BACKUP_ARCHIVE_PROVIDER", Value: runtimeEnvValue(overrides, "BACKUP_ARCHIVE_PROVIDER", firstNonEmptyString(u.settings.BackupArchiveProvider, "local")), Description: "Archive provider: local, s3, or oss. S3 and OSS are active external archive stores when fully configured.", EditableInUI: true, RestartRequired: true},
		{Key: "BACKUP_ARCHIVE_S3_BUCKET", Value: runtimeEnvValue(overrides, "BACKUP_ARCHIVE_S3_BUCKET", u.settings.BackupArchiveS3Bucket), Description: "S3 archive bucket.", EditableInUI: true, RestartRequired: true},
		{Key: "BACKUP_ARCHIVE_S3_REGION", Value: runtimeEnvValue(overrides, "BACKUP_ARCHIVE_S3_REGION", u.settings.BackupArchiveS3Region), Description: "S3 archive region.", EditableInUI: true, RestartRequired: true},
		{Key: "BACKUP_ARCHIVE_S3_ENDPOINT", Value: runtimeEnvValue(overrides, "BACKUP_ARCHIVE_S3_ENDPOINT", u.settings.BackupArchiveS3Endpoint), Description: "Optional S3-compatible endpoint.", EditableInUI: true, RestartRequired: true},
		{Key: "BACKUP_ARCHIVE_S3_ACCESS_KEY_ID", Value: sensitiveRuntimeEnvValue(overrides, "BACKUP_ARCHIVE_S3_ACCESS_KEY_ID", u.settings.BackupArchiveS3AccessKeyID), Description: "S3 access key ID. Existing values are not revealed.", Sensitive: true, EditableInUI: true, RestartRequired: true},
		{Key: "BACKUP_ARCHIVE_S3_SECRET_ACCESS_KEY", Value: sensitiveRuntimeEnvValue(overrides, "BACKUP_ARCHIVE_S3_SECRET_ACCESS_KEY", u.settings.BackupArchiveS3SecretKey), Description: "S3 secret access key. Existing values are not revealed.", Sensitive: true, EditableInUI: true, RestartRequired: true},
		{Key: "BACKUP_ARCHIVE_S3_PREFIX", Value: runtimeEnvValue(overrides, "BACKUP_ARCHIVE_S3_PREFIX", u.settings.BackupArchiveS3Prefix), Description: "Optional S3 object prefix.", EditableInUI: true, RestartRequired: true},
		{Key: "BACKUP_ARCHIVE_OSS_BUCKET", Value: runtimeEnvValue(overrides, "BACKUP_ARCHIVE_OSS_BUCKET", u.settings.BackupArchiveOSSBucket), Description: "OSS archive bucket for external backup storage.", EditableInUI: true, RestartRequired: true},
		{Key: "BACKUP_ARCHIVE_OSS_REGION", Value: runtimeEnvValue(overrides, "BACKUP_ARCHIVE_OSS_REGION", u.settings.BackupArchiveOSSRegion), Description: "OSS archive region for external backup storage.", EditableInUI: true, RestartRequired: true},
		{Key: "BACKUP_ARCHIVE_OSS_ENDPOINT", Value: runtimeEnvValue(overrides, "BACKUP_ARCHIVE_OSS_ENDPOINT", u.settings.BackupArchiveOSSEndpoint), Description: "OSS archive endpoint for external backup storage.", EditableInUI: true, RestartRequired: true},
		{Key: "BACKUP_ARCHIVE_OSS_ACCESS_KEY_ID", Value: sensitiveRuntimeEnvValue(overrides, "BACKUP_ARCHIVE_OSS_ACCESS_KEY_ID", u.settings.BackupArchiveOSSAccessKey), Description: "OSS access key ID for external backup storage. Existing values are not revealed.", Sensitive: true, EditableInUI: true, RestartRequired: true},
		{Key: "BACKUP_ARCHIVE_OSS_ACCESS_KEY_SECRET", Value: sensitiveRuntimeEnvValue(overrides, "BACKUP_ARCHIVE_OSS_ACCESS_KEY_SECRET", u.settings.BackupArchiveOSSSecretKey), Description: "OSS access key secret for external backup storage. Existing values are not revealed.", Sensitive: true, EditableInUI: true, RestartRequired: true},
		{Key: "BACKUP_ARCHIVE_OSS_PREFIX", Value: runtimeEnvValue(overrides, "BACKUP_ARCHIVE_OSS_PREFIX", u.settings.BackupArchiveOSSPrefix), Description: "Optional OSS object prefix for external backup storage.", EditableInUI: true, RestartRequired: true},
		{Key: "BACKUP_RESTORE_DRILL_INTERVAL_HOURS", Value: runtimeEnvValue(overrides, "BACKUP_RESTORE_DRILL_INTERVAL_HOURS", int(u.settings.BackupRestoreDrillInterval.Hours())), Description: "Scheduled restore dry-run interval in hours; 0 disables scheduling.", EditableInUI: true, RestartRequired: true},
		{Key: "OTEL_SERVICE_NAME", Value: runtimeEnvValue(overrides, "OTEL_SERVICE_NAME", u.settings.OTELServiceName), Description: "OpenTelemetry service name override.", EditableInUI: true, RestartRequired: true},
		{Key: "OTEL_TRACES_EXPORTER", Value: runtimeEnvValue(overrides, "OTEL_TRACES_EXPORTER", u.settings.OTELTracesExporter), Description: "Trace exporter: none or otlp.", EditableInUI: true, RestartRequired: true},
		{Key: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: runtimeEnvValue(overrides, "OTEL_EXPORTER_OTLP_ENDPOINT", u.settings.OTELExporterOTLPEndpoint), Description: "OTLP collector endpoint URL.", EditableInUI: true, RestartRequired: true},
		{Key: "OTEL_EXPORTER_OTLP_PROTOCOL", Value: runtimeEnvValue(overrides, "OTEL_EXPORTER_OTLP_PROTOCOL", u.settings.OTELExporterOTLPProtocol), Description: "OTLP protocol: grpc or http/protobuf.", EditableInUI: true, RestartRequired: true},
		{Key: "OTEL_TRACES_SAMPLER", Value: runtimeEnvValue(overrides, "OTEL_TRACES_SAMPLER", u.settings.OTELTracesSampler), Description: "Trace sampler strategy.", EditableInUI: true, RestartRequired: true},
		{Key: "OTEL_TRACES_SAMPLER_ARG", Value: runtimeEnvValue(overrides, "OTEL_TRACES_SAMPLER_ARG", u.settings.OTELTracesSamplerArg), Description: "Sampler argument, for example a trace ID ratio between 0 and 1.", EditableInUI: true, RestartRequired: true},
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
	keys := []string{"PUBLIC_BASE_URL", "DEFAULT_MARKET_PROVIDER", "MARKET_REALTIME_PROVIDER", "MARKET_REALTIME_COMPAT_PROVIDER", "MARKET_REALTIME_CACHE_TTL_SECONDS", "MEETING_MAX_ROUNDS", "MEETING_DAILY_TOKEN_BUDGET", domainsettings.SettingAIDailyCostBudget, "TOOL_RESULT_LIMIT", "SQL_STATEMENT_TIMEOUT_MS", "LOG_DIR", "LOG_LEVEL", "LOG_ROTATION_MODE", "LOG_ROTATION_SIZE_MB", "LOG_ROTATION_TOTAL_SIZE_MB", "LOG_ROTATION_MAX_AGE_DAYS", "BACKUP_ARCHIVE_PROVIDER", "BACKUP_ARCHIVE_S3_BUCKET", "BACKUP_ARCHIVE_S3_REGION", "BACKUP_ARCHIVE_S3_ENDPOINT", "BACKUP_ARCHIVE_S3_ACCESS_KEY_ID", "BACKUP_ARCHIVE_S3_SECRET_ACCESS_KEY", "BACKUP_ARCHIVE_S3_PREFIX", "BACKUP_ARCHIVE_OSS_BUCKET", "BACKUP_ARCHIVE_OSS_REGION", "BACKUP_ARCHIVE_OSS_ENDPOINT", "BACKUP_ARCHIVE_OSS_ACCESS_KEY_ID", "BACKUP_ARCHIVE_OSS_ACCESS_KEY_SECRET", "BACKUP_ARCHIVE_OSS_PREFIX", "BACKUP_RESTORE_DRILL_INTERVAL_HOURS", "OTEL_SERVICE_NAME", "OTEL_TRACES_EXPORTER", "OTEL_EXPORTER_OTLP_ENDPOINT", "OTEL_EXPORTER_OTLP_PROTOCOL", "OTEL_TRACES_SAMPLER", "OTEL_TRACES_SAMPLER_ARG"}
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
	if key == domainsettings.SettingAICostRates {
		return validateAICostRates(value)
	}
	if key == "MARKET_REALTIME_COMPAT_PROVIDER" {
		switch strings.ToLower(strings.TrimSpace(fmt.Sprint(settingScalar(value)))) {
		case "tencent", "sina", "disabled":
			return nil
		default:
			return errors.New("MARKET_REALTIME_COMPAT_PROVIDER must be tencent, sina, or disabled")
		}
	}
	if key == "BACKUP_ARCHIVE_PROVIDER" {
		switch strings.ToLower(strings.TrimSpace(fmt.Sprint(settingScalar(value)))) {
		case "", "local", "local_filesystem", "s3", "oss":
			return nil
		default:
			return errors.New("BACKUP_ARCHIVE_PROVIDER must be local, s3, or oss")
		}
	}
	if key == "OTEL_TRACES_EXPORTER" {
		switch strings.ToLower(strings.TrimSpace(fmt.Sprint(settingScalar(value)))) {
		case "", "none", "noop", "otlp":
			return nil
		default:
			return errors.New("OTEL_TRACES_EXPORTER must be none or otlp")
		}
	}
	if key == "OTEL_EXPORTER_OTLP_PROTOCOL" {
		switch strings.ToLower(strings.TrimSpace(fmt.Sprint(settingScalar(value)))) {
		case "", "grpc", "http", "http/protobuf":
			return nil
		default:
			return errors.New("OTEL_EXPORTER_OTLP_PROTOCOL must be grpc or http/protobuf")
		}
	}
	if key == "OTEL_TRACES_SAMPLER" {
		switch strings.ToLower(strings.TrimSpace(fmt.Sprint(settingScalar(value)))) {
		case "", "always_on", "alwayson", "always_off", "alwaysoff", "traceidratio", "trace_id_ratio", "parentbased_traceidratio", "parent_based_traceidratio", "parentbased_always_on", "parent_based_always_on", "parentbased_always_off", "parent_based_always_off":
			return nil
		default:
			return errors.New("OTEL_TRACES_SAMPLER is not supported")
		}
	}
	if key == "OTEL_TRACES_SAMPLER_ARG" {
		value := strings.TrimSpace(fmt.Sprint(settingScalar(value)))
		if value == "" {
			return nil
		}
		ratio, err := strconv.ParseFloat(value, 64)
		if err != nil || ratio < 0 || ratio > 1 {
			return errors.New("OTEL_TRACES_SAMPLER_ARG must be between 0 and 1")
		}
	}
	switch key {
	case "MEETING_DAILY_TOKEN_BUDGET":
		budget, ok := intScalar(settingScalar(value))
		if !ok || (budget != -1 && budget <= 0) {
			return errors.New("MEETING_DAILY_TOKEN_BUDGET must be -1 for unlimited or a positive integer")
		}
	case domainsettings.SettingAIDailyCostBudget:
		budget, ok := floatScalar(settingScalar(value))
		if !ok || (budget != -1 && budget <= 0) {
			return errors.New("AI_DAILY_COST_BUDGET must be -1 for unlimited or a positive number")
		}
	}
	return nil
}

func validateAICostRates(value any) error {
	value = settingScalar(value)
	cfg, ok := value.(map[string]any)
	if !ok {
		return errors.New("AI_COST_RATES must be an object with a rates array")
	}
	ratesValue, exists := cfg["rates"]
	if !exists || ratesValue == nil {
		return nil
	}
	rates, ok := ratesValue.([]any)
	if !ok {
		return errors.New("AI_COST_RATES.rates must be an array")
	}
	for i, raw := range rates {
		item, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("AI_COST_RATES.rates[%d] must be an object", i)
		}
		if strings.TrimSpace(fmt.Sprint(item["model"])) == "" {
			return fmt.Errorf("AI_COST_RATES.rates[%d].model is required", i)
		}
		input, inputOK := floatScalar(firstPresent(item, "inputPerMillion", "promptPerMillion", "input_per_million", "prompt_per_million"))
		output, outputOK := floatScalar(firstPresent(item, "outputPerMillion", "completionPerMillion", "output_per_million", "completion_per_million"))
		total, totalOK := floatScalar(firstPresent(item, "totalPerMillion", "total_per_million"))
		if inputOK && input < 0 || outputOK && output < 0 || totalOK && total < 0 {
			return fmt.Errorf("AI_COST_RATES.rates[%d] prices must be non-negative", i)
		}
		if (!inputOK || input == 0) && (!outputOK || output == 0) && (!totalOK || total == 0) {
			return fmt.Errorf("AI_COST_RATES.rates[%d] must configure input/output or total price", i)
		}
	}
	return nil
}

func firstPresent(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
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

func floatScalar(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case float64:
		return typed, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
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

func sensitiveRuntimeEnvValue(overrides map[string]any, key string, fallback string) any {
	if value, ok := overrides[key]; ok {
		if strings.TrimSpace(fmt.Sprint(settingScalar(value))) != "" {
			return "configured"
		}
	}
	if strings.TrimSpace(fallback) != "" {
		return "configured"
	}
	return ""
}
