package meeting

import (
	"context"
	"encoding/json"
	"fmt"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	"strings"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/ai/client"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	tiktoken "github.com/pkoukk/tiktoken-go"
	"gorm.io/gorm"
)

func reserveMeetingTokens(ctx context.Context, db *gorm.DB, meetingID uint, estimated int, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if estimated <= 0 {
		return nil
	}
	budget := meetingDailyTokenBudget(db, config.Load())
	if budget > 0 {
		used, err := meetingDailyTokenUsage(db)
		if err != nil {
			return err
		}
		if used+estimated > budget {
			return fmt.Errorf("meeting daily token budget exceeded before %s: used=%d estimated=%d budget=%d", label, used, estimated, budget)
		}
	}
	return gormrepo.NewMeetingRepository(db).AdjustTokenBudget(ctx, meetingID, estimated)
}

func adjustMeetingTokenReservation(db *gorm.DB, meetingID uint, reserved int, actual int) error {
	delta := actual - reserved
	if delta == 0 {
		return nil
	}
	return gormrepo.NewMeetingRepository(db).AdjustTokenBudget(dbContext(db), meetingID, delta)
}

func settleMeetingTokenUsage(ctx context.Context, db *gorm.DB, meetingID uint, reserved int, actual int, label string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	delta := actual - reserved
	if delta > 0 {
		if err := reserveMeetingTokens(ctx, db, meetingID, delta, label+"_usage_delta"); err != nil {
			return err
		}
		return nil
	}
	return adjustMeetingTokenReservation(db, meetingID, reserved, actual)
}

func chatPromptTokens(model string, messages []map[string]string) int {
	enc := encodingForModel(model)
	if enc == nil {
		return roughChatTokens(messages, "")
	}
	tokens := 3
	for _, message := range messages {
		tokens += 3
		tokens += len(enc.Encode(message["role"], nil, nil))
		tokens += len(enc.Encode(message["content"], nil, nil))
		if name := strings.TrimSpace(message["name"]); name != "" {
			tokens += 1 + len(enc.Encode(name, nil, nil))
		}
	}
	if tokens < 1 {
		tokens = 1
	}
	return tokens
}

func completionTokens(model string, text string) int {
	enc := encodingForModel(model)
	if enc == nil {
		return roughChatTokens(nil, text)
	}
	tokens := len(enc.Encode(text, nil, nil))
	if tokens < 1 && strings.TrimSpace(text) != "" {
		return 1
	}
	return tokens
}

func roughChatTokens(messages []map[string]string, extra string) int {
	chars := len(extra)
	for _, message := range messages {
		chars += len(message["role"]) + len(message["content"]) + len(message["name"]) + 8
	}
	tokens := chars / 4
	if chars%4 != 0 {
		tokens++
	}
	if tokens < 1 && chars > 0 {
		return 1
	}
	return tokens
}

func actualChatTokens(model string, messages []map[string]string, content string, usage *ai.Usage) int {
	if usage != nil && usage.TotalTokens > 0 {
		return usage.TotalTokens
	}
	if usage != nil && usage.PromptTokens+usage.CompletionTokens > 0 {
		return usage.PromptTokens + usage.CompletionTokens
	}
	return chatPromptTokens(model, messages) + completionTokens(model, content)
}

func tokenUsagePayload(usage *ai.Usage, promptEstimate int, actual int) any {
	if usage != nil {
		return usage
	}
	if actual <= 0 {
		return nil
	}
	completionEstimate := actual - promptEstimate
	if completionEstimate < 0 {
		completionEstimate = 0
	}
	return map[string]any{
		"prompt_tokens":     promptEstimate,
		"completion_tokens": completionEstimate,
		"total_tokens":      actual,
		"estimated":         true,
	}
}

func encodingForModel(model string) *tiktoken.Tiktoken {
	enc, err := tiktoken.EncodingForModel(tokenizerModelAlias(model))
	if err == nil && enc != nil {
		return enc
	}
	enc, err = tiktoken.GetEncoding("cl100k_base")
	if err == nil && enc != nil {
		return enc
	}
	return nil
}

func tokenizerModelAlias(model string) string {
	name := strings.ToLower(strings.TrimSpace(model))
	if name == "" {
		return "gpt-4o-mini"
	}
	segments := strings.FieldsFunc(name, func(r rune) bool {
		return r == '/' || r == ':' || r == ' ' || r == '\t'
	})
	for _, segment := range segments {
		if strings.HasPrefix(segment, "gpt-") || strings.HasPrefix(segment, "o1") || strings.HasPrefix(segment, "o3") || strings.HasPrefix(segment, "o4") {
			return segment
		}
	}
	nonOpenAIPrefixes := []string{
		"deepseek", "qwen", "moonshot", "kimi", "glm", "yi-", "doubao", "baichuan", "ernie", "abab",
		"claude", "gemini", "mistral", "llama", "mixtral", "grok", "gpt-oss", "openrouter",
	}
	for _, candidate := range append([]string{name}, segments...) {
		for _, prefix := range nonOpenAIPrefixes {
			if strings.HasPrefix(candidate, prefix) {
				return "gpt-4o-mini"
			}
		}
	}
	return strings.TrimSpace(model)
}

func meetingDailyTokenBudget(db *gorm.DB, settings config.Settings) int {
	budget := settings.MeetingDailyTokenBudget
	var setting persistmodel.AppSetting
	result := db.Where("key = ?", "MEETING_DAILY_TOKEN_BUDGET").Limit(1).Find(&setting)
	if result.Error != nil || result.RowsAffected == 0 {
		return budget
	}
	var raw any
	if json.Unmarshal(setting.Value, &raw) == nil {
		if obj, ok := raw.(map[string]any); ok {
			raw = obj["value"]
		}
		if parsed := intSetting(raw); parsed == -1 || parsed > 0 {
			budget = parsed
		}
	}
	return budget
}

func meetingDailyTokenUsage(db *gorm.DB) (int, error) {
	return gormrepo.NewMeetingRepository(db).DailyTokenUsage(dbContext(db), time.Now())
}

func tokenBudgetPayload(db *gorm.DB, meetingID uint) map[string]any {
	var meeting persistmodel.Meeting
	_ = db.Select("token_budget").First(&meeting, meetingID).Error
	return map[string]any{"token_budget": meeting.TokenBudget}
}

func refreshMeetingTokenBudget(db *gorm.DB, meeting *domainmeeting.Meeting) {
	if meeting == nil || meeting.ID == 0 {
		return
	}
	var current persistmodel.Meeting
	if db.Select("token_budget").First(&current, meeting.ID).Error == nil {
		meeting.TokenBudget = current.TokenBudget
	}
}

func tokenLabel(parts ...string) string {
	return strings.Join(parts, "/")
}
