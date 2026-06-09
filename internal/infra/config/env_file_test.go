package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestDefaultEnvFileUsesTCEnvFileOverride(t *testing.T) {
	customEnv := filepath.Join(t.TempDir(), "runtime", "app.env")
	t.Setenv("TC_ENV_FILE", customEnv)
	if got := DefaultEnvFile(); got != customEnv {
		t.Fatalf("expected %s, got %s", customEnv, got)
	}
}

func TestLoadMessageSubscriptionListenersInServeOverride(t *testing.T) {
	t.Setenv("TC_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	t.Setenv("MESSAGE_SUBSCRIPTION_LISTENERS_IN_SERVE", "false")
	if Load().MessageSubscriptionListenersInServe {
		t.Fatal("expected MESSAGE_SUBSCRIPTION_LISTENERS_IN_SERVE=false to disable embedded listeners")
	}
}

func TestLoadMeetingDailyTokenBudgetAllowsOnlyUnlimitedOrPositive(t *testing.T) {
	t.Setenv("TC_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))

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

func TestLoadAIDailyCostBudgetAllowsOnlyUnlimitedOrPositive(t *testing.T) {
	t.Setenv("TC_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))

	t.Setenv("AI_DAILY_COST_BUDGET", "-1")
	if got := Load().AIDailyCostBudget; got != -1 {
		t.Fatalf("expected -1 unlimited budget, got %f", got)
	}

	t.Setenv("AI_DAILY_COST_BUDGET", "12.34")
	if got := Load().AIDailyCostBudget; got != 12.34 {
		t.Fatalf("expected positive budget, got %f", got)
	}

	t.Setenv("AI_DAILY_COST_BUDGET", "0")
	if got := Load().AIDailyCostBudget; got != -1 {
		t.Fatalf("expected invalid zero budget to fall back to -1, got %f", got)
	}

	t.Setenv("AI_DAILY_COST_BUDGET", "-2")
	if got := Load().AIDailyCostBudget; got != -1 {
		t.Fatalf("expected values below -1 to fall back to -1, got %f", got)
	}
}

func TestLoadBackupRetentionUsesPositiveDefaults(t *testing.T) {
	t.Setenv("TC_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	settings := Load()
	if settings.BackupRetentionCopies != 7 || settings.BackupRetentionDays != 30 {
		t.Fatalf("expected default backup retention 7 copies / 30 days, got %d / %d", settings.BackupRetentionCopies, settings.BackupRetentionDays)
	}

	t.Setenv("BACKUP_RETENTION_COPIES", "3")
	t.Setenv("BACKUP_RETENTION_DAYS", "14")
	settings = Load()
	if settings.BackupRetentionCopies != 3 || settings.BackupRetentionDays != 14 {
		t.Fatalf("expected configured backup retention 3 copies / 14 days, got %d / %d", settings.BackupRetentionCopies, settings.BackupRetentionDays)
	}

	t.Setenv("BACKUP_RETENTION_COPIES", "0")
	t.Setenv("BACKUP_RETENTION_DAYS", "-1")
	settings = Load()
	if settings.BackupRetentionCopies != 7 || settings.BackupRetentionDays != 30 {
		t.Fatalf("expected invalid backup retention to fall back to defaults, got %d / %d", settings.BackupRetentionCopies, settings.BackupRetentionDays)
	}
}

func TestLoadBackupRestoreDrillInterval(t *testing.T) {
	t.Setenv("TC_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	settings := Load()
	if settings.BackupRestoreDrillInterval != 168*time.Hour {
		t.Fatalf("expected default backup restore drill interval 168h, got %s", settings.BackupRestoreDrillInterval)
	}

	t.Setenv("BACKUP_RESTORE_DRILL_INTERVAL_HOURS", "24")
	settings = Load()
	if settings.BackupRestoreDrillInterval != 24*time.Hour {
		t.Fatalf("expected configured backup restore drill interval 24h, got %s", settings.BackupRestoreDrillInterval)
	}

	t.Setenv("BACKUP_RESTORE_DRILL_INTERVAL_HOURS", "0")
	settings = Load()
	if settings.BackupRestoreDrillInterval != 0 {
		t.Fatalf("expected zero backup restore drill interval to disable scheduling, got %s", settings.BackupRestoreDrillInterval)
	}

	t.Setenv("BACKUP_RESTORE_DRILL_INTERVAL_HOURS", "-1")
	settings = Load()
	if settings.BackupRestoreDrillInterval != 168*time.Hour {
		t.Fatalf("expected negative backup restore drill interval to fall back to 168h, got %s", settings.BackupRestoreDrillInterval)
	}
}

func TestLoadBackupArchiveProviderConfig(t *testing.T) {
	t.Setenv("TC_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	settings := Load()
	if settings.BackupArchiveProvider != "local" {
		t.Fatalf("expected default archive provider local, got %q", settings.BackupArchiveProvider)
	}

	t.Setenv("BACKUP_ARCHIVE_PROVIDER", "s3")
	t.Setenv("BACKUP_ARCHIVE_S3_BUCKET", "tc-backups")
	t.Setenv("BACKUP_ARCHIVE_S3_REGION", "us-east-1")
	t.Setenv("BACKUP_ARCHIVE_S3_ENDPOINT", "https://s3.example.com")
	t.Setenv("BACKUP_ARCHIVE_S3_ACCESS_KEY_ID", "key-id")
	t.Setenv("BACKUP_ARCHIVE_S3_SECRET_ACCESS_KEY", "secret")
	t.Setenv("BACKUP_ARCHIVE_S3_PREFIX", "prod")
	settings = Load()
	if settings.BackupArchiveProvider != "s3" || settings.BackupArchiveS3Bucket != "tc-backups" || settings.BackupArchiveS3Region != "us-east-1" || settings.BackupArchiveS3Prefix != "prod" {
		t.Fatalf("s3 archive config not loaded: %+v", settings)
	}
	if settings.BackupArchiveS3Endpoint != "https://s3.example.com" || settings.BackupArchiveS3AccessKeyID != "key-id" || settings.BackupArchiveS3SecretAccessKey != "secret" {
		t.Fatalf("s3 archive credential metadata not loaded: %+v", settings)
	}

	t.Setenv("BACKUP_ARCHIVE_PROVIDER", "oss")
	t.Setenv("BACKUP_ARCHIVE_OSS_BUCKET", "oss-backups")
	t.Setenv("BACKUP_ARCHIVE_OSS_REGION", "cn-hangzhou")
	t.Setenv("BACKUP_ARCHIVE_OSS_ENDPOINT", "https://oss-cn-hangzhou.aliyuncs.com")
	t.Setenv("BACKUP_ARCHIVE_OSS_ACCESS_KEY_ID", "oss-key")
	t.Setenv("BACKUP_ARCHIVE_OSS_ACCESS_KEY_SECRET", "oss-secret")
	t.Setenv("BACKUP_ARCHIVE_OSS_PREFIX", "prod")
	settings = Load()
	if settings.BackupArchiveProvider != "oss" || settings.BackupArchiveOSSBucket != "oss-backups" || settings.BackupArchiveOSSRegion != "cn-hangzhou" || settings.BackupArchiveOSSPrefix != "prod" {
		t.Fatalf("oss archive config not loaded: %+v", settings)
	}
	if settings.BackupArchiveOSSEndpoint != "https://oss-cn-hangzhou.aliyuncs.com" || settings.BackupArchiveOSSAccessKeyID != "oss-key" || settings.BackupArchiveOSSAccessKeySecret != "oss-secret" {
		t.Fatalf("oss archive credential metadata not loaded: %+v", settings)
	}
}

func TestLoadMarketRealtimeCompatProviderDefault(t *testing.T) {
	t.Setenv("TC_ENV_FILE", filepath.Join(t.TempDir(), "missing.env"))
	if got := Load().MarketRealtimeCompatProvider; got != "tencent" {
		t.Fatalf("expected default realtime compatibility provider tencent, got %s", got)
	}
	t.Setenv("MARKET_REALTIME_COMPAT_PROVIDER", "sina")
	if got := Load().MarketRealtimeCompatProvider; got != "sina" {
		t.Fatalf("expected configured realtime compatibility provider, got %s", got)
	}
}
