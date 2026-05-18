package meeting

import (
	"encoding/json"
	"fmt"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainmsg "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/messaging"
	"strings"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	infratelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/telegram"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	puretelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/telegram"
	"gorm.io/gorm"
)

var telegramBotSendMessage = puretelegram.SendBotMessage
var telegramBotSendMessageWithClient = puretelegram.SendBotMessageWithClient

func NotifyMeetingFinished(db *gorm.DB, meeting *domainmeeting.Meeting, settings config.Settings, sec security.Service) {
	publicBaseURL := runtimeStringSetting(db, "PUBLIC_BASE_URL", settings.PublicBaseURL)
	meetingURL := strings.TrimRight(publicBaseURL, "/") + fmt.Sprintf("/meetings/%d", meeting.ID)
	conclusion := ""
	if meeting.Conclusion != nil {
		conclusion = strings.TrimSpace(*meeting.Conclusion)
	}
	if len(conclusion) > 2500 {
		conclusion = conclusion[:2500]
	}
	text := fmt.Sprintf(
		"%s meeting finished\nID: %d\nTopic: %s\nStatus: %s\nURL: %s\n\n%s",
		firstNonEmptyString(settings.AppName, "TradingCopilot"),
		meeting.ID,
		meeting.Topic,
		meeting.Status,
		meetingURL,
		conclusion,
	)
	token, chatID, err := platformAdapterPushConfig(db, sec)
	if err != nil {
		token, chatID, err = infratelegram.TelegramBotPushConfig(db, sec)
	}
	if err == nil {
		var result map[string]any
		result, err = telegramBotSendMessageWithClient(puretelegram.BotAPIBase, token, chatID, text, runtimeproxy.NewHTTPClient(db, settings, runtimeproxy.ModuleTelegram, 15*time.Second))
		if err == nil {
			payload := map[string]any{"status": "telegram_notified"}
			for key, value := range result {
				payload[key] = value
			}
			_, _ = AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, "Telegram completion notification sent.", payload)
			return
		}
	}
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventError, nil, "Telegram completion notification failed: "+err.Error(), map[string]any{"status": "telegram_notify_failed"})
}

func platformAdapterPushConfig(db *gorm.DB, sec security.Service) (string, string, error) {
	var adapter persistmodel.PlatformAdapter
	if err := db.Where("provider = ? AND enabled = ?", domainmsg.ProviderTelegramBot, true).Order("id").First(&adapter).Error; err != nil {
		return "", "", err
	}
	token, err := platformAdapterSecret(db, sec, adapter.ID, "bot_token")
	if err != nil {
		return "", "", err
	}
	chatID, err := platformAdapterSecret(db, sec, adapter.ID, "chat_id")
	if err != nil {
		return "", "", err
	}
	return token, chatID, nil
}

func platformAdapterSecret(db *gorm.DB, sec security.Service, adapterID uint, key string) (string, error) {
	var row persistmodel.Secret
	if err := db.First(&row, "kind = ? AND name = ?", domainkernel.SecretKindPlatformAdapter, fmt.Sprintf("adapter:%d:%s", adapterID, key)).Error; err != nil {
		return "", err
	}
	return sec.DecryptSecret(row.EncryptedValue)
}

func runtimeStringSetting(db *gorm.DB, key string, fallback string) string {
	var setting persistmodel.AppSetting
	if err := db.First(&setting, "key = ?", key).Error; err != nil {
		return fallback
	}
	var value any
	if err := json.Unmarshal(setting.Value, &value); err != nil {
		return fallback
	}
	if obj, ok := value.(map[string]any); ok {
		if inner, exists := obj["value"]; exists {
			value = inner
		}
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return fallback
	}
	return text
}
