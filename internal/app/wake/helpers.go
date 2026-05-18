package wake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	domaintelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/telegram"
	domainwake "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/wake"
	"github.com/shopspring/decimal"
)

func wakeConfig(plan *domainwake.Plan) map[string]any {
	var out map[string]any
	_ = json.Unmarshal(plan.TriggerConfig, &out)
	if out == nil {
		return map[string]any{}
	}
	return out
}

func stringFromConfig(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func intFromConfig(value any, fallback int) int {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(text, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func stringsFromConfig(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		parts := strings.Split(typed, ",")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if text := strings.TrimSpace(part); text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func indicatorConditionsFromConfig(cfg map[string]any) []map[string]any {
	raw := firstNonNil(cfg["conditions"], cfg["all"], cfg["any"])
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if condition, ok := item.(map[string]any); ok {
			out = append(out, condition)
		}
	}
	if _, ok := cfg["any"]; ok && strings.TrimSpace(stringFromConfig(cfg["mode"])) == "" && strings.TrimSpace(stringFromConfig(cfg["logic"])) == "" {
		cfg["mode"] = "any"
	}
	return out
}

func normalizeIndicatorField(field string) string {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "change_percent", "pct_chg", "change", "change_rate":
		return "change_pct"
	case "last", "latest", "last_price":
		return "price"
	default:
		return strings.ToLower(strings.TrimSpace(field))
	}
}

func latestDailyBarField(ctx context.Context, repo Repository, code string, field string) (decimal.Decimal, bool, error) {
	row, found, err := repo.LatestDailyBar(ctx, code)
	if err != nil || !found {
		return decimal.Zero, false, err
	}
	switch field {
	case "open":
		return row.Open, true, nil
	case "high":
		return row.High, true, nil
	case "low":
		return row.Low, true, nil
	case "close", "price", "last":
		return row.Close, true, nil
	case "volume":
		return row.Volume, true, nil
	case "amount":
		return row.Amount, true, nil
	default:
		return row.Close, true, nil
	}
}

func compareIndicatorValue(_ string, _ string, value decimal.Decimal, threshold decimal.Decimal, operator string) bool {
	switch operator {
	case ">", "gt":
		return value.GreaterThan(threshold)
	case ">=", "gte":
		return value.GreaterThanOrEqual(threshold)
	case "<", "lt":
		return value.LessThan(threshold)
	case "<=", "lte":
		return value.LessThanOrEqual(threshold)
	case "=", "==", "eq":
		return value.Equal(threshold)
	default:
		return value.GreaterThanOrEqual(threshold)
	}
}

func indicatorSummary(code string, field string, value decimal.Decimal, threshold decimal.Decimal, operator string) string {
	if compareIndicatorValue(code, field, value, threshold, operator) {
		return fmt.Sprintf("%s %s=%s satisfied %s %s", code, field, value, operator, threshold)
	}
	return fmt.Sprintf("%s %s=%s did not satisfy %s %s", code, field, value, operator, threshold)
}

func containsAnyKeyword(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, strings.ToLower(strings.TrimSpace(keyword))) {
			return true
		}
	}
	return false
}

func keywordsMatch(text string, keywords []string, mode string) bool {
	if mode == "all" || mode == "and" {
		for _, keyword := range keywords {
			if !strings.Contains(text, strings.ToLower(strings.TrimSpace(keyword))) {
				return false
			}
		}
		return true
	}
	return containsAnyKeyword(text, keywords)
}

func symbolsMatch(message domaintelegram.Message, symbols []string, mode string) bool {
	if mode == "all" || mode == "and" {
		for _, symbol := range symbols {
			if !messageHasSymbol(message, []string{symbol}) {
				return false
			}
		}
		return true
	}
	return messageHasSymbol(message, symbols)
}

func messageHasSymbol(message domaintelegram.Message, symbols []string) bool {
	var related []string
	_ = json.Unmarshal(message.RelatedSymbols, &related)
	text := message.Text
	for _, symbol := range symbols {
		symbol = strings.TrimSpace(symbol)
		if symbol == "" {
			continue
		}
		for _, item := range related {
			if item == symbol {
				return true
			}
		}
		if strings.Contains(strings.ToLower(text), strings.ToLower(symbol)) {
			return true
		}
	}
	return false
}

func (u Usecase) withTx(ctx context.Context, fn func(Repository) error) error {
	if u.tx != nil {
		return u.tx.WithTx(ctx, fn)
	}
	return errors.New("wake unit of work is not configured")
}
