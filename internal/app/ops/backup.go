package ops

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const RestoreDrillTaskType = "run_backup_restore_drill"

type BackupSettings struct {
	AppName                    string
	AppEnv                     string
	DatabaseURL                string
	LogDir                     string
	RuntimeEnvFile             string
	BackupArchiveProvider      string
	BackupArchiveS3Bucket      string
	BackupArchiveS3Region      string
	BackupArchiveS3Endpoint    string
	BackupArchiveS3AccessKeyID string
	BackupArchiveS3SecretKey   string
	BackupArchiveS3Prefix      string
	BackupArchiveOSSBucket     string
	BackupArchiveOSSRegion     string
	BackupArchiveOSSEndpoint   string
	BackupArchiveOSSAccessKey  string
	BackupArchiveOSSSecretKey  string
	BackupArchiveOSSPrefix     string
	BackupRetentionCopies      int
	BackupRetentionDays        int
	BackupRestoreDrillInterval time.Duration
}

type DatabaseSummary struct {
	Backend any
	Target  any
}

type DatabaseSummaryProvider func(context.Context) (DatabaseSummary, error)

type BackupArchiveProvider struct {
	Key                        string
	Kind                       string
	Status                     string
	Root                       string
	External                   bool
	SupportsList               bool
	SupportsWrite              bool
	SupportsRead               bool
	SupportsDelete             bool
	ConfiguredProvider         string
	ConfiguredExternalProvider string
	ExternalReady              bool
	MissingExternalConfig      []string
	SetupHint                  string
}

type BackupArchiveStore interface {
	Provider() BackupArchiveProvider
	Root() string
	EnsureRoot(context.Context) error
	ListArchives(context.Context) ([]map[string]any, map[string]any, error)
	CreateArchive(context.Context, string) (io.WriteCloser, string, error)
	StatArchive(context.Context, string) (map[string]any, error)
	ResolveArchive(context.Context, string) (string, func(), error)
	DeleteArchive(context.Context, string) error
}

type LocalBackupArchiveStore struct {
	root string
}

func NewLocalBackupArchiveStore(root string) LocalBackupArchiveStore {
	return LocalBackupArchiveStore{root: filepath.Clean(strings.TrimSpace(root))}
}

func (s LocalBackupArchiveStore) Provider() BackupArchiveProvider {
	return BackupArchiveProvider{
		Key:            "local_filesystem",
		Kind:           "local",
		Status:         "ready",
		Root:           s.Root(),
		External:       false,
		SupportsList:   true,
		SupportsWrite:  true,
		SupportsRead:   true,
		SupportsDelete: true,
	}
}

func (s LocalBackupArchiveStore) Root() string {
	if s.root == "" || s.root == "." {
		return "backups"
	}
	return s.root
}

func (s LocalBackupArchiveStore) EnsureRoot(context.Context) error {
	return os.MkdirAll(s.Root(), 0o755)
}

func (s LocalBackupArchiveStore) ListArchives(context.Context) ([]map[string]any, map[string]any, error) {
	return listBackupArchives(s.Root())
}

