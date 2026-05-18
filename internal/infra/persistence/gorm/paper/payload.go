package paper

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

func decimalFromPayload(value any) decimal.Decimal {
	switch v := value.(type) {
	case decimal.Decimal:
		return v
	case int:
		return decimal.NewFromInt(int64(v))
	case int64:
		return decimal.NewFromInt(v)
	case uint:
		return decimal.NewFromInt(int64(v))
	case float64:
		return decimal.NewFromFloat(v)
	case string:
		d, _ := decimal.NewFromString(strings.TrimSpace(v))
		return d
	default:
		d, _ := decimal.NewFromString(fmt.Sprint(v))
		return d
	}
}

func stringFromPayload(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func boolFromPayload(value any, fallback bool) bool {
	if value == nil {
		return fallback
	}
	switch v := value.(type) {
	case bool:
		return v
	case string:
		parsed, err := strconv.ParseBool(v)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func intFromPayload(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint:
		return int(v)
	case float64:
		return int(v)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(v))
		return parsed
	default:
		return 0
	}
}

func uintFromPayload(value any) (uint, bool) {
	switch v := value.(type) {
	case uint:
		return v, true
	case int:
		return uint(v), v > 0
	case int64:
		return uint(v), v > 0
	case float64:
		return uint(v), v > 0
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		return uint(parsed), err == nil && parsed > 0
	default:
		return 0, false
	}
}

func optionalUintPtr(value any) *uint {
	if id, ok := uintFromPayload(value); ok {
		return &id
	}
	return nil
}

func optionalString(value any) *string {
	text := stringFromPayload(value)
	if text == "" {
		return nil
	}
	return &text
}

func optionalTimePtr(value any) *time.Time {
	switch v := value.(type) {
	case time.Time:
		return &v
	case string:
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			return &parsed
		}
	}
	return nil
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil && fmt.Sprint(value) != "" {
			return value
		}
	}
	return nil
}
