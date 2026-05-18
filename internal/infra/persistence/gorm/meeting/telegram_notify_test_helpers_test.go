package meeting

import (
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"
	"testing"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
	"gorm.io/gorm"
)

func seedTelegramBotSecrets(t *testing.T, db *gorm.DB, token string, chatID string) security.Service {
	t.Helper()
	sec := security.New(testTelegramBotSettings())
	encryptedToken, err := sec.EncryptSecret(token)
	if err != nil {
		t.Fatal(err)
	}
	encryptedChatID, err := sec.EncryptSecret(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domainsettings.Secret{Kind: domainkernel.SecretKindTelegram, Name: "bot_token", EncryptedValue: encryptedToken}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domainsettings.Secret{Kind: domainkernel.SecretKindTelegram, Name: "bot_chat_id", EncryptedValue: encryptedChatID}).Error; err != nil {
		t.Fatal(err)
	}
	return sec
}

func testTelegramBotSettings() config.Settings {
	return config.Settings{AppSecretKey: "telegram-bot-test-secret"}
}
