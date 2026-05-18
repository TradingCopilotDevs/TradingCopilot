package runtimeproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	appsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/settings"
	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/connect"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	gormuow "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/uow"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestSaveLoadProxyConfigEncryptedAndNoProxyDefaults(t *testing.T) {
	db := newProxyTestDB(t)
	sec := security.New(config.Settings{AppSecretKey: "proxy-test-secret"})
	uc := appsettings.NewUsecase(gormrepo.NewSettingsRepository(db), sec, appsettings.RuntimeSettings{}, gormuow.NewSettingsUnitOfWork(db))
	saved, err := uc.SaveProxyConfig(context.Background(), domainsettings.ProxyConfig{
		ProxyURL:        "socks5h://user:pass@proxy.example:1080",
		EnabledAI:       true,
		EnabledTelegram: true,
		NoProxy:         []string{"10.0.0.0/8"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.ProxyURL != "socks5h://user:pass@proxy.example:1080" || !saved.EnabledAI || !saved.EnabledTelegram {
		t.Fatalf("saved config mismatch: %+v", saved)
	}
	loaded := LoadWithSecurity(db, sec)
	if loaded.ProxyURL != saved.ProxyURL || !loaded.EnabledAI || !loaded.EnabledTelegram || loaded.Revision == "" {
		t.Fatalf("loaded config mismatch: %+v", loaded)
	}
	if !ShouldBypass("localhost", loaded.NoProxy) || !ShouldBypass("127.0.0.1", loaded.NoProxy) || !ShouldBypass("10.1.2.3", loaded.NoProxy) {
		t.Fatalf("expected default and CIDR no_proxy rules: %+v", loaded.NoProxy)
	}
}

func TestHTTPClientIgnoresEnvironmentProxyWhenModuleDisabled(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	resp, err := HTTPClientForConfig(Config{ProxyURL: "http://127.0.0.1:1"}, ModuleAI, 0).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

func TestValidateProxyURL(t *testing.T) {
	for _, raw := range []string{"http://proxy.example:8080", "https://proxy.example:8443", "socks5://proxy.example:1080", "socks5h://proxy.example:1080"} {
		if err := ValidateProxyURL(raw); err != nil {
			t.Fatalf("expected valid proxy %s: %v", raw, err)
		}
	}
	if err := ValidateProxyURL("ftp://proxy.example:21"); err == nil {
		t.Fatal("expected unsupported scheme to fail")
	}
}

func newProxyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	_ = os.Setenv("TZ", "Asia/Shanghai")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}
