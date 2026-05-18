package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	domainai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/ai"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domaintelegram "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/telegram"
	"net/http"
	"regexp"
	"strings"

	ai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/ai/client"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/marketdata/ashare"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
)

var (
	sixDigitCode = regexp.MustCompile(`\b\d{6}\b`)
	jsonFence    = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")
)

type FilterResult struct {
	Decision       domainkernel.NewsDecision
	Reason         string
	RelatedSymbols []string
}

func SanitizeRelatedSymbols(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		code, err := ashare.EnsureCode(value)
		if err != nil {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

func ExtractRelatedSymbols(text string) []string {
	return SanitizeRelatedSymbols(sixDigitCode.FindAllString(text, -1))
}

func ExtractFilterJSON(text string) (map[string]any, error) {
	cleaned := strings.TrimSpace(text)
	if match := jsonFence.FindStringSubmatch(cleaned); len(match) == 2 {
		cleaned = strings.TrimSpace(match[1])
	}
	if !strings.HasPrefix(cleaned, "{") {
		start := strings.Index(cleaned, "{")
		end := strings.LastIndex(cleaned, "}")
		if start >= 0 && end > start {
			cleaned = cleaned[start : end+1]
		}
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(cleaned), &data); err != nil {
		return nil, err
	}
	return data, nil
}

func FilterMessageWithRole(ctx context.Context, message domaintelegram.Message, role domainai.AgentRole, provider domainai.Provider, sec security.Service, settings config.Settings, httpClient *http.Client) (FilterResult, error) {
	model := provider.DefaultModel
	if role.Model != nil && strings.TrimSpace(*role.Model) != "" {
		model = strings.TrimSpace(*role.Model)
	}
	if strings.TrimSpace(model) == "" {
		return FilterResult{}, fmt.Errorf("news_filter role has no model configured")
	}
	baseMessages := []map[string]string{
		{
			"role": "system",
			"content": fmt.Sprintf(
				"You are %s.\nResponsibility: %s\nInstruction: %s\n\nReturn JSON only: {\"decision\":\"ignore|observe|meeting\",\"reason\":\"...\",\"related_symbols\":[\"...\"]}\nUse semantic judgment only. Do not use keyword heuristics.",
				role.Name,
				role.Responsibility,
				role.PromptTemplate,
			),
		},
		{
			"role": "user",
			"content": fmt.Sprintf(
				"Telegram channel_id: %d\nmessage_id: %d\ntime: %s\nmessage:\n%s",
				message.ChannelID,
				message.MessageID,
				message.MessageTime,
				message.Text,
			),
		},
	}

	attempts := settings.AIJSONMaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	aiClient := ai.Client{Provider: provider, Security: sec, Settings: settings, HTTPClient: httpClient}
	var previousRaw string
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		messages := append([]map[string]string{}, baseMessages...)
		if attempt > 1 {
			preview := previousRaw
			if strings.TrimSpace(preview) == "" {
				preview = "<empty response>"
			}
			messages = append(messages, map[string]string{
				"role":    "user",
				"content": "Your previous response was not valid JSON for the news filter. Return JSON only: {\"decision\":\"ignore|observe|meeting\",\"reason\":\"...\",\"related_symbols\":[\"...\"]}\nPrevious response:\n" + truncateForPrompt(preview, 2000),
			})
		}
		content, err := aiClient.ChatWithContext(ctx, messages, model)
		if err != nil {
			lastErr = err
			continue
		}
		previousRaw = content
		data, err := ExtractFilterJSON(content)
		if err != nil {
			lastErr = err
			continue
		}
		result, err := FilterResultFromJSON(data)
		if err != nil {
			lastErr = err
			continue
		}
		return result, nil
	}
	preview := strings.ReplaceAll(strings.TrimSpace(previousRaw), "\n", " ")
	if preview == "" {
		preview = "<empty response>"
	}
	return FilterResult{}, fmt.Errorf("news_filter returned invalid JSON after retries: %s: %w", truncateForPrompt(preview, 240), lastErr)
}

func FilterResultFromJSON(data map[string]any) (FilterResult, error) {
	decision := domainkernel.NewsDecision(strings.ToLower(strings.TrimSpace(fmt.Sprint(data["decision"]))))
	switch decision {
	case domainkernel.NewsIgnore, domainkernel.NewsObserve, domainkernel.NewsMeeting:
	default:
		return FilterResult{}, fmt.Errorf("invalid news_filter decision %q", decision)
	}
	reason := strings.TrimSpace(fmt.Sprint(data["reason"]))
	if reason == "" || reason == "<nil>" {
		reason = "No reason provided by news_filter."
	}
	rawSymbols, _ := data["related_symbols"].([]any)
	symbols := make([]string, 0, len(rawSymbols))
	for _, raw := range rawSymbols {
		symbols = append(symbols, fmt.Sprint(raw))
	}
	return FilterResult{Decision: decision, Reason: reason, RelatedSymbols: SanitizeRelatedSymbols(symbols)}, nil
}
