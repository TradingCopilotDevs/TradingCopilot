package ai

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func attrValue(attrs map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		value, ok := attrs[key]
		if ok {
			return value, true
		}
	}
	return nil, false
}

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
