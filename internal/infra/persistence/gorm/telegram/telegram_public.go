package telegram

import (
	"encoding/json"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domaintelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/telegram"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	infratelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/telegram"
	"gorm.io/gorm"
)

var telegramPublicBaseURL = infratelegram.PublicBaseURL

type TelegramPublicMessage = infratelegram.PublicMessage

type TelegramPublicResult = infratelegram.PublicResult

func GetLatestPublicTelegramMessage(channelRef string) (map[string]any, error) {
	return infratelegram.GetLatestPublicMessage(channelRef)
}

func FetchPublicTelegramMessages(channelRef string, limit int, minMessageID *int64) (TelegramPublicResult, error) {
	infratelegram.PublicBaseURL = telegramPublicBaseURL
	return infratelegram.FetchPublicMessages(channelRef, limit, minMessageID)
}

func FetchPublicTelegramMessagesWithClient(channelRef string, limit int, minMessageID *int64, client *http.Client) (TelegramPublicResult, error) {
	infratelegram.PublicBaseURL = telegramPublicBaseURL
	return infratelegram.FetchPublicMessagesWithClient(channelRef, limit, minMessageID, client)
}

func CollectPublicTelegramChannel(db *gorm.DB, channel *domaintelegram.Channel, limit int, sec security.Service, settings config.Settings) (int, int, error) {
	var latestRow persistmodel.TelegramMessage
	var minID *int64
	if err := db.Where("channel_id = ?", channel.ID).Order("message_id desc").First(&latestRow).Error; err == nil {
		minID = &latestRow.MessageID
	}
	fetchLimit := limit
	if fetchLimit < 100 {
		fetchLimit = 100
	}
	result, err := FetchPublicTelegramMessagesWithClient(channel.ChannelRef, fetchLimit, minID, runtimeproxy.NewHTTPClient(db, settings, runtimeproxy.ModuleTelegram, 20*time.Second))
	if err != nil {
		return 0, 0, err
	}
	if strings.TrimSpace(result.Title) != "" && result.Title != channel.Title {
		channel.Title = result.Title
		_ = gormrepo.NewTelegramRepository(db).SaveChannel(dbContext(db), channel)
	}
	sort.Slice(result.Messages, func(i, j int) bool { return result.Messages[i].MessageID < result.Messages[j].MessageID })
	collected := 0
	filtered := 0
	for _, item := range result.Messages {
		message, created, err := IngestTelegramChannelMessage(db, channel, item, "public_page", nil, sec, settings)
		if err != nil {
			return collected, filtered, err
		}
		if !created || message == nil {
			continue
		}
		collected++
		if message.FilterDecision != nil || message.FilterReason != nil {
			filtered++
		}
	}
	return collected, filtered, nil
}

