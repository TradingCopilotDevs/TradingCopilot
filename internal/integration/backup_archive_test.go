package integration

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appops "github.com/TradingCopilotDevs/TradingCopilot/internal/app/ops"
	infrabackup "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/backup"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
)

func TestS3BackupArchiveIntegrationHarness(t *testing.T) {
	requireIntegration(t, "TC_INTEGRATION_BACKUP_S3")
	settings := config.Settings{
		BackupArchiveProvider:          "s3",
		BackupArchiveS3Bucket:          firstEnv("TC_BACKUP_ARCHIVE_S3_BUCKET", "BACKUP_ARCHIVE_S3_BUCKET"),
		BackupArchiveS3Region:          firstEnv("TC_BACKUP_ARCHIVE_S3_REGION", "BACKUP_ARCHIVE_S3_REGION"),
		BackupArchiveS3Endpoint:        firstEnv("TC_BACKUP_ARCHIVE_S3_ENDPOINT", "BACKUP_ARCHIVE_S3_ENDPOINT"),
		BackupArchiveS3AccessKeyID:     firstEnv("TC_BACKUP_ARCHIVE_S3_ACCESS_KEY_ID", "BACKUP_ARCHIVE_S3_ACCESS_KEY_ID"),
		BackupArchiveS3SecretAccessKey: firstEnv("TC_BACKUP_ARCHIVE_S3_SECRET_ACCESS_KEY", "BACKUP_ARCHIVE_S3_SECRET_ACCESS_KEY"),
		BackupArchiveS3Prefix:          backupArchiveIntegrationPrefix(firstEnv("TC_BACKUP_ARCHIVE_S3_PREFIX", "BACKUP_ARCHIVE_S3_PREFIX"), "s3"),
	}
	if settings.BackupArchiveS3Bucket == "" || settings.BackupArchiveS3Region == "" || settings.BackupArchiveS3AccessKeyID == "" || settings.BackupArchiveS3SecretAccessKey == "" {
		t.Skip("set S3 bucket, region, access key ID, and secret access key to run backup archive integration harness")
	}
	runBackupArchiveHarness(t, settings, "s3")
}

func TestOSSBackupArchiveIntegrationHarness(t *testing.T) {
	requireIntegration(t, "TC_INTEGRATION_BACKUP_OSS")
	settings := config.Settings{
		BackupArchiveProvider:           "oss",
		BackupArchiveOSSBucket:          firstEnv("TC_BACKUP_ARCHIVE_OSS_BUCKET", "BACKUP_ARCHIVE_OSS_BUCKET"),
		BackupArchiveOSSRegion:          firstEnv("TC_BACKUP_ARCHIVE_OSS_REGION", "BACKUP_ARCHIVE_OSS_REGION"),
		BackupArchiveOSSEndpoint:        firstEnv("TC_BACKUP_ARCHIVE_OSS_ENDPOINT", "BACKUP_ARCHIVE_OSS_ENDPOINT"),
		BackupArchiveOSSAccessKeyID:     firstEnv("TC_BACKUP_ARCHIVE_OSS_ACCESS_KEY_ID", "BACKUP_ARCHIVE_OSS_ACCESS_KEY_ID"),
		BackupArchiveOSSAccessKeySecret: firstEnv("TC_BACKUP_ARCHIVE_OSS_ACCESS_KEY_SECRET", "BACKUP_ARCHIVE_OSS_ACCESS_KEY_SECRET"),
		BackupArchiveOSSPrefix:          backupArchiveIntegrationPrefix(firstEnv("TC_BACKUP_ARCHIVE_OSS_PREFIX", "BACKUP_ARCHIVE_OSS_PREFIX"), "oss"),
	}
	if settings.BackupArchiveOSSBucket == "" || settings.BackupArchiveOSSRegion == "" || settings.BackupArchiveOSSEndpoint == "" || settings.BackupArchiveOSSAccessKeyID == "" || settings.BackupArchiveOSSAccessKeySecret == "" {
		t.Skip("set OSS bucket, region, endpoint, access key ID, and access key secret to run backup archive integration harness")
	}
	runBackupArchiveHarness(t, settings, "oss")
}

