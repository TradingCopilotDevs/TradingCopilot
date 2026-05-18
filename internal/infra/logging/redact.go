package logging

import (
	"net/url"
	"strings"
)

const redacted = "[redacted]"

func RedactMap(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return fields
	}
	out := make(map[string]any, len(fields))
	for key, value := range fields {
		out[key] = redactValue(key, value)
	}
	return out
}

func redactValue(key string, value any) any {
	if sensitiveKey(key) {
		return redacted
	}
	switch typed := value.(type) {
	case map[string]any:
		return RedactMap(typed)
	case string:
		return redactURL(typed)
	default:
		return value
	}
}

func sensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, token := range []string{"token", "secret", "password", "hash", "session", "key"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func redactURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.User == nil {
		return value
	}
	parsed.User = url.User(redacted)
	return parsed.String()
}