func (s LocalBackupArchiveStore) CreateArchive(ctx context.Context, name string) (io.WriteCloser, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if err := s.EnsureRoot(ctx); err != nil {
		return nil, "", err
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "" || !strings.HasSuffix(strings.ToLower(name), ".zip") {
		return nil, "", os.ErrInvalid
	}
	archivePath := filepath.Join(s.Root(), name)
	if !pathInsideRoot(s.Root(), archivePath) {
		return nil, "", os.ErrInvalid
	}
	file, err := os.Create(archivePath)
	if err != nil {
		return nil, "", err
	}
	return file, archivePath, nil
}

func (s LocalBackupArchiveStore) StatArchive(ctx context.Context, name string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "" || !strings.HasSuffix(strings.ToLower(name), ".zip") {
		return nil, os.ErrInvalid
	}
	archivePath := filepath.Join(s.Root(), name)
	if !pathInsideRoot(s.Root(), archivePath) {
		return nil, os.ErrInvalid
	}
	stat, err := os.Stat(archivePath)
	if err != nil {
		return nil, err
	}
	return backupArchiveMetadata(name, archivePath, stat), nil
}

func (s LocalBackupArchiveStore) ResolveArchive(ctx context.Context, name string) (string, func(), error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "" || !strings.HasSuffix(strings.ToLower(name), ".zip") {
		return "", nil, os.ErrInvalid
	}
	archivePath := filepath.Join(s.Root(), name)
	if !pathInsideRoot(s.Root(), archivePath) {
		return "", nil, os.ErrInvalid
	}
	return archivePath, func() {}, nil
}

func (s LocalBackupArchiveStore) DeleteArchive(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "" || !strings.HasSuffix(strings.ToLower(name), ".zip") {
		return os.ErrInvalid
	}
	target := filepath.Join(s.Root(), name)
	if !pathInsideRoot(s.Root(), target) {
		return os.ErrInvalid
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

type BackupService struct {
	settings        BackupSettings
	databaseSummary DatabaseSummaryProvider
	archiveStore    BackupArchiveStore
	now             func() time.Time
}

func NewBackupService(settings BackupSettings, databaseSummary DatabaseSummaryProvider) BackupService {
	service := BackupService{settings: settings, databaseSummary: databaseSummary, now: time.Now}
	service.archiveStore = NewLocalBackupArchiveStore(service.defaultBackupDir())
	return service
}

func (s BackupService) WithArchiveStore(store BackupArchiveStore) BackupService {
	if store != nil {
		s.archiveStore = store
	}
	return s
}

func (s BackupService) Report(ctx context.Context) (map[string]any, error) {
	summary, err := s.loadDatabaseSummary(ctx)
	if err != nil {
		return nil, err
	}
	store := s.store()
	backupDir := store.Root()
	metadataDir := s.metadataDir()
	backups, latest, err := store.ListArchives(ctx)
	if err != nil {
		return nil, err
	}
	status := "manual"
	if latest != nil {
		status = "ready"
	}
	restoreDrill := backupRestoreDrillStatus(latest, metadataDir)
	copies, days := s.backupRetentionConfig()
	archiveProvider := s.archiveProviderDetail()
	return map[string]any{
		"generatedAt":              s.now(),
		"status":                   status,
		"databaseBackend":          summary.Backend,
		"databaseTarget":           summary.Target,
		"backupDir":                backupDir,
		"backupMetadataDir":        metadataDir,
		"archiveProvider":          archiveProvider.Key,
		"archiveProviderDetail":    archiveProviderMap(archiveProvider),
		"latestBackup":             latest,
		"backups":                  backups,
		"retentionPolicy":          "automatic_rotation",
		"retentionPolicyDetail":    backupRetentionPolicy(backups, copies, days, s.now()),
		"retentionLastRun":         nullableMap(latestBackupRetentionRun(metadataDir)),
		"restoreDrill":             restoreDrill["status"],
		"restoreDrillDetail":       restoreDrill,
		"restoreDrillSchedule":     s.restoreDrillSchedule(),
		"restoreDrillScheduleHint": "Redis scheduler enqueues run_backup_restore_drill when MEETING_DISPATCH_MODE is not local.",
		"supportedActions": []string{
			"database_snapshot",
			"runtime_env_export",
			"logs_archive",
			"restore_dry_run",
			"scheduled_restore_drill",
			"archive_provider_local",
			"archive_provider_s3",
			"archive_provider_oss",
			"archive_provider_external_config_check",
		},
	}, nil
}

func (s BackupService) CreateArchive(ctx context.Context) (map[string]any, error) {
	report, err := s.Report(ctx)
	if err != nil {
		return nil, err
	}
	store := s.store()
	backupDir := store.Root()
	metadataDir := s.metadataDir()
	if err := store.EnsureRoot(ctx); err != nil {
		return nil, err
	}
	now := s.now()
	name := "tradingcopilot-backup-" + now.Format("20060102-150405") + ".zip"
	file, archivePath, err := store.CreateArchive(ctx, name)
	if err != nil {
		return nil, err
	}
	zipWriter := zip.NewWriter(file)
	included := []map[string]any{}
	notes := []string{}
	addFile := func(zipName string, sourcePath string) {
		info, err := addZipFile(zipWriter, zipName, sourcePath)
		if err != nil {
			notes = append(notes, zipName+": "+err.Error())
			return
		}
		included = append(included, info)
	}

	if envFile := strings.TrimSpace(s.settings.RuntimeEnvFile); envFile != "" {
		addFile("runtime/"+filepath.Base(envFile), envFile)
	}
	if dbPath, ok := sqliteDatabasePath(s.settings.DatabaseURL); ok {
		addFile("database/"+filepath.Base(dbPath), dbPath)
		for _, suffix := range []string{"-wal", "-shm"} {
			if _, err := os.Stat(dbPath + suffix); err == nil {
				addFile("database/"+filepath.Base(dbPath+suffix), dbPath+suffix)
			}
		}
	} else {
		notes = append(notes, "database snapshot requires external database backup tooling for this backend")
	}
	for _, logPath := range listLogFilesForBackup(s.settings.LogDir) {
		addFile("logs/"+filepath.Base(logPath), logPath)
	}

	manifest := map[string]any{
		"generatedAt":       now,
		"appName":           s.settings.AppName,
		"appEnv":            s.settings.AppEnv,
		"databaseBackend":   report["databaseBackend"],
		"databaseTarget":    report["databaseTarget"],
		"runtimeEnvFile":    s.settings.RuntimeEnvFile,
		"logDir":            s.settings.LogDir,
		"included":          included,
		"notes":             notes,
		"restoreStatus":     "manual_restore_required",
		"manifestVersion":   1,
		"requestAuditScope": "archive path and metadata only",
		"archiveProvider":   archiveProviderMap(s.archiveProviderDetail()),
	}
	if err := addZipJSON(zipWriter, "manifest.json", manifest); err != nil {
		_ = zipWriter.Close()
		_ = file.Close()
		return nil, err
	}
	if err := zipWriter.Close(); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	archiveStat, err := store.StatArchive(ctx, name)
	if err != nil {
		return nil, err
	}
	backups, _, err := store.ListArchives(ctx)
	if err != nil {
		notes = append(notes, "retention list failed: "+err.Error())
	}
	retentionRun := map[string]any{}
	if err == nil {
		copies, days := s.backupRetentionConfig()
		retentionRun = pruneBackupArtifactsWithStore(ctx, store, metadataDir, backups, copies, days, now)
		if err := saveBackupRetentionRun(metadataDir, retentionRun); err != nil {
			notes = append(notes, "retention record failed: "+err.Error())
		}
	}
	return map[string]any{
		"status":                "created",
		"backupName":            name,
		"backupPath":            archivePath,
		"backupDir":             backupDir,
		"backupMetadataDir":     metadataDir,
		"archiveProvider":       s.archiveProviderDetail().Key,
		"archiveProviderDetail": archiveProviderMap(s.archiveProviderDetail()),
		"sizeBytes":             archiveStat["sizeBytes"],
		"createdAt":             now,
		"included":              included,
		"notes":                 notes,
		"databaseBackend":       report["databaseBackend"],
		"databaseTarget":        report["databaseTarget"],
		"retentionRun":          nullableMap(retentionRun),
	}, nil
}

func (s BackupService) RestoreDryRun(ctx context.Context, backupName string) (map[string]any, error) {
	result, err := s.inspectArchive(ctx, backupName)
	if err != nil {
		return nil, err
	}
	if err := saveBackupRestoreDrill(s.metadataDir(), result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s BackupService) RunLatestRestoreDrill(ctx context.Context) (map[string]any, error) {
	if s.settings.BackupRestoreDrillInterval <= 0 {
		return map[string]any{
			"status":      "skipped",
			"reason":      "disabled",
			"backupDir":   s.BackupDir(),
			"metadataDir": s.metadataDir(),
			"checkedAt":   s.now(),
			"taskType":    RestoreDrillTaskType,
			"destructive": false,
		}, nil
	}
	backups, latest, err := s.store().ListArchives(ctx)
	if err != nil {
		return nil, err
	}
	if latest == nil {
		return map[string]any{
			"status":      "skipped",
			"reason":      "no_backups",
			"backupDir":   s.BackupDir(),
			"metadataDir": s.metadataDir(),
			"backupCount": len(backups),
			"checkedAt":   s.now(),
			"taskType":    RestoreDrillTaskType,
			"destructive": false,
		}, nil
	}
	name := strings.TrimSpace(fmt.Sprint(latest["name"]))
	result, err := s.RestoreDryRun(ctx, name)
	if err != nil {
		return nil, err
	}
	result["scheduled"] = true
	result["taskType"] = RestoreDrillTaskType
	result["intervalHours"] = int(s.settings.BackupRestoreDrillInterval.Hours())
	return result, nil
}

func (s BackupService) BackupDir() string {
	return s.store().Root()
}

func (s BackupService) metadataDir() string {
	store := s.store()
	if store.Provider().External {
		return s.defaultBackupDir()
	}
	return store.Root()
}

func (s BackupService) store() BackupArchiveStore {
	if s.archiveStore != nil {
		return s.archiveStore
	}
	return NewLocalBackupArchiveStore(s.defaultBackupDir())
}

func (s BackupService) archiveProviderDetail() BackupArchiveProvider {
	provider := s.store().Provider()
	return backupArchiveProviderWithExternalConfig(provider, s.settings)
}

func (s BackupService) defaultBackupDir() string {
	if dbPath, ok := sqliteDatabasePath(s.settings.DatabaseURL); ok {
		return filepath.Join(filepath.Dir(dbPath), "backups")
	}
	if strings.TrimSpace(s.settings.LogDir) != "" {
		return filepath.Join(s.settings.LogDir, "backups")
	}
	return "backups"
}

func (s BackupService) loadDatabaseSummary(ctx context.Context) (DatabaseSummary, error) {
	if s.databaseSummary != nil {
		return s.databaseSummary(ctx)
	}
	return DatabaseSummary{Backend: "Database", Target: "current connection"}, nil
}

func (s BackupService) backupRetentionConfig() (int, int) {
	copies := s.settings.BackupRetentionCopies
	if copies <= 0 {
		copies = 7
	}
	days := s.settings.BackupRetentionDays
	if days <= 0 {
		days = 30
	}
	return copies, days
}

func (s BackupService) restoreDrillSchedule() map[string]any {
	interval := s.settings.BackupRestoreDrillInterval
	if interval <= 0 {
		return map[string]any{"status": "disabled", "taskType": RestoreDrillTaskType, "intervalHours": 0}
	}
	return map[string]any{
		"status":        "enabled",
		"taskType":      RestoreDrillTaskType,
		"intervalHours": int(interval.Hours()),
	}
}

func sqliteDatabasePath(databaseURL string) (string, bool) {
	value := strings.TrimSpace(databaseURL)
	switch {
	case strings.HasPrefix(value, "sqlite+aiosqlite:///"):
		value = strings.TrimPrefix(value, "sqlite+aiosqlite:///")
	case strings.HasPrefix(value, "sqlite:///"):
		value = strings.TrimPrefix(value, "sqlite:///")
	default:
		if strings.Contains(value, "://") {
			return "", false
		}
	}
	if value == "" || value == ":memory:" {
		return "", false
	}
	return filepath.Clean(value), true
}

func listLogFilesForBackup(logDir string) []string {
	logDir = strings.TrimSpace(logDir)
	if logDir == "" {
		return nil
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".gz") {
			paths = append(paths, filepath.Join(logDir, name))
		}
	}
	sort.Strings(paths)
	return paths
}

func listBackupArchives(dir string) ([]map[string]any, map[string]any, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []map[string]any{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	backups := []map[string]any{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".zip") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, backupArchiveMetadata(entry.Name(), filepath.Join(dir, entry.Name()), info))
	}
	sort.Slice(backups, func(i, j int) bool {
		return backups[i]["createdAt"].(time.Time).After(backups[j]["createdAt"].(time.Time))
	})
	var latest map[string]any
	if len(backups) > 0 {
		latest = backups[0]
	}
	return backups, latest, nil
}

func backupArchiveMetadata(name string, archivePath string, info os.FileInfo) map[string]any {
	return map[string]any{
		"name":      name,
		"path":      archivePath,
		"sizeBytes": info.Size(),
		"createdAt": info.ModTime(),
	}
}

func archiveProviderMap(provider BackupArchiveProvider) map[string]any {
	return map[string]any{
		"key":                        provider.Key,
		"kind":                       provider.Kind,
		"status":                     provider.Status,
		"root":                       provider.Root,
		"external":                   provider.External,
		"supportsList":               provider.SupportsList,
		"supportsWrite":              provider.SupportsWrite,
		"supportsRead":               provider.SupportsRead,
		"supportsDelete":             provider.SupportsDelete,
		"configuredProvider":         provider.ConfiguredProvider,
		"configuredExternalProvider": provider.ConfiguredExternalProvider,
		"externalReady":              provider.ExternalReady,
		"missingExternalConfig":      provider.MissingExternalConfig,
		"setupHint":                  provider.SetupHint,
	}
}

func backupArchiveProviderWithExternalConfig(provider BackupArchiveProvider, settings BackupSettings) BackupArchiveProvider {
	configured := normalizeArchiveProvider(settings.BackupArchiveProvider)
	if configured == "" {
		configured = "local"
	}
	provider.ConfiguredProvider = configured
	provider.MissingExternalConfig = []string{}
	switch configured {
	case "local":
		provider.ExternalReady = false
		provider.SetupHint = "Local filesystem backup archives are active. Set BACKUP_ARCHIVE_PROVIDER=s3 or oss to check external archive configuration readiness."
	case "s3":
		provider.ConfiguredExternalProvider = "s3"
		provider.ExternalReady, provider.MissingExternalConfig = s3ArchiveConfigReady(settings)
		if provider.External && provider.Key == "s3" {
			provider.Status = "ready"
			provider.ExternalReady = true
			provider.SetupHint = "S3 archive store is active. Backups, restore dry-runs, and retention deletes use the configured bucket and prefix."
		} else if provider.ExternalReady {
			provider.Status = providerStatusWithWarning(provider.Status)
			provider.SetupHint = "S3 archive configuration is complete, but the S3 archive store is not active in this runtime; local filesystem backups remain active."
		} else {
			provider.Status = providerStatusWithWarning(provider.Status)
			provider.SetupHint = "S3 archive configuration is incomplete. Set the missing BACKUP_ARCHIVE_S3_* variables; local filesystem backups remain active."
		}
	case "oss":
		provider.ConfiguredExternalProvider = "oss"
		provider.ExternalReady, provider.MissingExternalConfig = ossArchiveConfigReady(settings)
		if provider.External && provider.Key == "oss" {
			provider.Status = "ready"
			provider.ExternalReady = true
			provider.SetupHint = "OSS archive store is active. Backups, restore dry-runs, and retention deletes use the configured bucket and prefix."
		} else if provider.ExternalReady {
			provider.Status = providerStatusWithWarning(provider.Status)
			provider.SetupHint = "OSS archive configuration is complete, but the OSS archive store is not active in this runtime; local filesystem backups remain active."
		} else {
			provider.Status = providerStatusWithWarning(provider.Status)
			provider.SetupHint = "OSS archive configuration is incomplete. Set the missing BACKUP_ARCHIVE_OSS_* variables; local filesystem backups remain active."
		}
	default:
		provider.Status = providerStatusWithWarning(provider.Status)
		provider.SetupHint = "Unknown BACKUP_ARCHIVE_PROVIDER value. Use local, s3, or oss; local filesystem backups remain active."
	}
	return provider
}

func normalizeArchiveProvider(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "local", "local_filesystem", "filesystem", "file":
		return "local"
	case "s3", "aws_s3", "s3_object_storage":
		return "s3"
	case "oss", "aliyun_oss", "oss_object_storage":
		return "oss"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func s3ArchiveConfigReady(settings BackupSettings) (bool, []string) {
	missing := missingArchiveConfig(map[string]string{
		"BACKUP_ARCHIVE_S3_BUCKET":            settings.BackupArchiveS3Bucket,
		"BACKUP_ARCHIVE_S3_REGION":            settings.BackupArchiveS3Region,
		"BACKUP_ARCHIVE_S3_ACCESS_KEY_ID":     settings.BackupArchiveS3AccessKeyID,
		"BACKUP_ARCHIVE_S3_SECRET_ACCESS_KEY": settings.BackupArchiveS3SecretKey,
	})
	return len(missing) == 0, missing
}

func ossArchiveConfigReady(settings BackupSettings) (bool, []string) {
	missing := missingArchiveConfig(map[string]string{
		"BACKUP_ARCHIVE_OSS_BUCKET":            settings.BackupArchiveOSSBucket,
		"BACKUP_ARCHIVE_OSS_REGION":            settings.BackupArchiveOSSRegion,
		"BACKUP_ARCHIVE_OSS_ENDPOINT":          settings.BackupArchiveOSSEndpoint,
		"BACKUP_ARCHIVE_OSS_ACCESS_KEY_ID":     settings.BackupArchiveOSSAccessKey,
		"BACKUP_ARCHIVE_OSS_ACCESS_KEY_SECRET": settings.BackupArchiveOSSSecretKey,
	})
	return len(missing) == 0, missing
}

func missingArchiveConfig(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	missing := []string{}
	for _, key := range keys {
		if strings.TrimSpace(values[key]) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}

func providerStatusWithWarning(status string) string {
	if strings.TrimSpace(status) == "" || status == "ready" {
		return "warning"
	}
	return status
}

func backupRetentionPolicy(backups []map[string]any, copies int, days int, now time.Time) map[string]any {
	if copies <= 0 {
		copies = 7
	}
	if days <= 0 {
		days = 30
	}
	prunable := 0
	cutoff := now.AddDate(0, 0, -days)
	for index, backup := range backups {
		createdAt, _ := backup["createdAt"].(time.Time)
		if index >= copies && !createdAt.IsZero() && createdAt.Before(cutoff) {
			prunable++
		}
	}
	return map[string]any{
		"mode":                "automatic_rotation",
		"recommendedCopies":   copies,
		"recommendedDays":     days,
		"currentCopies":       len(backups),
		"prunableBackupCount": prunable,
	}
}

func pruneBackupArtifacts(backupDir string, backups []map[string]any, copies int, days int, now time.Time) map[string]any {
	return pruneBackupArtifactsWithStore(context.Background(), NewLocalBackupArchiveStore(backupDir), backupDir, backups, copies, days, now)
}

func pruneBackupArtifactsWithStore(ctx context.Context, store BackupArchiveStore, backupDir string, backups []map[string]any, copies int, days int, now time.Time) map[string]any {
	if copies <= 0 {
		copies = 7
	}
	if days <= 0 {
		days = 30
	}
	cutoff := now.AddDate(0, 0, -days)
	prunedBackups := []string{}
	errors := []string{}
	for index, backup := range backups {
		createdAt, _ := backup["createdAt"].(time.Time)
		if index < copies || createdAt.IsZero() || !createdAt.Before(cutoff) {
			continue
		}
		name := filepath.Base(strings.TrimSpace(fmt.Sprint(backup["name"])))
		if name == "." || name == "" || !strings.HasSuffix(strings.ToLower(name), ".zip") {
			errors = append(errors, "invalid backup archive name: "+fmt.Sprint(backup["name"]))
			continue
		}
		if !store.Provider().External {
			target := filepath.Join(backupDir, name)
			if !pathInsideRoot(backupDir, target) {
				errors = append(errors, "backup archive outside retention root: "+name)
				continue
			}
		}
		if err := store.DeleteArchive(ctx, name); err != nil {
			errors = append(errors, name+": "+err.Error())
			continue
		}
		prunedBackups = append(prunedBackups, name)
	}
	prunedDrills, drillErrors := pruneRestoreDrillDirs(backupDir, cutoff)
	errors = append(errors, drillErrors...)
	status := "applied"
	if len(errors) > 0 {
		status = "warning"
	}
	return map[string]any{
		"status":                status,
		"mode":                  "automatic_rotation",
		"appliedAt":             now,
		"retentionCopies":       copies,
		"retentionDays":         days,
		"cutoffAt":              cutoff,
		"prunedBackupCount":     len(prunedBackups),
		"prunedBackups":         prunedBackups,
		"prunedSandboxDirCount": len(prunedDrills),
		"prunedSandboxDirs":     prunedDrills,
		"errors":                errors,
	}
}

func pruneRestoreDrillDirs(backupDir string, cutoff time.Time) ([]string, []string) {
	root := filepath.Join(backupDir, "restore-drills")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{"restore-drills: " + err.Error()}
	}
	pruned := []string{}
	errors := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			errors = append(errors, entry.Name()+": "+err.Error())
			continue
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		target := filepath.Join(root, entry.Name())
		if !pathInsideRoot(root, target) {
			errors = append(errors, "restore drill outside retention root: "+entry.Name())
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			errors = append(errors, entry.Name()+": "+err.Error())
			continue
		}
		pruned = append(pruned, entry.Name())
	}
	return pruned, errors
}

func pathInsideRoot(root string, target string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}

func backupRestoreDrillStatus(latest map[string]any, backupDir string) map[string]any {
	status := "not_ready"
	if latest != nil {
		status = "dry_run_available"
	}
	record := latestBackupRestoreDrill(backupDir)
	if len(record) > 0 {
		recordBackup := strings.TrimSpace(fmt.Sprint(record["backupName"]))
		latestBackup := ""
		if latest != nil {
			latestBackup = strings.TrimSpace(fmt.Sprint(latest["name"]))
		}
		switch {
		case latestBackup == "" || recordBackup == latestBackup:
			if fmt.Sprint(record["status"]) == "valid" {
				status = "drilled"
			} else {
				status = "drill_warning"
			}
		default:
			status = "stale_drill"
		}
	}
	return map[string]any{
		"status":           status,
		"lastDryRun":       nullableMap(record),
		"supportedAction":  "POST /api/ops/backups/{backupName}/restore-dry-run",
		"destructive":      false,
		"requiresOperator": true,
	}
}

func saveBackupRestoreDrill(backupDir string, result map[string]any) error {
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return err
	}
	record := map[string]any{
		"backupName":         result["backupName"],
		"backupPath":         result["backupPath"],
		"status":             result["status"],
		"checkedAt":          result["checkedAt"],
		"manifestVersion":    result["manifestVersion"],
		"entryCount":         result["entryCount"],
		"databaseEntryCount": result["databaseEntryCount"],
		"logEntryCount":      result["logEntryCount"],
		"notes":              result["notes"],
		"sandboxStatus":      result["sandboxStatus"],
		"sandboxDir":         result["sandboxDir"],
		"sandboxFileCount":   result["sandboxFileCount"],
		"sandboxSizeBytes":   result["sandboxSizeBytes"],
		"sandboxChecks":      result["sandboxChecks"],
		"destructive":        false,
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(backupRestoreDrillPath(backupDir), raw, 0o600)
}

func latestBackupRestoreDrill(backupDir string) map[string]any {
	raw, err := os.ReadFile(backupRestoreDrillPath(backupDir))
	if err != nil {
		return nil
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil
	}
	return record
}

func backupRestoreDrillPath(backupDir string) string {
	return filepath.Join(backupDir, ".restore-drill.json")
}

func saveBackupRetentionRun(backupDir string, result map[string]any) error {
	if len(result) == 0 {
		return nil
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return err
	}
	record := map[string]any{
		"status":                result["status"],
		"mode":                  result["mode"],
		"appliedAt":             result["appliedAt"],
		"retentionCopies":       result["retentionCopies"],
		"retentionDays":         result["retentionDays"],
		"cutoffAt":              result["cutoffAt"],
		"prunedBackupCount":     result["prunedBackupCount"],
		"prunedBackups":         result["prunedBackups"],
		"prunedSandboxDirCount": result["prunedSandboxDirCount"],
		"prunedSandboxDirs":     result["prunedSandboxDirs"],
		"errors":                result["errors"],
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(backupRetentionRunPath(backupDir), raw, 0o600)
}

func latestBackupRetentionRun(backupDir string) map[string]any {
	raw, err := os.ReadFile(backupRetentionRunPath(backupDir))
	if err != nil {
		return nil
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil
	}
	return record
}

func backupRetentionRunPath(backupDir string) string {
	return filepath.Join(backupDir, ".retention-run.json")
}

func (s BackupService) inspectArchive(ctx context.Context, backupName string) (map[string]any, error) {
	name := filepath.Base(strings.TrimSpace(backupName))
	if name == "." || name == "" || name != strings.TrimSpace(backupName) || !strings.HasSuffix(strings.ToLower(name), ".zip") {
		return nil, os.ErrInvalid
	}
	store := s.store()
	archivePath, cleanup, err := store.ResolveArchive(ctx, name)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return nil, err
	}
	displayPath := archivePath
	if stat, err := store.StatArchive(ctx, name); err == nil {
		if pathValue := strings.TrimSpace(fmt.Sprint(stat["path"])); pathValue != "" {
			displayPath = pathValue
		}
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	entries := []map[string]any{}
	notes := []string{}
	manifest := map[string]any{}
	databaseEntries := 0
	logEntries := 0
	sandboxChecks := []map[string]any{}
	checkedAt := s.now()
	sandboxDir := restoreDrillSandboxDir(s.metadataDir(), name, checkedAt)
	if err := os.MkdirAll(sandboxDir, 0o700); err != nil {
		return nil, err
	}
	sandboxFileCount := 0
	var sandboxSizeBytes int64
	sandboxDatabaseFiles := []string{}
	sandboxLogFiles := []string{}
	sandboxRuntimeFiles := []string{}
	sandboxStatus := "extracted"
	for _, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry := map[string]any{"name": file.Name, "compressedSizeBytes": file.CompressedSize64, "sizeBytes": file.UncompressedSize64}
		entries = append(entries, entry)
		switch {
		case file.Name == "manifest.json":
			body, err := readZipFile(file)
			if err != nil {
				notes = append(notes, "manifest.json: "+err.Error())
				continue
			}
			if err := json.Unmarshal(body, &manifest); err != nil {
				notes = append(notes, "manifest.json: "+err.Error())
			}
		case strings.HasPrefix(file.Name, "database/"):
			databaseEntries++
		case strings.HasPrefix(file.Name, "logs/"):
			logEntries++
		}
		extractedPath, ok := safeZipExtractPath(sandboxDir, file.Name)
		if !ok {
			sandboxStatus = "warning"
			notes = append(notes, "skipped unsafe archive entry: "+file.Name)
			continue
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(extractedPath, 0o700); err != nil {
				sandboxStatus = "warning"
				notes = append(notes, file.Name+": sandbox directory create failed: "+err.Error())
			}
			continue
		}
		written, err := extractZipFile(file, extractedPath)
		if err != nil {
			sandboxStatus = "warning"
			notes = append(notes, file.Name+": sandbox extract failed: "+err.Error())
			continue
		}
		sandboxFileCount++
		sandboxSizeBytes += written
		switch {
		case strings.HasPrefix(file.Name, "database/"):
			sandboxDatabaseFiles = append(sandboxDatabaseFiles, file.Name)
		case strings.HasPrefix(file.Name, "logs/"):
			sandboxLogFiles = append(sandboxLogFiles, file.Name)
		case file.Name == "manifest.json" || strings.HasPrefix(file.Name, "runtime/"):
			sandboxRuntimeFiles = append(sandboxRuntimeFiles, file.Name)
		}
	}
	if len(manifest) == 0 {
		notes = append(notes, "manifest.json is missing or unreadable")
	}
	if databaseEntries == 0 {
		notes = append(notes, "no embedded database snapshot; restore requires external database backup tooling")
	}
	sandboxChecks = append(sandboxChecks,
		map[string]any{"name": "manifest", "status": checkStatus(len(manifest) > 0), "detail": "manifest.json parsed from archive"},
		map[string]any{"name": "database_snapshot", "status": checkStatus(databaseEntries > 0), "detail": fmt.Sprintf("%d database archive entries", databaseEntries)},
		map[string]any{"name": "sandbox_extraction", "status": checkStatus(sandboxStatus == "extracted"), "detail": fmt.Sprintf("%d files extracted to sandbox", sandboxFileCount)},
	)
	status := "valid"
	if len(notes) > 0 {
		status = "warning"
	}
	restorePlan := []string{
		"Verify manifest metadata and archive checksums.",
		"Restore database artifacts with environment-specific tooling.",
		"Restore runtime environment overrides and log archives only after operator review.",
		"Restart TradingCopilot and run setup readiness plus smoke tests.",
	}
	return map[string]any{
		"status":                status,
		"backupName":            name,
		"backupPath":            displayPath,
		"backupDir":             s.BackupDir(),
		"backupMetadataDir":     s.metadataDir(),
		"archiveProvider":       s.archiveProviderDetail().Key,
		"archiveProviderDetail": archiveProviderMap(s.archiveProviderDetail()),
		"checkedAt":             checkedAt,
		"manifest":              manifest,
		"manifestVersion":       manifest["manifestVersion"],
		"entryCount":            len(entries),
		"databaseEntryCount":    databaseEntries,
		"logEntryCount":         logEntries,
		"entries":               entries,
		"notes":                 notes,
		"sandboxStatus":         sandboxStatus,
		"sandboxDir":            sandboxDir,
		"sandboxFileCount":      sandboxFileCount,
		"sandboxSizeBytes":      sandboxSizeBytes,
		"sandboxDatabaseFiles":  sandboxDatabaseFiles,
		"sandboxLogFiles":       sandboxLogFiles,
		"sandboxRuntimeFiles":   sandboxRuntimeFiles,
		"sandboxChecks":         sandboxChecks,
		"restorePlan":           restorePlan,
		"destructive":           false,
		"operatorAction":        "manual_restore_required",
		"retentionPolicyHint":   "Keep at least the latest 7 local archives or 30 days, whichever retains more recovery points.",
	}, nil
}

func restoreDrillSandboxDir(backupDir string, backupName string, checkedAt time.Time) string {
	base := strings.TrimSuffix(filepath.Base(backupName), filepath.Ext(backupName))
	base = safeFileToken(base)
	return filepath.Join(backupDir, "restore-drills", base+"-"+checkedAt.UTC().Format("20060102-150405.000000000"))
}

func safeFileToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "backup"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	token := strings.Trim(builder.String(), "-")
	if token == "" {
		return "backup"
	}
	return token
}

func safeZipExtractPath(root string, entryName string) (string, bool) {
	if strings.TrimSpace(entryName) == "" || strings.ContainsRune(entryName, '\x00') {
		return "", false
	}
	normalized := strings.ReplaceAll(entryName, "\\", "/")
	if strings.HasPrefix(normalized, "/") || strings.Contains(normalized, ":") {
		return "", false
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == "." || part == ".." {
			return "", false
		}
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	targetAbs, err := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(cleaned)))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return targetAbs, true
}

func extractZipFile(file *zip.File, targetPath string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return 0, err
	}
	reader, err := file.Open()
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	writer, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(writer, reader)
	closeErr := writer.Close()
	if copyErr != nil {
		return written, copyErr
	}
	return written, closeErr
}

func checkStatus(ok bool) string {
	if ok {
		return "pass"
	}
	return "warning"
}

func nullableMap(value map[string]any) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func addZipFile(writer *zip.Writer, zipName string, sourcePath string) (map[string]any, error) {
	file, err := os.Open(sourcePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		return nil, os.ErrInvalid
	}
	header, err := zip.FileInfoHeader(stat)
	if err != nil {
		return nil, err
	}
	header.Name = filepath.ToSlash(zipName)
	header.Method = zip.Deflate
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(entry, file); err != nil {
		return nil, err
	}
	return map[string]any{"name": header.Name, "sourcePath": sourcePath, "sizeBytes": stat.Size()}, nil
}

func addZipJSON(writer *zip.Writer, zipName string, payload any) error {
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	header := &zip.FileHeader{Name: filepath.ToSlash(zipName), Method: zip.Deflate}
	header.SetModTime(time.Now())
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = entry.Write(raw)
	return err
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