func runBackupArchiveHarness(t *testing.T, settings config.Settings, provider string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	store := infrabackup.StoreForSettings(settings)
	if store == nil {
		t.Fatalf("expected %s backup archive store for integration settings", provider)
	}
	providerDetail := store.Provider()
	if providerDetail.Key != provider || !providerDetail.External || providerDetail.Root == "" {
		t.Fatalf("unexpected backup archive provider: %+v", providerDetail)
	}

	root := t.TempDir()
	dbPath := filepath.Join(root, "local-dev.db")
	logDir := filepath.Join(root, "logs")
	envPath := filepath.Join(root, ".env")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for filePath, body := range map[string]string{
		dbPath:                           "sqlite snapshot",
		filepath.Join(logDir, "app.log"): "integration log line",
		envPath:                          "APP_ENV=integration",
	} {
		if err := os.WriteFile(filePath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	service := appops.NewBackupService(appops.BackupSettings{
		AppName:                    "TradingCopilot",
		AppEnv:                     "integration",
		DatabaseURL:                "sqlite:///" + dbPath,
		LogDir:                     logDir,
		RuntimeEnvFile:             envPath,
		BackupArchiveProvider:      settings.BackupArchiveProvider,
		BackupArchiveS3Bucket:      settings.BackupArchiveS3Bucket,
		BackupArchiveS3Region:      settings.BackupArchiveS3Region,
		BackupArchiveS3Endpoint:    settings.BackupArchiveS3Endpoint,
		BackupArchiveS3AccessKeyID: settings.BackupArchiveS3AccessKeyID,
		BackupArchiveS3SecretKey:   settings.BackupArchiveS3SecretAccessKey,
		BackupArchiveS3Prefix:      settings.BackupArchiveS3Prefix,
		BackupArchiveOSSBucket:     settings.BackupArchiveOSSBucket,
		BackupArchiveOSSRegion:     settings.BackupArchiveOSSRegion,
		BackupArchiveOSSEndpoint:   settings.BackupArchiveOSSEndpoint,
		BackupArchiveOSSAccessKey:  settings.BackupArchiveOSSAccessKeyID,
		BackupArchiveOSSSecretKey:  settings.BackupArchiveOSSAccessKeySecret,
		BackupArchiveOSSPrefix:     settings.BackupArchiveOSSPrefix,
		BackupRetentionCopies:      100000,
		BackupRetentionDays:        36500,
		BackupRestoreDrillInterval: time.Hour,
	}, func(context.Context) (appops.DatabaseSummary, error) {
		return appops.DatabaseSummary{Backend: "SQLite", Target: dbPath}, nil
	}).WithArchiveStore(store)

	created, err := service.CreateArchive(ctx)
	if err != nil {
		t.Fatalf("%s backup archive create failed: %v", provider, err)
	}
	backupName := strings.TrimSpace(fmt.Sprint(created["backupName"]))
	if backupName == "" {
		t.Fatalf("%s backup archive create did not return backupName: %+v", provider, created)
	}
	defer func() {
		if err := store.DeleteArchive(context.Background(), backupName); err != nil {
			t.Logf("cleanup remote backup archive %s failed: %v", backupName, err)
		}
	}()
	if created["archiveProvider"] != provider {
		t.Fatalf("%s backup archive provider mismatch: %+v", provider, created)
	}
	if _, err := store.StatArchive(ctx, backupName); err != nil {
		t.Fatalf("%s backup archive stat failed: %v", provider, err)
	}
	backups, _, err := store.ListArchives(ctx)
	if err != nil {
		t.Fatalf("%s backup archive list failed: %v", provider, err)
	}
	if !backupArchiveListContains(backups, backupName) {
		t.Fatalf("%s backup archive list did not include %s: %+v", provider, backupName, backups)
	}
	drill, err := service.RestoreDryRun(ctx, backupName)
	if err != nil {
		t.Fatalf("%s backup archive restore dry-run failed: %v", provider, err)
	}
	if drill["status"] != "valid" || drill["archiveProvider"] != provider {
		t.Fatalf("%s backup archive dry-run mismatch: %+v", provider, drill)
	}
}

func backupArchiveIntegrationPrefix(base string, provider string) string {
	parts := []string{}
	if cleaned := strings.Trim(strings.TrimSpace(base), "/"); cleaned != "" {
		parts = append(parts, cleaned)
	}
	parts = append(parts, "integration", provider, time.Now().UTC().Format("20060102-150405.000000000"))
	return path.Join(parts...)
}

func backupArchiveListContains(backups []map[string]any, name string) bool {
	for _, backup := range backups {
		if strings.TrimSpace(fmt.Sprint(backup["name"])) == name {
			return true
		}
	}
	return false
}
