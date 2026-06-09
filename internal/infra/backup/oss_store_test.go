package backup

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

func TestStoreForSettingsReturnsOSSStoreWhenConfigReady(t *testing.T) {
	store := StoreForSettings(config.Settings{
		BackupArchiveProvider:           "oss",
		BackupArchiveOSSBucket:          "tc-backups",
		BackupArchiveOSSRegion:          "cn-hangzhou",
		BackupArchiveOSSEndpoint:        "https://oss-cn-hangzhou.aliyuncs.com",
		BackupArchiveOSSAccessKeyID:     "key",
		BackupArchiveOSSAccessKeySecret: "secret",
		BackupArchiveOSSPrefix:          "prod",
	})
	if store == nil {
		t.Fatal("expected OSS archive store for complete oss config")
	}
	provider := store.Provider()
	if provider.Key != "oss" || !provider.External || provider.Root != "oss://tc-backups/prod" {
		t.Fatalf("provider = %+v", provider)
	}
}

func TestStoreForSettingsFallsBackWhenOSSConfigIncomplete(t *testing.T) {
	store := StoreForSettings(config.Settings{
		BackupArchiveProvider:  "oss",
		BackupArchiveOSSBucket: "tc-backups",
	})
	if store != nil {
		t.Fatalf("expected nil store for incomplete oss config, got %+v", store.Provider())
	}
}

func TestOSSArchiveStoreCreateListStatResolveAndDelete(t *testing.T) {
	client := newFakeOSSArchiveClient()
	store := NewOSSArchiveStore(client, "tc-backups", "prod/backups", "cn-hangzhou")

	writer, archivePath, err := store.CreateArchive(context.Background(), "tradingcopilot-backup-20260609-120000.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("zip bytes")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if archivePath != "oss://tc-backups/prod/backups/tradingcopilot-backup-20260609-120000.zip" {
		t.Fatalf("archivePath = %q", archivePath)
	}
	if got := string(client.objects["prod/backups/tradingcopilot-backup-20260609-120000.zip"].body); got != "zip bytes" {
		t.Fatalf("uploaded body = %q", got)
	}

	oldTime := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	client.objects["prod/backups/tradingcopilot-backup-20260608-100000.zip"] = fakeOSSObject{body: []byte("old"), lastModified: oldTime}
	backups, latest, err := store.ListArchives(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 2 || latest["name"] != "tradingcopilot-backup-20260609-120000.zip" {
		t.Fatalf("list/latest mismatch: backups=%+v latest=%+v", backups, latest)
	}

	stat, err := store.StatArchive(context.Background(), "tradingcopilot-backup-20260609-120000.zip")
	if err != nil {
		t.Fatal(err)
	}
	if stat["sizeBytes"] != int64(len("zip bytes")) || stat["path"] != archivePath {
		t.Fatalf("stat mismatch: %+v", stat)
	}

	resolvedPath, cleanup, err := store.ResolveArchive(context.Background(), "tradingcopilot-backup-20260609-120000.zip")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(resolvedPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "zip bytes" {
		t.Fatalf("resolved body = %q", string(body))
	}
	cleanup()
	if _, err := os.Stat(resolvedPath); !os.IsNotExist(err) {
		t.Fatalf("cleanup should remove temp file, stat err=%v", err)
	}

	if err := store.DeleteArchive(context.Background(), "tradingcopilot-backup-20260609-120000.zip"); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.objects["prod/backups/tradingcopilot-backup-20260609-120000.zip"]; ok {
		t.Fatal("delete should remove remote object")
	}
}

type fakeOSSArchiveClient struct {
	objects map[string]fakeOSSObject
}

type fakeOSSObject struct {
	body         []byte
	lastModified time.Time
}

func newFakeOSSArchiveClient() *fakeOSSArchiveClient {
	return &fakeOSSArchiveClient{objects: map[string]fakeOSSObject{}}
}

func (c *fakeOSSArchiveClient) ListObjects(_ ...oss.Option) (oss.ListObjectsResult, error) {
	result := oss.ListObjectsResult{Objects: []oss.ObjectProperties{}}
	for key, object := range c.objects {
		modified := object.lastModified
		if modified.IsZero() {
			modified = time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
		}
		result.Objects = append(result.Objects, oss.ObjectProperties{
			Key:          key,
			Size:         int64(len(object.body)),
			LastModified: modified,
		})
	}
	return result, nil
}

func (c *fakeOSSArchiveClient) PutObject(objectKey string, reader io.Reader, _ ...oss.Option) error {
	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	c.objects[objectKey] = fakeOSSObject{body: body, lastModified: time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)}
	return nil
}

func (c *fakeOSSArchiveClient) GetObjectMeta(objectKey string, _ ...oss.Option) (http.Header, error) {
	object, ok := c.objects[objectKey]
	if !ok {
		return nil, os.ErrNotExist
	}
	modified := object.lastModified
	if modified.IsZero() {
		modified = time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	}
	header := http.Header{}
	header.Set("Content-Length", "9")
	header.Set("Last-Modified", modified.Format(http.TimeFormat))
	return header, nil
}

func (c *fakeOSSArchiveClient) GetObject(objectKey string, _ ...oss.Option) (io.ReadCloser, error) {
	object, ok := c.objects[objectKey]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(object.body)), nil
}

func (c *fakeOSSArchiveClient) DeleteObject(objectKey string, _ ...oss.Option) error {
	delete(c.objects, objectKey)
	return nil
}
