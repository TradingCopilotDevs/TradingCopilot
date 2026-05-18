package telegram

import (
	"context"
	"fmt"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	"sort"
	"strconv"
	"strings"
	"time"
)

type BotSender func(chatID string, text string) error
type MeetingDispatcher func(meeting *domainmeeting.Meeting) error

func (u Usecase) ProcessBotUpdate(ctx context.Context, update map[string]any, sender BotSender, dispatch MeetingDispatcher) (bool, error) {
	message := botMessageFromUpdate(update)
	if message == nil {
		return false, nil
	}
	text := strings.TrimSpace(stringFromMap(message, "text"))
	if text == "" {
		return false, nil
	}
	chatID := botChatID(message)
	if chatID == "" || !u.botChatAllowed(ctx, chatID) {
		return false, nil
	}
	command, argument := normalizeBotCommand(text)
	switch command {
	case "/help":
		return true, sender(chatID, "/meeting <topic> - create a new virtual meeting\n/help - show commands\n/meetings - list recent meetings")
	case "/meetings":
		return true, u.replyBotMeetings(ctx, chatID, sender)
	case "/meeting":
		return true, u.createBotMeeting(ctx, chatID, argument, update, sender, dispatch)
	default:
		return false, nil
	}
}

func (u Usecase) botChatAllowed(ctx context.Context, inboundChatID string) bool {
	chatSecret, ok := u.secret(ctx, domainkernel.SecretKindTelegram, "bot_chat_id")
	if !ok {
		return true
	}
	configuredChatID, err := u.security.DecryptSecret(chatSecret.EncryptedValue)
	if err != nil {
		return true
	}
	return strings.TrimSpace(configuredChatID) == "" || strings.TrimSpace(configuredChatID) == inboundChatID
}

func (u Usecase) replyBotMeetings(ctx context.Context, chatID string, sender BotSender) error {
	meetings, err := u.repo.RecentMeetings(ctx, 5)
	if err != nil {
		return err
	}
	if len(meetings) == 0 {
		return sender(chatID, "No meetings yet.")
	}
	sort.SliceStable(meetings, func(i, j int) bool { return meetings[i].CreatedAt.After(meetings[j].CreatedAt) })
	lines := make([]string, 0, len(meetings))
	for _, meeting := range meetings {
		lines = append(lines, fmt.Sprintf("#%d [%s] %s", meeting.ID, meeting.Status, meeting.Topic))
	}
	return sender(chatID, strings.Join(lines, "\n"))
}

func (u Usecase) createBotMeeting(ctx context.Context, chatID string, topic string, update map[string]any, sender BotSender, dispatch MeetingDispatcher) error {
	topic = strings.TrimSpace(topic)
	if topic == "" {
		return sender(chatID, "Usage: /meeting <topic>")
	}
	var meeting *domainmeeting.Meeting
	if err := u.withTx(ctx, func(repo Repository) error {
		meeting = &domainmeeting.Meeting{Topic: topic, TriggerSource: "telegram_bot", Status: domainkernel.MeetingQueued, Tags: u.service.JSON(nil)}
		if err := repo.CreateMeeting(ctx, meeting); err != nil {
			return err
		}
		if err := repo.AppendMeetingEvent(ctx, &domainmeeting.Event{MeetingID: meeting.ID, Type: domainkernel.EventSystem, Content: "Meeting submitted for execution.", Payload: u.service.JSON(map[string]any{"status": "queued"})}); err != nil {
			return err
		}
		return repo.AppendMeetingEvent(ctx, &domainmeeting.Event{
			MeetingID: meeting.ID,
			Type:      domainkernel.EventSystem,
			Content:   "Telegram Bot triggered a meeting.\n\n" + topic,
			Payload:   u.service.JSON(map[string]any{"status": "telegram_bot_triggered", "chat_id": chatID, "update_id": int64FromAny(update["update_id"])}),
		})
	}); err != nil {
		return err
	}
	if dispatch != nil {
		if err := dispatch(meeting); err != nil {
			return err
		}
	}
	return sender(chatID, fmt.Sprintf("Meeting created: #%d\nTopic: %s", meeting.ID, meeting.Topic))
}

func normalizeBotCommand(text string) (string, string) {
	stripped := strings.TrimSpace(text)
	if !strings.HasPrefix(stripped, "/") {
		return "", stripped
	}
	first, remainder, found := strings.Cut(stripped, " ")
	if !found {
		remainder = ""
	}
	command, _, _ := strings.Cut(first, "@")
	return strings.ToLower(command), strings.TrimSpace(remainder)
}

func botMessageFromUpdate(update map[string]any) map[string]any {
	for _, key := range []string{"message", "edited_message"} {
		if message, ok := update[key].(map[string]any); ok {
			return message
		}
	}
	return nil
}

func botChatID(message map[string]any) string {
	chat, _ := message["chat"].(map[string]any)
	if chat == nil || chat["id"] == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(chat["id"]))
}

func stringFromMap(value map[string]any, key string) string {
	if raw, ok := value[key].(string); ok {
		return raw
	}
	return ""
}

func int64FromAny(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case string:
		parsed, _ := strconv.ParseInt(v, 10, 64)
		return parsed
	case fmt.Stringer:
		parsed, _ := strconv.ParseInt(v.String(), 10, 64)
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
