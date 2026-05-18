package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func DefaultEnvFile() string {
	return env("AIWB_ENV_FILE", ".env")
}

func WriteEnvOverrides(updates map[string]any, envFile string) error {
	if strings.TrimSpace(envFile) == "" {
		envFile = DefaultEnvFile()
	}
	normalized := map[string]string{}
	for key, value := range updates {
		envKey := strings.TrimSpace(key)
		if envKey == "" {
			continue
		}
		normalized[envKey] = strings.TrimSpace(fmt.Sprint(value))
	}
	if len(normalized) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(envFile), 0o755); err != nil {
		return err
	}
	content, err := os.ReadFile(envFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	lines := splitEnvLines(string(content))
	remaining := make(map[string]string, len(normalized))
	for key, value := range normalized {
		remaining[key] = value
	}
	out := make([]string, 0, len(lines)+len(remaining)+1)
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == "" || strings.HasPrefix(stripped, "#") || !strings.Contains(line, "=") {
			out = append(out, line)
			continue
		}
		key, _, _ := strings.Cut(line, "=")
		envKey := strings.TrimSpace(key)
		if value, ok := remaining[envKey]; ok {
			out = append(out, envKey+"="+value)
			delete(remaining, envKey)
			continue
		}
		out = append(out, line)
	}
	if len(remaining) > 0 {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		keys := make([]string, 0, len(remaining))
		for key := range remaining {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			out = append(out, key+"="+remaining[key])
		}
	}
	return os.WriteFile(envFile, []byte(strings.Join(out, "\n")+"\n"), 0o644)
}

func splitEnvLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}
