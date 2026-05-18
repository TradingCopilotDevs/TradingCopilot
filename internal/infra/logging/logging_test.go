package logging

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	applogging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/logging"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
)

func TestReaderFiltersAndRedactsJSONL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.log")
	content := `{"time":"2026-05-16T01:00:00Z","level":"info","role":"serve","source":"http","event":"http.request","group":"logs","method":"GET","path":"/api/logs","status":"200","durationMs":3,"noise":true,"msg":"GET /api/logs -> 200 3ms","token":"secret-token"}` + "\n" +
		`{"time":"2026-05-16T01:01:00Z","level":"error","role":"serve","source":"gorm","event":"gorm.query_failed","group":"database","method":"query","status":"error","durationMs":250,"msg":"query failed","password":"secret-password"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	from := time.Date(2026, 5, 16, 1, 0, 30, 0, time.UTC)
	result, err := NewReader(testSettings(dir)).Query(context.Background(), applogging.Query{
		From:  &from,
		Level: "error",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected one filtered entry, got %d", len(result.Entries))
	}
	entry := result.Entries[0]
	if entry.Message != "query failed" || entry.Source != "gorm" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
	if entry.Event != "gorm.query_failed" || entry.DurationMS == nil || *entry.DurationMS != 250 {
		t.Fatalf("structured fields were not promoted: %+v", entry)
	}
	if entry.Fields["password"] != redacted || entry.Raw["password"] != redacted {
		t.Fatalf("sensitive fields were not redacted: %+v %+v", entry.Fields, entry.Raw)
	}

	noiseResult, err := NewReader(testSettings(dir)).Query(context.Background(), applogging.Query{
		Event: "http.request",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(noiseResult.Entries) != 0 {
		t.Fatalf("noise entries should be hidden by default: %+v", noiseResult.Entries)
	}
	noiseResult, err = NewReader(testSettings(dir)).Query(context.Background(), applogging.Query{
		Event:        "http.request",
		Group:        "logs",
		Method:       "GET",
		Path:         "/api/logs",
		Status:       "200",
		IncludeNoise: true,
		Limit:        10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(noiseResult.Entries) != 1 || noiseResult.Entries[0].Path != "/api/logs" {
		t.Fatalf("expected one included noise HTTP entry, got %+v", noiseResult.Entries)
	}
}

func TestReaderFillsMissingCommonFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worker.log")
	content := `{"time":"2026-05-16T01:00:00Z","level":"info","role":"worker","source":"stdlog","msg":"plain stdlib log"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := NewReader(testSettings(dir)).Query(context.Background(), applogging.Query{IncludeNoise: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected one entry, got %d", len(result.Entries))
	}
	entry := result.Entries[0]
	if entry.Event != Placeholder || entry.Group != Placeholder || entry.Method != Placeholder || entry.Status != Placeholder || entry.DurationMS != nil {
		t.Fatalf("missing fields were not filled correctly: %+v", entry)
	}
}

func TestReaderCursorPaginationUsesOpaqueCursors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.log")
	content := `{"time":"2026-05-16T01:00:00Z","level":"info","role":"serve","msg":"first"}` + "\n" +
		`{"time":"2026-05-16T01:01:00Z","level":"info","role":"serve","msg":"second"}` + "\n" +
		`{"time":"2026-05-16T01:02:00Z","level":"info","role":"serve","msg":"third"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	reader := NewReader(testSettings(dir))
	firstPage, err := reader.Query(context.Background(), applogging.Query{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(firstPage.Entries) != 2 {
		t.Fatalf("expected two entries on first page, got %d", len(firstPage.Entries))
	}
	if firstPage.Entries[0].Message != "third" || firstPage.Entries[1].Message != "second" {
		t.Fatalf("expected newest entries first, got %+v", firstPage.Entries)
	}
	if firstPage.NextCursor == "" || firstPage.NextCursor == "2" {
		t.Fatalf("expected non-numeric opaque next cursor, got %q", firstPage.NextCursor)
	}

	secondPage, err := reader.Query(context.Background(), applogging.Query{Limit: 2, Cursor: firstPage.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(secondPage.Entries) != 1 || secondPage.Entries[0].Message != "first" {
		t.Fatalf("expected remaining older entry without duplicate, got %+v", secondPage.Entries)
	}
	if secondPage.NextCursor != "" {
		t.Fatalf("expected no next cursor on final page, got %q", secondPage.NextCursor)
	}
}

func TestSetupAddsCommonFieldsToZapWrites(t *testing.T) {
	dir := t.TempDir()
	shutdown, err := Setup(testSettings(dir), "worker")
	if err != nil {
		t.Fatal(err)
	}
	Logger().Info("plain zap log")
	if err := shutdown(); err != nil {
		t.Fatal(err)
	}
	result, err := NewReader(testSettings(dir)).Query(context.Background(), applogging.Query{
		Q:     "plain zap log",
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("expected one plain zap entry, got %d", len(result.Entries))
	}
	entry := result.Entries[0]
	if entry.Event != Placeholder || entry.Group != Placeholder || entry.Method != Placeholder || entry.Status != Placeholder || entry.DurationMS != nil {
		t.Fatalf("zap defaults were not written: %+v", entry)
	}
}

func TestCleanSizeModeDeletesOldestInactiveFiles(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "serve.log")
	old := filepath.Join(dir, "serve-2026-05-16T01-00-00.000.log")
	newer := filepath.Join(dir, "worker-2026-05-16T02-00-00.000.log")
	for path, content := range map[string]string{
		active: "active",
		old:    "old",
		newer:  "newer",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	settings := testSettings(dir)
	settings.LogRotationTotalSizeMB = 1
	if err := os.WriteFile(old, make([]byte, 800*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newer, make([]byte, 800*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newer, newTime, newTime); err != nil {
		t.Fatal(err)
	}
	if err := Clean(settings, "serve"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(active); err != nil {
		t.Fatalf("active file should remain: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("oldest inactive file should be deleted, stat err=%v", err)
	}
}

func testSettings(dir string) config.Settings {
	return config.Settings{
		LogDir:                 dir,
		LogLevel:               "info",
		LogRotationMode:        RotationModeSize,
		LogRotationSizeMB:      5,
		LogRotationTotalSizeMB: 100,
		LogRotationMaxAgeDays:  7,
	}
}
