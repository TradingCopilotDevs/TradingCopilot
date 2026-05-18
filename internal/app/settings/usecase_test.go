package settings

import (
	"context"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	"testing"
	"time"

	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"
)

func TestRuntimeEnvUsesStoredOverridesAndFallbacks(t *testing.T) {
	repo := &fakeSettingsRepo{rows: []domainsettings.AppSetting{
		{Key: "PUBLIC_BASE_URL", Value: domainkernel.JSON(`{"value":"https://app.example"}`)},
		{Key: "MARKET_REALTIME_CACHE_TTL_SECONDS", Value: domainkernel.JSON(`{"value":10}`)},
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
		ToolResultLimit:              200,
		SQLStatementTimeoutMillis:    5000,
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
