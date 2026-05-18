package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteEnvOverridesUpdatesExistingKeysAndAppendsNewOnes(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("PUBLIC_BASE_URL=http://localhost:5173\nMEETING_MAX_ROUNDS=6\n# comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnvOverrides(map[string]any{
		"PUBLIC_BASE_URL":    "http://127.0.0.1:5173",
		"MEETING_MAX_ROUNDS": 8,
		"TOOL_RESULT_LIMIT":  400,
	}, envFile); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	text := string(content)
	for _, want := range []string{
		"PUBLIC_BASE_URL=http://127.0.0.1:5173",
		"MEETING_MAX_ROUNDS=8",
		"TOOL_RESULT_LIMIT=400",
		"# comment",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in env file:\n%s", want, text)
		}
	}
}

func TestDefaultEnvFileUsesAIWBEnvFileOverride(t *testing.T) {
	customEnv := filepath.Join(t.TempDir(), "runtime", "app.env")
	t.Setenv("AIWB_ENV_FILE", customEnv)
	if got := DefaultEnvFile(); got != customEnv {
		t.Fatalf("expected %s, got %s", customEnv, got)
	}
}

func TestLoadMessageSubscriptionListenersInServeOverride(t *testing.T) {
	t.Setenv("AIWB_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	t.Setenv("MESSAGE_SUBSCRIPTION_LISTENERS_IN_SERVE", "false")
	if Load().MessageSubscriptionListenersInServe {
		t.Fatal("expected MESSAGE_SUBSCRIPTION_LISTENERS_IN_SERVE=false to disable embedded listeners")
	}
}

func TestLoadMeetingDailyTokenBudgetAllowsOnlyUnlimitedOrPositive(t *testing.T) {
	t.Setenv("AIWB_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))

	t.Setenv("MEETING_DAILY_TOKEN_BUDGET", "-1")
	if got := Load().MeetingDailyTokenBudget; got != -1 {
		t.Fatalf("expected -1 unlimited budget, got %d", got)
	}

	t.Setenv("MEETING_DAILY_TOKEN_BUDGET", "123")
	if got := Load().MeetingDailyTokenBudget; got != 123 {
		t.Fatalf("expected positive budget, got %d", got)
	}

	t.Setenv("MEETING_DAILY_TOKEN_BUDGET", "0")
	if got := Load().MeetingDailyTokenBudget; got != -1 {
		t.Fatalf("expected invalid zero budget to fall back to -1, got %d", got)
	}

	t.Setenv("MEETING_DAILY_TOKEN_BUDGET", "-2")
	if got := Load().MeetingDailyTokenBudget; got != -1 {
		t.Fatalf("expected values below -1 to fall back to -1, got %d", got)
	}
}

func TestLoadMarketRealtimeCompatProviderDefault(t *testing.T) {
	t.Setenv("AIWB_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	if got := Load().MarketRealtimeCompatProvider; got != "tencent" {
		t.Fatalf("expected default realtime compatibility provider tencent, got %s", got)
	}
	t.Setenv("MARKET_REALTIME_COMPAT_PROVIDER", "sina")
	if got := Load().MarketRealtimeCompatProvider; got != "sina" {
		t.Fatalf("expected configured realtime compatibility provider, got %s", got)
	}
}
