package telegram

import (
	"encoding/json"
	"fmt"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/meeting"
	domaintelegram "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/telegram"
	"strings"
	"time"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	persistmodel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/model"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
	infratelegram "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/telegram"
	"gorm.io/gorm"
)

type TelegramFilterResult = infratelegram.FilterResult

func NormalizeTelegramChannelRef(value string) string {
	return infratelegram.NormalizeChannelRef(value)
}

func SanitizeRelatedSymbols(values []string) []string {
	return infratelegram.SanitizeRelatedSymbols(values)
}

func ExtractRelatedSymbols(text string) []string {
	return infratelegram.ExtractRelatedSymbols(text)
}

func ExtractTelegramFilterJSON(text string) (map[string]any, error) {
	return infratelegram.ExtractFilterJSON(text)
}

func FilterTelegramMessageWithRole(db *gorm.DB, message domaintelegram.Message, sec security.Service, settings config.Settings) (TelegramFilterResult, error) {
	var roleRow persistmodel.AgentRole
	if err := db.Where("key = ? AND enabled = ?", "news_filter", true).First(&roleRow).Error; err != nil {
		return TelegramFilterResult{}, fmt.Errorf("news_filter role is not enabled")
	}
	role := agentRoleFromModel(roleRow)
	if role.ProviderID == nil {
		return TelegramFilterResult{}, fmt.Errorf("news_filter role has no available provider")
	}
	var providerRow persistmodel.AiProvider
	if err := db.Preload("APIKeySecret").First(&providerRow, *role.ProviderID).Error; err != nil || !providerRow.Enabled || providerRow.APIKeySecret == nil {
		return TelegramFilterResult{}, fmt.Errorf("news_filter role has no available provider or api key")
	}
	provider := aiProviderFromModel(providerRow)
	model := provider.DefaultModel
	if role.Model != nil && strings.TrimSpace(*role.Model) != "" {
		model = strings.TrimSpace(*role.Model)
	}
	if strings.TrimSpace(model) == "" {
		return TelegramFilterResult{}, fmt.Errorf("news_filter role has no model configured")
	}

	return infratelegram.FilterMessageWithRole(dbContext(db), message, role, provider, sec, settings, runtimeproxy.NewHTTPClient(db, settings, runtimeproxy.ModuleAI, settings.AIChatTimeout))
}

func ApplyTelegramFilter(db *gorm.DB, message *domaintelegram.Message, sec security.Service, settings config.Settings) error {
	result, err := FilterTelegramMessageWithRole(db, *message, sec, settings)
	if err != nil {
		message.FilterDecision = nil
		reason := fmt.Sprintf("message filter call failed: %v", err)
		message.FilterReason = &reason
		message.RelatedSymbols = JSONList(nil)
	} else {
		message.FilterDecision = &result.Decision
		message.FilterReason = &result.Reason
		message.RelatedSymbols = JSON(result.RelatedSymbols)
	}
	key := "news_filter"
	message.FilterModelRoleKey = &key
	now := time.Now()
	message.FilteredAt = &now
	message.UpdatedAt = now
	if err := gormrepo.NewTelegramRepository(db).SaveMessage(dbContext(db), message); err != nil {
		return err
	}
	return nil
}

func ApplyTelegramCompatibilityFilter(db *gorm.DB, message *domaintelegram.Message) error {
	symbols := ExtractRelatedSymbols(message.Text)
	decision := domainkernel.NewsObserve
	reason := "No AI news_filter provider configured; applied Go compatibility filter."
	if len(symbols) == 0 {
		decision = domainkernel.NewsIgnore
		reason = "No AI news_filter provider configured; no A-share symbols detected."
	}
	message.FilterDecision = &decision
	message.FilterReason = &reason
	message.RelatedSymbols = JSON(symbols)
	key := "news_filter"
	message.FilterModelRoleKey = &key
	now := time.Now()
	message.FilteredAt = &now
	message.UpdatedAt = now
	if err := gormrepo.NewTelegramRepository(db).SaveMessage(dbContext(db), message); err != nil {
		return err
	}
	return nil
}

func telegramFilterResultFromJSON(data map[string]any) (TelegramFilterResult, error) {
	return infratelegram.FilterResultFromJSON(data)
}

func TelegramMessageExternalRef(message domaintelegram.Message) string {
	return fmt.Sprintf("telegram_message:%d", message.ID)
}

func EnsureMeetingForTelegramMessage(db *gorm.DB, message *domaintelegram.Message, triggerSource string) (*domainmeeting.Meeting, bool, error) {
	if message.FilterDecision == nil || *message.FilterDecision != domainkernel.NewsMeeting {
		return nil, false, nil
	}
	var channelRow persistmodel.TelegramChannel
	if err := db.First(&channelRow, message.ChannelID).Error; err != nil {
		return nil, false, err
	}
	channel := telegramChannelFromModel(channelRow)
	externalRef := TelegramMessageExternalRef(*message)
	var existing persistmodel.MeetingReference
	if err := db.Where("reference_type = ? AND external_ref = ?", "telegram_message", externalRef).First(&existing).Error; err == nil {
		var meetingRow persistmodel.Meeting
		if db.First(&meetingRow, existing.SourceMeetingID).Error == nil {
			meeting := meetingFromModel(meetingRow)
			return &meeting, false, nil
		}
	}
	symbols := relatedSymbolsFromJSON(message.RelatedSymbols)
	if len(symbols) == 0 {
		symbols = ExtractRelatedSymbols(message.Text)
	}
	symbolPart := "message-event"
	if len(symbols) > 0 {
		limit := len(symbols)
		if limit > 3 {
			limit = 3
		}
		symbolPart = strings.Join(symbols[:limit], ", ")
	}
	meeting, err := createMeeting(db, fmt.Sprintf("Telegram trigger: %s / %s", channel.Title, symbolPart), triggerSource)
	if err != nil {
		return nil, false, err
	}
	content := fmt.Sprintf("Channel %s triggered a meeting.\nFilter reason: %s\nRelated symbols: %s\n\n%s", channel.Title, stringValue(message.FilterReason), strings.Join(symbols, ", "), message.Text)
	_, _ = appendEvent(db, meeting.ID, domainkernel.EventSystem, nil, content, map[string]any{"status": "telegram_triggered", "telegram_message_id": message.ID, "channel_id": channel.ID, "decision": *message.FilterDecision, "related_symbols": symbols})
	note := fmt.Sprintf("%s / message#%d", channel.Title, message.MessageID)
	topic := fmt.Sprintf("Telegram message %s / #%d", channel.Title, message.MessageID)
	summary := message.Text
	if len(summary) > 2000 {
		summary = summary[:2000]
	}
	ref := domainmeeting.Reference{SourceMeetingID: meeting.ID, ReferenceType: "telegram_message", Note: &note, TargetTopicSnapshot: topic, TargetSummarySnapshot: &summary, ExternalRef: &externalRef}
	if err := gormrepo.NewMeetingRepository(db).CreateReference(dbContext(db), &ref); err != nil {
		return nil, false, err
	}
	return meeting, true, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func relatedSymbolsFromJSON(raw []byte) []string {
	var values []string
	if len(raw) == 0 || json.Unmarshal(raw, &values) != nil {
		return nil
	}
	return SanitizeRelatedSymbols(values)
}
