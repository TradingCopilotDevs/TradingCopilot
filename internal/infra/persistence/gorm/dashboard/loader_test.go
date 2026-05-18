package dashboard

import (
	"context"
	"testing"
	"time"

	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/connect"
	persistmodel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/model"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMessageSubscriptionStatusRequiresDecryptableSecrets(t *testing.T) {
	db := newDashboardTestDB(t)
	oldSecret := security.New(config.Settings{AppSecretKey: "old-secret"})
	for _, item := range []struct {
		name  string
		value string
	}{
		{name: "telegram:app_id", value: "12345"},
		{name: "telegram:app_hash", value: "hash"},
		{name: "telegram:mtproto_session", value: "session"},
	} {
		encrypted, err := oldSecret.EncryptSecret(item.value)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&persistmodel.Secret{
			Kind:           domainkernel.SecretKindMessageSubscription,
			Name:           item.name,
			EncryptedValue: encrypted,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&persistmodel.MessageSubscription{
		Provider:  "telegram_channel",
		Title:     "News",
		SourceRef: "@news",
		Enabled:   true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&persistmodel.AppSetting{
		Key:       "system_heartbeat_message_subscription_listener",
		Value:     datatypes.JSON(domainkernel.NewJSON(map[string]any{"service": "message_subscription_listener", "mode": "redis", "status": "running", "timestamp": time.Now(), "pid": 1})),
		UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	payload, err := NewLoader(db, config.Settings{AppSecretKey: "current-secret", MeetingDispatchMode: "redis"}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	systemStatus := payload["systemStatus"].(map[string]any)
	listener := systemStatus["messageSubscriptionListener"].(map[string]any)
	if listener["status"] != "disabled" {
		t.Fatalf("undecryptable MTProto secrets should disable listener status, got %+v", listener)
	}
	diagnostics := payload["dependencyDiagnostics"].(map[string]any)
	messaging := diagnostics["messaging"].(map[string]any)
	provider := messaging["provider"].(map[string]any)
	mtproto := provider["mtproto"].(map[string]any)
	if mtproto["hasAppId"] != false || mtproto["hasAppHash"] != false || mtproto["hasSession"] != false {
		t.Fatalf("undecryptable MTProto diagnostics should be false, got %+v", mtproto)
	}
}

func newDashboardTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}