func ReconcilePublicTelegramChannelBackfill(db *gorm.DB, channel *domaintelegram.Channel, previousBackfillLimit int, previousChannelRef string, sec security.Service, settings config.Settings) (map[string]int, error) {
	result := map[string]int{"added": 0, "removed": 0}
	oldLimit := max(previousBackfillLimit, 0)
	newLimit := max(channel.BackfillLimit, 0)
	oldRef := strings.TrimSpace(previousChannelRef)
	if oldRef == "" {
		oldRef = channel.ChannelRef
	}

	oldBackfillIDs := map[int64]struct{}{}
	if oldLimit > 0 {
		oldResult, err := FetchPublicTelegramMessages(oldRef, oldLimit, nil)
		if err != nil {
			return result, err
		}
		for _, item := range oldResult.Messages {
			oldBackfillIDs[item.MessageID] = struct{}{}
		}
	}

	var desiredItems []TelegramPublicMessage
	desiredIDs := map[int64]struct{}{}
	if newLimit > 0 {
		newResult, err := FetchPublicTelegramMessages(channel.ChannelRef, newLimit, nil)
		if err != nil {
			return result, err
		}
		if strings.TrimSpace(newResult.Title) != "" && newResult.Title != channel.Title {
			channel.Title = newResult.Title
			_ = gormrepo.NewTelegramRepository(db).SaveChannel(dbContext(db), channel)
		}
		desiredItems = newResult.Messages
		for _, item := range desiredItems {
			desiredIDs[item.MessageID] = struct{}{}
		}
	}

	var storedRows []persistmodel.TelegramMessage
	if err := db.Where("channel_id = ? AND message_time >= ?", channel.ID, channel.CollectFrom).Order("message_time, message_id").Find(&storedRows).Error; err != nil {
		return result, err
	}
	stored := telegramMessagesFromModel(storedRows)
	storedByMessageID := map[int64]domaintelegram.Message{}
	for _, message := range stored {
		storedByMessageID[message.MessageID] = message
	}

	managed := true
	for messageID := range oldBackfillIDs {
		message, ok := storedByMessageID[messageID]
		if !ok || telegramRawBool(message.Raw, "manual") {
			continue
		}
		raw := mergeTelegramRaw(message.Raw, "", &managed)
		if string(raw) != string(message.Raw) {
			message.Raw = raw
			if err := gormrepo.NewTelegramRepository(db).SaveMessage(dbContext(db), &message); err != nil {
				return result, err
			}
		}
	}

	sort.Slice(desiredItems, func(i, j int) bool { return desiredItems[i].MessageID < desiredItems[j].MessageID })
	for _, item := range desiredItems {
		message, created, err := IngestTelegramChannelMessage(db, channel, item, "backfill", &managed, sec, settings)
		if err != nil {
			return result, err
		}
		if created && message != nil {
			result["added"]++
		}
	}

	refs := []string{}
	ids := []uint{}
	for _, message := range stored {
		if telegramRawBool(message.Raw, "manual") {
			continue
		}
		_, wasOldBackfill := oldBackfillIDs[message.MessageID]
		_, isDesired := desiredIDs[message.MessageID]
		if (telegramRawBool(message.Raw, "backfill_managed") || wasOldBackfill) && !isDesired {
			refs = append(refs, TelegramMessageExternalRef(message))
			ids = append(ids, message.ID)
		}
	}
	if len(ids) == 0 {
		return result, nil
	}
	if err := gormrepo.NewTelegramRepository(db).DeleteMessagesByIDsWithRefs(dbContext(db), ids, refs); err != nil {
		return result, err
	}
	result["removed"] = len(ids)
	return result, nil
}

func IngestTelegramChannelMessage(db *gorm.DB, channel *domaintelegram.Channel, item TelegramPublicMessage, ingestSource string, backfillManaged *bool, sec security.Service, settings config.Settings) (*domaintelegram.Message, bool, error) {
	if !channel.CollectFrom.IsZero() && item.MessageTime.Before(channel.CollectFrom) {
		return nil, false, nil
	}
	var existingRow persistmodel.TelegramMessage
	if err := db.Where("channel_id = ? AND message_id = ?", channel.ID, item.MessageID).First(&existingRow).Error; err == nil {
		existing := telegramMessageFromModel(existingRow)
		raw := mergeTelegramRaw(existing.Raw, ingestSource, backfillManaged)
		if string(raw) != string(existing.Raw) {
			existing.Raw = raw
			_ = gormrepo.NewTelegramRepository(db).SaveMessage(dbContext(db), &existing)
		}
		return &existing, false, nil
	}
	message := domaintelegram.Message{
		ChannelID:   channel.ID,
		MessageID:   item.MessageID,
		MessageTime: item.MessageTime,
		Text:        item.Text,
		Raw:         mergeTelegramRaw(item.Raw, ingestSource, backfillManaged),
		UpdatedAt:   time.Now(),
	}
	if err := gormrepo.NewTelegramRepository(db).CreateMessage(dbContext(db), &message); err != nil {
		return nil, false, err
	}
	if err := ApplyTelegramFilter(db, &message, sec, settings); err != nil {
		return nil, false, err
	}
	return &message, true, nil
}

func publicTelegramUsername(normalized string) (string, error) {
	return infratelegram.PublicUsername(normalized)
}

func mergeTelegramRaw(raw domainkernel.JSON, ingestSource string, backfillManaged *bool) domainkernel.JSON {
	var data map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &data)
	}
	if data == nil {
		data = map[string]any{}
	}
	if ingestSource != "" {
		data["ingest_source"] = ingestSource
	}
	if backfillManaged != nil {
		data["backfill_managed"] = *backfillManaged
	}
	return JSON(data)
}

func telegramRawBool(raw domainkernel.JSON, key string) bool {
	var data map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &data) != nil {
		return false
	}
	value, ok := data[key]
	if !ok {
		return false
	}
	if boolValue, ok := value.(bool); ok {
		return boolValue
	}
	if stringValue, ok := value.(string); ok {
		parsed, _ := strconv.ParseBool(stringValue)
		return parsed
	}
	return false
}
