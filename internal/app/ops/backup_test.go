package ops

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSafeZipExtractPathRejectsUnsafeEntries(t *testing.T) {
	root := t.TempDir()
	for _, entry := range []string{
		"../evil.txt",
		"/absolute.txt",
		`C:\evil.txt`,
		"database/../evil.txt",
		"database/./snapshot.db",
	} {
		if target, ok := safeZipExtractPath(root, entry); ok {
			t.Fatalf("safeZipExtractPath(%q) = %q, want rejected", entry, target)
		}
	}
	target, ok := safeZipExtractPath(root, "database/tradingcopilot.db")
	if !ok {
		t.Fatalf("safeZipExtractPath rejected valid database entry")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(rootAbs, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("safeZipExtractPath escaped root: root=%q target=%q rel=%q err=%v", rootAbs, target, rel, err)
	}
}

func TestBackupRetentionPrunesOldArchivesAndSandboxDirs(t *testing.T) {
	backupDir := t.TempDir()
	now := time.Now().UTC()
	for index := 0; index < 9; index++ {
		name := fmt.Sprintf("tradingcopilot-backup-202601%02d-000000.zip", index+1)
		path := filepath.Join(backupDir, name)
		if err := os.WriteFile(path, []byte("backup"), 0o600); err != nil {
			t.Fatal(err)
		}
		modTime := now.AddDate(0, 0, -index)
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}
	drillRoot := filepath.Join(backupDir, "restore-drills")
	oldDrill := filepath.Join(drillRoot, "old")
	recentDrill := filepath.Join(drillRoot, "recent")
	for _, dir := range []string{oldDrill, recentDrill} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := now.AddDate(0, 0, -8)
	recentTime := now.AddDate(0, 0, -1)
	if err := os.Chtimes(oldDrill, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(recentDrill, recentTime, recentTime); err != nil {
		t.Fatal(err)
	}

	backups, _, err := listBackupArchives(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	run := pruneBackupArtifacts(backupDir, backups, 3, 5, now)
	if run["status"] != "applied" {
		t.Fatalf("retention status = %#v", run["status"])
	}
	if got := run["prunedBackupCount"]; got != 3 {
		t.Fatalf("pruned backup count = %#v, want 3; run=%+v", got, run)
	}
	if got := run["prunedSandboxDirCount"]; got != 1 {
		t.Fatalf("pruned sandbox dir count = %#v, want 1; run=%+v", got, run)
	}
	remaining, _, err := listBackupArchives(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 6 {
		t.Fatalf("remaining backups = %d, want 6", len(remaining))
	}
	if _, err := os.Stat(oldDrill); !os.IsNotExist(err) {
		t.Fatalf("old restore drill should be pruned, stat err=%v", err)
	}
	if _, err := os.Stat(recentDrill); err != nil {
		t.Fatalf("recent restore drill should remain: %v", err)
	}
}

func TestBackupServiceCreatesArchiveAndRunsLatestRestoreDrill(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "local-dev.db")
	logDir := filepath.Join(root, "logs")
	envPath := filepath.Join(root, ".env")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		dbPath:                           "sqlite snapshot",
		filepath.Join(logDir, "app.log"): "log line",
		envPath:                          "APP_ENV=test",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	service := NewBackupService(BackupSettings{
		AppName:                    "TradingCopilot",
		AppEnv:                     "test",
		DatabaseURL:                "sqlite:///" + dbPath,
		LogDir:                     logDir,
		RuntimeEnvFile:             envPath,
		BackupRetentionCopies:      7,
		BackupRetentionDays:        30,
		BackupRestoreDrillInterval: time.Hour,
	}, func(context.Context) (DatabaseSummary, error) {
		return DatabaseSummary{Backend: "SQLite", Target: dbPath}, nil
	})

	created, err := service.CreateArchive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if created["status"] != "created" || created["backupName"] == "" {
		t.Fatalf("backup create result = %+v", created)
	}
	drill, err := service.RunLatestRestoreDrill(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if drill["status"] != "valid" {
		t.Fatalf("scheduled restore drill status = %#v; drill=%+v", drill["status"], drill)
	}
	if drill["scheduled"] != true || drill["taskType"] != RestoreDrillTaskType {
		t.Fatalf("scheduled restore drill metadata missing: %+v", drill)
	}
	report, err := service.Report(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report["restoreDrill"] != "drilled" {
		t.Fatalf("report restoreDrill = %#v, want drilled", report["restoreDrill"])
	}
	if report["archiveProvider"] != "local_filesystem" {
		t.Fatalf("report archive provider = %#v", report["archiveProvider"])
	}
	providerDetail, ok := report["archiveProviderDetail"].(map[string]any)
	if !ok || providerDetail["kind"] != "local" || providerDetail["supportsRead"] != true || providerDetail["supportsDelete"] != true {
		t.Fatalf("archive provider detail mismatch: %+v", report["archiveProviderDetail"])
	}
	if providerDetail["configuredProvider"] != "local" || providerDetail["externalReady"] != false {
		t.Fatalf("local provider external diagnostics mismatch: %+v", providerDetail)
	}
	if created["archiveProvider"] != "local_filesystem" || drill["archiveProvider"] != "local_filesystem" {
		t.Fatalf("create/drill archive provider missing: created=%+v drill=%+v", created["archiveProvider"], drill["archiveProvider"])
	}
}

func TestBackupServiceReportsMissingS3ArchiveConfig(t *testing.T) {
	service := NewBackupService(BackupSettings{
		DatabaseURL:           "sqlite:///" + filepath.Join(t.TempDir(), "local-dev.db"),
		BackupArchiveProvider: "s3",
		BackupArchiveS3Bucket: "tradingcopilot-backups",
	}, nil)

	report, err := service.Report(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	providerDetail, ok := report["archiveProviderDetail"].(map[string]any)
	if !ok {
		t.Fatalf("archive provider detail missing: %+v", report["archiveProviderDetail"])
	}
	missing, ok := providerDetail["missingExternalConfig"].([]string)
	if !ok {
		t.Fatalf("missing external config type mismatch: %+v", providerDetail["missingExternalConfig"])
	}
	if providerDetail["archiveProvider"] != nil {
		t.Fatalf("provider detail should not include nested archive provider key: %+v", providerDetail)
	}
	if providerDetail["configuredProvider"] != "s3" || providerDetail["configuredExternalProvider"] != "s3" || providerDetail["externalReady"] != false || providerDetail["status"] != "warning" {
		t.Fatalf("s3 provider diagnostics mismatch: %+v", providerDetail)
	}
	for _, key := range []string{"BACKUP_ARCHIVE_S3_ACCESS_KEY_ID", "BACKUP_ARCHIVE_S3_REGION", "BACKUP_ARCHIVE_S3_SECRET_ACCESS_KEY"} {
		if !containsString(missing, key) {
			t.Fatalf("missing %s in diagnostics: %+v", key, missing)
		}
	}
}

func TestBackupServiceReportsActiveS3ArchiveStoreWhenInjected(t *testing.T) {
	store := &recordingArchiveStore{
		root: "s3://tc-backups/prod",
		provider: BackupArchiveProvider{
			Key:            "s3",
			Kind:           "object_storage",
			Status:         "ready",
			Root:           "s3://tc-backups/prod",
			External:       true,
			SupportsList:   true,
			SupportsWrite:  true,
			SupportsRead:   true,
			SupportsDelete: true,
		},
	}
	service := NewBackupService(BackupSettings{
		DatabaseURL:                "sqlite:///" + filepath.Join(t.TempDir(), "local-dev.db"),
		BackupArchiveProvider:      "s3",
		BackupArchiveS3Bucket:      "tc-backups",
		BackupArchiveS3Region:      "us-east-1",
		BackupArchiveS3AccessKeyID: "key-id",
		BackupArchiveS3SecretKey:   "secret",
		BackupArchiveS3Prefix:      "prod",
	}, nil).WithArchiveStore(store)

	report, err := service.Report(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report["archiveProvider"] != "s3" || report["backupDir"] != "s3://tc-backups/prod" {
		t.Fatalf("s3 archive store should be active: %+v", report)
	}
	providerDetail := report["archiveProviderDetail"].(map[string]any)
	if providerDetail["status"] != "ready" || providerDetail["external"] != true || providerDetail["externalReady"] != true {
		t.Fatalf("s3 provider detail mismatch: %+v", providerDetail)
	}
	if !strings.Contains(fmt.Sprint(providerDetail["setupHint"]), "S3 archive store is active") {
		t.Fatalf("s3 setup hint should describe active store: %+v", providerDetail)
	}
	if metadataDir := fmt.Sprint(report["backupMetadataDir"]); strings.HasPrefix(metadataDir, "s3://") || metadataDir == "" {
		t.Fatalf("external archive metadata dir must remain local, got %q", metadataDir)
	}
}

func TestBackupServiceReportsActiveOSSArchiveStoreWhenInjected(t *testing.T) {
	store := &recordingArchiveStore{
		root: "oss://tc-backups/prod",
		provider: BackupArchiveProvider{
			Key:            "oss",
			Kind:           "object_storage",
			Status:         "ready",
			Root:           "oss://tc-backups/prod",
			External:       true,
			SupportsList:   true,
			SupportsWrite:  true,
			SupportsRead:   true,
			SupportsDelete: true,
		},
	}
	service := NewBackupService(BackupSettings{
		DatabaseURL:               "sqlite:///" + filepath.Join(t.TempDir(), "local-dev.db"),
		BackupArchiveProvider:     "oss",
		BackupArchiveOSSBucket:    "tc-backups",
		BackupArchiveOSSRegion:    "cn-hangzhou",
		BackupArchiveOSSEndpoint:  "https://oss-cn-hangzhou.aliyuncs.com",
		BackupArchiveOSSAccessKey: "key-id",
		BackupArchiveOSSSecretKey: "secret",
		BackupArchiveOSSPrefix:    "prod",
	}, nil).WithArchiveStore(store)

	report, err := service.Report(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report["archiveProvider"] != "oss" || report["backupDir"] != "oss://tc-backups/prod" {
		t.Fatalf("oss archive store should be active: %+v", report)
	}
	providerDetail := report["archiveProviderDetail"].(map[string]any)
	if providerDetail["status"] != "ready" || providerDetail["external"] != true || providerDetail["externalReady"] != true {
		t.Fatalf("oss provider detail mismatch: %+v", providerDetail)
	}
	if !strings.Contains(fmt.Sprint(providerDetail["setupHint"]), "OSS archive store is active") {
		t.Fatalf("oss setup hint should describe active store: %+v", providerDetail)
	}
	if metadataDir := fmt.Sprint(report["backupMetadataDir"]); strings.HasPrefix(metadataDir, "oss://") || metadataDir == "" {
		t.Fatalf("external archive metadata dir must remain local, got %q", metadataDir)
	}
}

func TestBackupServiceReportsReadyOSSArchiveConfigWithoutChangingLocalStore(t *testing.T) {
	service := NewBackupService(BackupSettings{
		DatabaseURL:                "sqlite:///" + filepath.Join(t.TempDir(), "local-dev.db"),
		BackupArchiveProvider:      "oss",
		BackupArchiveOSSBucket:     "tc-backups",
		BackupArchiveOSSRegion:     "cn-hangzhou",
		BackupArchiveOSSEndpoint:   "https://oss-cn-hangzhou.aliyuncs.com",
		BackupArchiveOSSAccessKey:  "key-id",
		BackupArchiveOSSSecretKey:  "secret",
		BackupArchiveOSSPrefix:     "prod",
		BackupRestoreDrillInterval: time.Hour,
		BackupRetentionCopies:      7,
		BackupRetentionDays:        30,
	}, nil)

	report, err := service.Report(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report["archiveProvider"] != "local_filesystem" {
		t.Fatalf("external diagnostics should keep local archive store active, got %#v", report["archiveProvider"])
	}
	providerDetail := report["archiveProviderDetail"].(map[string]any)
	missing, ok := providerDetail["missingExternalConfig"].([]string)
	if !ok {
		t.Fatalf("missing external config type mismatch: %+v", providerDetail["missingExternalConfig"])
	}
	if len(missing) != 0 {
		t.Fatalf("ready oss config should not report missing env: %+v", missing)
	}
	if providerDetail["configuredProvider"] != "oss" || providerDetail["configuredExternalProvider"] != "oss" || providerDetail["externalReady"] != true || providerDetail["status"] != "warning" {
		t.Fatalf("oss provider diagnostics mismatch: %+v", providerDetail)
	}
	if providerDetail["supportsWrite"] != true || providerDetail["external"] != false {
		t.Fatalf("local store capability flags should remain visible: %+v", providerDetail)
	}
}

func TestBackupServiceRunLatestRestoreDrillSkipsWithoutBackups(t *testing.T) {
	service := NewBackupService(BackupSettings{
		DatabaseURL:                "sqlite:///" + filepath.Join(t.TempDir(), "local-dev.db"),
		BackupRestoreDrillInterval: time.Hour,
	}, nil)
	result, err := service.RunLatestRestoreDrill(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "skipped" || result["reason"] != "no_backups" {
		t.Fatalf("restore drill skip result = %+v", result)
	}
}

func TestBackupRetentionUsesArchiveStoreDelete(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	store := &recordingArchiveStore{
		root: "/remote/tradingcopilot/backups",
		provider: BackupArchiveProvider{
			Key:            "test_object_store",
			Kind:           "object_storage",
			Status:         "ready",
			Root:           "bucket/prefix",
			External:       true,
			SupportsList:   true,
			SupportsWrite:  true,
			SupportsRead:   true,
			SupportsDelete: true,
		},
	}
	backups := []map[string]any{
		{"name": "new.zip", "createdAt": now},
		{"name": "old.zip", "createdAt": now.AddDate(0, 0, -10)},
	}
	run := pruneBackupArtifactsWithStore(context.Background(), store, store.Root(), backups, 1, 5, now)
	if run["status"] != "applied" || run["prunedBackupCount"] != 1 {
		t.Fatalf("retention run mismatch: %+v", run)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "old.zip" {
		t.Fatalf("store delete calls mismatch: %+v", store.deleted)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type recordingArchiveStore struct {
	root     string
	provider BackupArchiveProvider
	deleted  []string
}

func (s *recordingArchiveStore) Provider() BackupArchiveProvider {
	if s.provider.Root == "" {
		s.provider.Root = s.root
	}
	return s.provider
}

func (s *recordingArchiveStore) Root() string { return s.root }

func (s *recordingArchiveStore) EnsureRoot(context.Context) error { return nil }

func (s *recordingArchiveStore) ListArchives(context.Context) ([]map[string]any, map[string]any, error) {
	return nil, nil, nil
}

func (s *recordingArchiveStore) CreateArchive(context.Context, string) (io.WriteCloser, string, error) {
	return nil, "", os.ErrInvalid
}

func (s *recordingArchiveStore) StatArchive(context.Context, string) (map[string]any, error) {
	return nil, os.ErrInvalid
}

func (s *recordingArchiveStore) ResolveArchive(context.Context, string) (string, func(), error) {
	return "", nil, os.ErrInvalid
}

func (s *recordingArchiveStore) DeleteArchive(_ context.Context, name string) error {
	s.deleted = append(s.deleted, name)
	return nil
}
