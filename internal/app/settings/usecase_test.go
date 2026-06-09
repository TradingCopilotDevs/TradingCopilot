package settings

import (
	"context"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	"testing"
	"time"

	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
)

func TestRuntimeEnvUsesStoredOverridesAndFallbacks(t *testing.T) {
	repo := &fakeSettingsRepo{rows: []domainsettings.AppSetting{
		{Key: "PUBLIC_BASE_URL", Value: domainkernel.JSON(`{"value":"https://app.example"}`)},
		{Key: "MARKET_REALTIME_CACHE_TTL_SECONDS", Value: domainkernel.JSON(`{"value":10}`)},
		{Key: "BACKUP_ARCHIVE_PROVIDER", Value: domainkernel.JSON(`{"value":"s3"}`)},
		{Key: "BACKUP_ARCHIVE_S3_BUCKET", Value: domainkernel.JSON(`{"value":"tc-backups"}`)},
		{Key: "BACKUP_ARCHIVE_S3_SECRET_ACCESS_KEY", Value: domainkernel.JSON(`{"value":"stored-secret"}`)},
		{Key: "BACKUP_RESTORE_DRILL_INTERVAL_HOURS", Value: domainkernel.JSON(`{"value":24}`)},
		{Key: "OTEL_TRACES_EXPORTER", Value: domainkernel.JSON(`{"value":"otlp"}`)},
		{Key: "MEETING_MAX_ROUNDS", Value: domainkernel.JSON(`{"value":" "}`)},
	}}
	uc := NewUsecase(repo, fakeSettingsSecurity{}, RuntimeSettings{
		PublicBaseURL:                "http://localhost:5173",
		DatabaseURL:                  "sqlite://test",
		RedisURL:                     "redis://test",
		DefaultMarketProvider:        "adata",
		MarketRealtimeProvider:       "adata",
		MarketRealtimeCompatProvider: "tencent",
		MarketRealtimeCacheTTL:       5 * time.Second,
		MeetingMaxRounds:             6,
		MeetingDailyTokenBudget:      -1,
		AIDailyCostBudget:            -1,
		ToolResultLimit:              200,
		SQLStatementTimeoutMillis:    5000,
		BackupArchiveProvider:        "local",
		BackupArchiveS3Region:        "us-east-1",
		BackupArchiveS3AccessKeyID:   "env-key-id",
		BackupRestoreDrillInterval:   168 * time.Hour,
		OTELTracesExporter:           "none",
		OTELExporterOTLPProtocol:     "grpc",
		OTELTracesSampler:            "always_on",
	}, &fakeSettingsTransactor{repo: repo})

	items, err := uc.RuntimeEnv(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{}
	for _, item := range items {
		values[item.Key] = item.Value
	}
	if values["PUBLIC_BASE_URL"] != "https://app.example" {
		t.Fatalf("override not applied: %+v", values)
	}
	if values["MARKET_REALTIME_CACHE_TTL_SECONDS"].(float64) != 10 {
		t.Fatalf("numeric override not applied: %+v", values)
	}
	if values["MARKET_REALTIME_COMPAT_PROVIDER"] != "tencent" {
		t.Fatalf("expected realtime compatibility provider default: %+v", values)
	}
	if values["MEETING_MAX_ROUNDS"] != 6 {
		t.Fatalf("blank string should fall back: %+v", values)
	}
	if values["MEETING_DAILY_TOKEN_BUDGET"] != -1 {
		t.Fatalf("daily token budget should default to unlimited: %+v", values)
	}
	if values["AI_DAILY_COST_BUDGET"] != -1.0 {
		t.Fatalf("daily AI cost budget should default to unlimited: %+v", values)
	}
	if values["BACKUP_RESTORE_DRILL_INTERVAL_HOURS"].(float64) != 24 {
		t.Fatalf("backup restore drill interval override not applied: %+v", values)
	}
	if values["BACKUP_ARCHIVE_PROVIDER"] != "s3" || values["BACKUP_ARCHIVE_S3_BUCKET"] != "tc-backups" || values["BACKUP_ARCHIVE_S3_REGION"] != "us-east-1" {
		t.Fatalf("backup archive settings mismatch: %+v", values)
	}
	if values["BACKUP_ARCHIVE_S3_ACCESS_KEY_ID"] != "configured" || values["BACKUP_ARCHIVE_S3_SECRET_ACCESS_KEY"] != "configured" {
		t.Fatalf("backup archive credentials should be masked: %+v", values)
	}
	if values["OTEL_TRACES_EXPORTER"] != "otlp" || values["OTEL_EXPORTER_OTLP_PROTOCOL"] != "grpc" || values["OTEL_TRACES_SAMPLER"] != "always_on" {
		t.Fatalf("OTEL runtime settings mismatch: %+v", values)
	}
}

func TestWritesUseTransactions(t *testing.T) {
	repo := &fakeSettingsRepo{}
	tx := &fakeSettingsTransactor{repo: repo}
	uc := NewUsecase(repo, fakeSettingsSecurity{}, RuntimeSettings{}, tx)

	secret, err := uc.SaveSecret(context.Background(), domainkernel.SecretKindMarketData, "token", "plain")
	if err != nil {
		t.Fatal(err)
	}
	if secret.EncryptedValue != "encrypted:plain" || tx.txCount != 1 {
		t.Fatalf("secret write mismatch secret=%+v tx=%d", secret, tx.txCount)
	}

	description := "desc"
	setting, err := uc.UpsertAppSetting(context.Background(), "CUSTOM", map[string]any{"value": "x"}, &description)
	if err != nil {
		t.Fatal(err)
	}
	if setting.Key != "CUSTOM" || string(setting.Value) != `{"value":"x"}` || tx.txCount != 2 {
		t.Fatalf("setting write mismatch setting=%+v tx=%d", setting, tx.txCount)
	}
}

func TestUpsertAppSettingValidatesMeetingDailyTokenBudget(t *testing.T) {
	repo := &fakeSettingsRepo{}
	writes := map[string]any{}
	tx := &fakeSettingsTransactor{repo: repo}
	uc := NewUsecase(repo, fakeSettingsSecurity{}, RuntimeSettings{}, tx, func(values map[string]any, _ string) error {
		for key, value := range values {
			writes[key] = value
		}
		return nil
	})

	setting, err := uc.UpsertAppSetting(context.Background(), "MEETING_DAILY_TOKEN_BUDGET", map[string]any{"value": -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if setting.Key != "MEETING_DAILY_TOKEN_BUDGET" || writes["MEETING_DAILY_TOKEN_BUDGET"] != -1 {
		t.Fatalf("unlimited budget setting not saved cleanly setting=%+v writes=%+v", setting, writes)
	}

	if _, err := uc.UpsertAppSetting(context.Background(), "MEETING_DAILY_TOKEN_BUDGET", map[string]any{"value": 0}, nil); err == nil {
		t.Fatal("expected zero meeting daily token budget to be rejected")
	}
	if _, err := uc.UpsertAppSetting(context.Background(), "MEETING_DAILY_TOKEN_BUDGET", map[string]any{"value": -2}, nil); err == nil {
		t.Fatal("expected values below -1 to be rejected")
	}
	if _, err := uc.UpsertAppSetting(context.Background(), "MARKET_REALTIME_COMPAT_PROVIDER", map[string]any{"value": "invalid"}, nil); err == nil {
		t.Fatal("expected invalid realtime compatibility provider to be rejected")
	}
	if tx.txCount != 1 {
		t.Fatalf("invalid budget writes should fail before transaction, tx=%d", tx.txCount)
	}
}

func TestUpsertAppSettingValidatesAICostGovernance(t *testing.T) {
	repo := &fakeSettingsRepo{}
	writes := map[string]any{}
	tx := &fakeSettingsTransactor{repo: repo}
	uc := NewUsecase(repo, fakeSettingsSecurity{}, RuntimeSettings{}, tx, func(values map[string]any, _ string) error {
		for key, value := range values {
			writes[key] = value
		}
		return nil
	})

	if _, err := uc.UpsertAppSetting(context.Background(), "AI_DAILY_COST_BUDGET", map[string]any{"value": 12.5}, nil); err != nil {
		t.Fatal(err)
	}
	if writes["AI_DAILY_COST_BUDGET"] != 12.5 {
		t.Fatalf("AI cost budget not written cleanly: %+v", writes)
	}
	if _, err := uc.UpsertAppSetting(context.Background(), "AI_DAILY_COST_BUDGET", map[string]any{"value": 0}, nil); err == nil {
		t.Fatal("expected zero AI daily cost budget to be rejected")
	}
	if _, err := uc.UpsertAppSetting(context.Background(), "AI_COST_RATES", map[string]any{"value": map[string]any{
		"currency": "USD",
		"rates": []any{map[string]any{
			"providerName":           "OpenAI",
			"model":                  "gpt-test",
			"inputPerMillion":        1.25,
			"outputPerMillion":       2.5,
			"completionPerMillion":   2.5,
			"prompt_per_million":     1.25,
			"completion_per_million": 2.5,
		}},
	}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.UpsertAppSetting(context.Background(), "AI_COST_RATES", map[string]any{"value": map[string]any{
		"rates": []any{map[string]any{"model": "gpt-test"}},
	}}, nil); err == nil {
		t.Fatal("expected AI cost rate without price to be rejected")
	}
}

func TestUpsertAppSettingValidatesOTELSettings(t *testing.T) {
	repo := &fakeSettingsRepo{}
	writes := map[string]any{}
	tx := &fakeSettingsTransactor{repo: repo}
	uc := NewUsecase(repo, fakeSettingsSecurity{}, RuntimeSettings{}, tx, func(values map[string]any, _ string) error {
		for key, value := range values {
			writes[key] = value
		}
		return nil
	})

	if _, err := uc.UpsertAppSetting(context.Background(), "OTEL_TRACES_EXPORTER", map[string]any{"value": "otlp"}, nil); err != nil {
		t.Fatal(err)
	}
	if writes["OTEL_TRACES_EXPORTER"] != "otlp" {
		t.Fatalf("OTEL exporter not written cleanly: %+v", writes)
	}
	if _, err := uc.UpsertAppSetting(context.Background(), "OTEL_EXPORTER_OTLP_PROTOCOL", map[string]any{"value": "http/protobuf"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.UpsertAppSetting(context.Background(), "OTEL_TRACES_SAMPLER_ARG", map[string]any{"value": "0.25"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.UpsertAppSetting(context.Background(), "OTEL_TRACES_EXPORTER", map[string]any{"value": "zipkin"}, nil); err == nil {
		t.Fatal("expected unsupported exporter to be rejected")
	}
	if _, err := uc.UpsertAppSetting(context.Background(), "OTEL_TRACES_SAMPLER_ARG", map[string]any{"value": "1.1"}, nil); err == nil {
		t.Fatal("expected invalid sampler arg to be rejected")
	}
}

func TestUpsertAppSettingValidatesBackupArchiveProvider(t *testing.T) {
	repo := &fakeSettingsRepo{}
	writes := map[string]any{}
	tx := &fakeSettingsTransactor{repo: repo}
	uc := NewUsecase(repo, fakeSettingsSecurity{}, RuntimeSettings{}, tx, func(values map[string]any, _ string) error {
		for key, value := range values {
			writes[key] = value
		}
		return nil
	})

	if _, err := uc.UpsertAppSetting(context.Background(), "BACKUP_ARCHIVE_PROVIDER", map[string]any{"value": "oss"}, nil); err != nil {
		t.Fatal(err)
	}
	if writes["BACKUP_ARCHIVE_PROVIDER"] != "oss" {
		t.Fatalf("backup archive provider not written cleanly: %+v", writes)
	}
	if _, err := uc.UpsertAppSetting(context.Background(), "BACKUP_ARCHIVE_PROVIDER", map[string]any{"value": "ftp"}, nil); err == nil {
		t.Fatal("expected unsupported backup archive provider to be rejected")
	}
	if _, err := uc.UpsertAppSetting(context.Background(), "BACKUP_ARCHIVE_S3_SECRET_ACCESS_KEY", map[string]any{"value": "secret"}, nil); err != nil {
		t.Fatal(err)
	}
	if writes["BACKUP_ARCHIVE_S3_SECRET_ACCESS_KEY"] != "secret" {
		t.Fatalf("backup archive secret not written cleanly: %+v", writes)
	}
}

func TestProxyConfigSaveLoadUsesTransactionAndEncryptedSecret(t *testing.T) {
	repo := &fakeSettingsRepo{}
	tx := &fakeSettingsTransactor{repo: repo}
	uc := NewUsecase(repo, fakeSettingsSecurity{}, RuntimeSettings{}, tx)
	uc.now = func() time.Time { return time.Unix(123, 456).UTC() }

	saved, err := uc.SaveProxyConfig(context.Background(), domainsettings.ProxyConfig{
		ProxyURL:        " socks5h://user:pass@proxy.example:1080 ",
		EnabledAI:       true,
		EnabledTelegram: true,
		NoProxy:         []string{"10.0.0.0/8", "LOCALHOST"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ProxyURL != "socks5h://user:pass@proxy.example:1080" || saved.Revision == "" || tx.txCount != 1 {
		t.Fatalf("saved proxy mismatch saved=%+v tx=%d", saved, tx.txCount)
	}
	if len(repo.secrets) != 1 || repo.secrets[0].EncryptedValue != "encrypted:socks5h://user:pass@proxy.example:1080" {
		t.Fatalf("proxy secret not encrypted: %+v", repo.secrets)
	}

	loaded, err := uc.LoadProxyConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ProxyURL != saved.ProxyURL || !loaded.EnabledAI || !loaded.EnabledTelegram || loaded.Revision == "" {
		t.Fatalf("loaded proxy mismatch: %+v", loaded)
	}
	if !domainsettings.ShouldBypass("10.1.2.3", loaded.NoProxy) || !domainsettings.ShouldBypass("localhost", loaded.NoProxy) {
		t.Fatalf("expected normalized noProxy rules: %+v", loaded.NoProxy)
	}
}

func TestProxyConfigRejectsEnabledModuleWithoutURL(t *testing.T) {
	repo := &fakeSettingsRepo{}
	uc := NewUsecase(repo, fakeSettingsSecurity{}, RuntimeSettings{}, &fakeSettingsTransactor{repo: repo})
	if _, err := uc.SaveProxyConfig(context.Background(), domainsettings.ProxyConfig{EnabledAI: true}); err == nil {
		t.Fatal("expected enabled proxy without URL to fail")
	}
}

type fakeSettingsRepo struct {
	rows    []domainsettings.AppSetting
	secrets []domainsettings.Secret
}

func (r *fakeSettingsRepo) ListAppSettingsByKeys(context.Context, []string) ([]domainsettings.AppSetting, error) {
	return r.rows, nil
}

func (r *fakeSettingsRepo) ListAppSettings(context.Context) ([]domainsettings.AppSetting, error) {
	return r.rows, nil
}

func (r *fakeSettingsRepo) SaveAppSetting(_ context.Context, setting *domainsettings.AppSetting) error {
	r.rows = append(r.rows, *setting)
	return nil
}

func (r *fakeSettingsRepo) ListSecretsExcludingKind(context.Context, domainkernel.SecretKind) ([]domainsettings.Secret, error) {
	return r.secrets, nil
}

func (r *fakeSettingsRepo) FindSecret(_ context.Context, kind domainkernel.SecretKind, name string) (*domainsettings.Secret, bool, error) {
	for i := range r.secrets {
		if r.secrets[i].Kind == kind && r.secrets[i].Name == name {
			return &r.secrets[i], true, nil
		}
	}
	return nil, false, nil
}

func (r *fakeSettingsRepo) CreateSecret(_ context.Context, secret *domainsettings.Secret) error {
	secret.ID = uint(len(r.secrets) + 1)
	r.secrets = append(r.secrets, *secret)
	return nil
}

func (r *fakeSettingsRepo) UpdateSecret(context.Context, *domainsettings.Secret) error {
	return nil
}

func (r *fakeSettingsRepo) DeleteSecret(_ context.Context, kind domainkernel.SecretKind, name string) error {
	out := r.secrets[:0]
	for _, secret := range r.secrets {
		if secret.Kind == kind && secret.Name == name {
			continue
		}
		out = append(out, secret)
	}
	r.secrets = out
	return nil
}

type fakeSettingsTransactor struct {
	repo    *fakeSettingsRepo
	txCount int
}

func (t *fakeSettingsTransactor) WithTx(ctx context.Context, fn func(Repository) error) error {
	t.txCount++
	return fn(t.repo)
}

type fakeSettingsSecurity struct{}

func (fakeSettingsSecurity) EncryptSecret(value string) (string, error) {
	return "encrypted:" + value, nil
}

func (fakeSettingsSecurity) DecryptSecret(value string) (string, error) {
	if len(value) > len("encrypted:") && value[:len("encrypted:")] == "encrypted:" {
		return value[len("encrypted:"):], nil
	}
	return value, nil
}
