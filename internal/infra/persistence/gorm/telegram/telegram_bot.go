package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	apptelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/app/telegram"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	infratelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/telegram"
	"gorm.io/gorm"
)

const TelegramBotOffsetKey = apptelegram.BotOffsetSettingKey
const TelegramBotListenerLeaseKey = apptelegram.BotListenerLeaseSettingKey

var ErrTelegramBotLeaseHeld = errors.New("another Telegram Bot listener instance holds the polling lease")

var TelegramBotAPIBase = infratelegram.BotAPIBase

type TelegramBotUpdate = infratelegram.BotUpdate

type TelegramBotSender func(chatID string, text string) error

type MeetingDispatcher func(meeting *domainmeeting.Meeting) error

func TelegramBotPushConfig(db *gorm.DB, sec security.Service) (string, string, error) {
	var rows []persistmodel.Secret
	if err := db.Where("kind = ? AND name IN ?", domainkernel.SecretKindTelegram, []string{"bot_token", "bot_chat_id"}).Find(&rows).Error; err != nil {
		return "", "", err
	}
	secrets := map[string]domainsettings.Secret{}
	for _, row := range rows {
		secrets[row.Name] = secretFromModel(row)
	}
	tokenSecret, ok := secrets["bot_token"]
	if !ok {
		return "", "", errors.New("Telegram Bot Token and Chat ID are not configured")
	}
	chatSecret, ok := secrets["bot_chat_id"]
	if !ok {
		return "", "", errors.New("Telegram Bot Token and Chat ID are not configured")
	}
	token, err := sec.DecryptSecret(tokenSecret.EncryptedValue)
	if err != nil {
		return "", "", err
	}
	chatID, err := sec.DecryptSecret(chatSecret.EncryptedValue)
	if err != nil {
		return "", "", err
	}
	return token, chatID, nil
}

func SendTelegramBotMessage(apiBase string, token string, chatID string, text string) (map[string]any, error) {
	return infratelegram.SendBotMessage(apiBase, token, chatID, text)
}

func SendTelegramBotMessageWithClient(apiBase string, token string, chatID string, text string, client *http.Client) (map[string]any, error) {
	return infratelegram.SendBotMessageWithClient(apiBase, token, chatID, text, client)
}

func GetTelegramBotUpdates(apiBase string, token string, offset int64, timeoutSeconds int) ([]TelegramBotUpdate, error) {
	return infratelegram.GetBotUpdates(apiBase, token, offset, timeoutSeconds)
}

func GetTelegramBotUpdatesWithClient(apiBase string, token string, offset int64, timeoutSeconds int, client *http.Client) ([]TelegramBotUpdate, error) {
	return infratelegram.GetBotUpdatesWithClient(apiBase, token, offset, timeoutSeconds, client)
}

func LoadTelegramBotOffset(db *gorm.DB) int64 {
	settings := config.Load()
	offset, _ := telegramUsecase(db, settings, security.New(settings)).LoadBotOffset(dbContext(db))
	return offset
}

func StoreTelegramBotOffset(db *gorm.DB, offset int64) error {
	settings := config.Load()
	return telegramUsecase(db, settings, security.New(settings)).StoreBotOffset(dbContext(db), offset)
}

func ProcessTelegramBotUpdate(db *gorm.DB, sec security.Service, update TelegramBotUpdate, sender TelegramBotSender, dispatch MeetingDispatcher) (bool, error) {
	settings := config.Load()
	return telegramUsecase(db, settings, sec).ProcessBotUpdate(dbContext(db), update, apptelegram.BotSender(sender), apptelegram.MeetingDispatcher(dispatch))
}

func NormalizeTelegramBotCommand(text string) (string, string) {
	return infratelegram.NormalizeBotCommand(text)
}

func RunTelegramBotListener(ctx context.Context, db *gorm.DB, settings config.Settings, sec security.Service, dispatch MeetingDispatcher) {
	owner := telegramBotListenerOwner()
	heartbeat := systemUsecase(db)
	_ = heartbeat.RecordHeartbeat(ctx, "telegram_bot_listener", settings.MeetingDispatchMode, "")
	for {
		select {
		case <-ctx.Done():
			_ = heartbeat.MarkStopped(ctx, "telegram_bot_listener", settings.MeetingDispatchMode, ctx.Err().Error())
			return
		default:
		}
		_ = heartbeat.RecordHeartbeat(ctx, "telegram_bot_listener", settings.MeetingDispatchMode, "")
		if err := pollTelegramBotOnce(db, sec, dispatch, owner); err != nil {
			if errors.Is(err, ErrTelegramBotLeaseHeld) {
				_ = heartbeat.RecordHeartbeat(ctx, "telegram_bot_listener", settings.MeetingDispatchMode, err.Error())
				sleepWithContext(ctx, 10*time.Second)
				continue
			}
			_ = heartbeat.RecordHeartbeat(ctx, "telegram_bot_listener", settings.MeetingDispatchMode, err.Error())
			sleepWithContext(ctx, 10*time.Second)
			continue
		}
		_ = heartbeat.RecordHeartbeat(ctx, "telegram_bot_listener", settings.MeetingDispatchMode, "")
	}
}

func PollTelegramBotOnceForTest(db *gorm.DB, sec security.Service, dispatch MeetingDispatcher) error {
	return pollTelegramBotOnce(db, sec, dispatch, telegramBotListenerOwner())
}

func pollTelegramBotOnce(db *gorm.DB, sec security.Service, dispatch MeetingDispatcher, owner string) error {
	settings := config.Load()
	token, _, err := TelegramBotPushConfig(db, sec)
	if err != nil {
		return err
	}
	acquired, err := AcquireTelegramBotListenerLease(db, owner, 45*time.Second)
	if err != nil {
		return err
	}
	if !acquired {
		return ErrTelegramBotLeaseHeld
	}
	offset := LoadTelegramBotOffset(db)
	client := runtimeproxy.NewHTTPClient(db, settings, runtimeproxy.ModuleTelegram, 25*time.Second)
	updates, err := GetTelegramBotUpdatesWithClient(TelegramBotAPIBase, token, offset, 15, client)
	if err != nil {
		return err
	}
	maxOffset := offset
	for _, update := range updates {
		updateID := int64FromAny(update["update_id"])
		if updateID+1 > maxOffset {
			maxOffset = updateID + 1
		}
		_, err := ProcessTelegramBotUpdate(db, sec, update, func(chatID string, text string) error {
			_, err := SendTelegramBotMessageWithClient(TelegramBotAPIBase, token, chatID, text, client)
			return err
		}, dispatch)
		if err != nil {
			return err
		}
	}
	if maxOffset != offset {
		return StoreTelegramBotOffset(db, maxOffset)
	}
	return nil
}

func telegramBotListenerOwner() string {
	host, _ := os.Hostname()
	if strings.TrimSpace(host) == "" {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}

func AcquireTelegramBotListenerLease(db *gorm.DB, owner string, ttl time.Duration) (bool, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = telegramBotListenerOwner()
	}
	settings := config.Load()
	return telegramUsecase(db, settings, security.New(settings)).AcquireBotListenerLease(dbContext(db), owner, ttl)
}

func int64FromAny(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		parsed, _ := v.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(v, 10, 64)
		return parsed
	default:
		return 0
	}
}

func sleepWithContext(ctx context.Context, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
