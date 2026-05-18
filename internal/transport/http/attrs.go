package httptransport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func stringAttrValue(attrs map[string]any, keys ...string) (string, bool) {
	value, ok := attrValue(attrs, keys...)
	if !ok || value == nil {
		return "", ok
	}
	return fmt.Sprint(value), true
}

func numberAttrValue(attrs map[string]any, keys ...string) (float64, bool) {
	value, ok := attrValue(attrs, keys...)
	if !ok || value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64)
		return parsed, err == nil
	}
}

func numberAttrValueOrZero(attrs map[string]any, keys ...string) float64 {
	value, ok := numberAttrValue(attrs, keys...)
	if !ok {
		return 0
	}
	return value
}

func uintPtrFromAny(value any) *uint {
	if value == nil || fmt.Sprint(value) == "" {
		return nil
	}
	var parsed uint64
	switch typed := value.(type) {
	case float64:
		parsed = uint64(typed)
	case int:
		parsed = uint64(typed)
	case uint:
		parsed = uint64(typed)
	case string:
		value, err := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return nil
		}
		parsed = value
	default:
		value, err := strconv.ParseUint(strings.TrimSpace(fmt.Sprint(value)), 10, 64)
		if err != nil {
			return nil
		}
		parsed = value
	}
	id := uint(parsed)
	return &id
}

func uintPtrAttr(attrs map[string]any, keys ...string) *uint {
	value, ok := attrValue(attrs, keys...)
	if !ok {
		return nil
	}
	return uintPtrFromAny(value)
}

func uintQuery(r *http.Request, key string) uint {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return 0
	}
	parsed, _ := strconv.ParseUint(value, 10, 64)
	return uint(parsed)
}

func stringSliceAttr(attrs map[string]any, keys ...string) ([]string, bool) {
	value, ok := attrValue(attrs, keys...)
	if !ok || value == nil {
		return nil, ok
	}
	items, ok := value.([]any)
	if !ok {
		return nil, true
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(fmt.Sprint(item))
		if text != "" {
			out = append(out, text)
		}
	}
	return out, true
}

func timePtrAttr(attrs map[string]any, keys ...string) *time.Time {
	value, ok := attrValue(attrs, keys...)
	if !ok || value == nil || fmt.Sprint(value) == "" {
		return nil
	}
	if typed, ok := value.(time.Time); ok {
		return &typed
	}
	parsed, err := time.Parse(time.RFC3339, fmt.Sprint(value))
	if err != nil {
		return nil
	}
	return &parsed
}

func parseAnyTime(value any) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, true
	case string:
		parsed, err := time.Parse(time.RFC3339, typed)
		if err == nil {
			return parsed, true
		}
		parsed, err = time.Parse("2006-01-02 15:04:05", typed)
		if err == nil {
			return parsed, true
		}
		return time.Time{}, false
	default:
		return time.Time{}, false
	}
}

func jsonResourceAttributes(value any) map[string]any {
	attrs, ok := camelizeJSONKeys(value).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	delete(attrs, "id")
	return attrs
}

func fmtSprint(value any) string {
	return fmt.Sprint(value)
}
