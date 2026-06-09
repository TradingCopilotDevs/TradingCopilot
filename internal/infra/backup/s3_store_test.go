package backup

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func TestStoreForSettingsReturnsS3StoreWhenConfigReady(t *testing.T) {
	store := StoreForSettings(config.Settings{
		BackupArchiveProvider:          "s3",
		BackupArchiveS3Bucket:          "tc-backups",
		BackupArchiveS3Region:          "us-east-1",
		BackupArchiveS3AccessKeyID:     "key",
		BackupArchiveS3SecretAccessKey: "secret",
		BackupArchiveS3Prefix:          "prod",
	})
	if store == nil {
		t.Fatal("expected S3 archive store for complete s3 config")
	}
	provider := store.Provider()
	if provider.Key != "s3" || !provider.External || provider.Root != "s3://tc-backups/prod" {
		t.Fatalf("provider = %+v", provider)
	}
}

func TestStoreForSettingsFallsBackWhenS3ConfigIncomplete(t *testing.T) {
	store := StoreForSettings(config.Settings{
		BackupArchiveProvider: "s3",
		BackupArchiveS3Bucket: "tc-backups",
	})
	if store != nil {
		t.Fatalf("expected nil store for incomplete s3 config, got %+v", store.Provider())
	}
}

func TestS3ArchiveStoreCreateListStatResolveAndDelete(t *testing.T) {
	client := newFakeS3ArchiveClient()
	store := NewS3ArchiveStore(client, "tc-backups", "prod/backups", "us-east-1")

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
	if archivePath != "s3://tc-backups/prod/backups/tradingcopilot-backup-20260609-120000.zip" {
		t.Fatalf("archivePath = %q", archivePath)
	}
	if got := string(client.objects["prod/backups/tradingcopilot-backup-20260609-120000.zip"].body); got != "zip bytes" {
		t.Fatalf("uploaded body = %q", got)
	}

	oldTime := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	client.objects["prod/backups/tradingcopilot-backup-20260608-100000.zip"] = fakeS3Object{body: []byte("old"), lastModified: oldTime}
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

type fakeS3ArchiveClient struct {
	objects map[string]fakeS3Object
}

type fakeS3Object struct {
	body         []byte
	lastModified time.Time
}

func newFakeS3ArchiveClient() *fakeS3ArchiveClient {
	return &fakeS3ArchiveClient{objects: map[string]fakeS3Object{}}
}

func (c *fakeS3ArchiveClient) ListObjectsV2(_ context.Context, input *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	prefix := aws.ToString(input.Prefix)
	contents := []types.Object{}
	for key, object := range c.objects {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		keyCopy := key
		size := int64(len(object.body))
		modified := object.lastModified
		if modified.IsZero() {
			modified = time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
		}
		contents = append(contents, types.Object{Key: &keyCopy, Size: &size, LastModified: &modified})
	}
	return &s3.ListObjectsV2Output{Contents: contents, IsTruncated: aws.Bool(false)}, nil
}

func (c *fakeS3ArchiveClient) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	body, err := io.ReadAll(input.Body)
	if err != nil {
		return nil, err
	}
	c.objects[aws.ToString(input.Key)] = fakeS3Object{body: body, lastModified: time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)}
	return &s3.PutObjectOutput{}, nil
}

func (c *fakeS3ArchiveClient) HeadObject(_ context.Context, input *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	object, ok := c.objects[aws.ToString(input.Key)]
	if !ok {
		return nil, os.ErrNotExist
	}
	size := int64(len(object.body))
	modified := object.lastModified
	return &s3.HeadObjectOutput{ContentLength: &size, LastModified: &modified}, nil
}

func (c *fakeS3ArchiveClient) GetObject(_ context.Context, input *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	object, ok := c.objects[aws.ToString(input.Key)]
	if !ok {
		return nil, os.ErrNotExist
	}
	return &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(object.body))}, nil
}

func (c *fakeS3ArchiveClient) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	delete(c.objects, aws.ToString(input.Key))
	return &s3.DeleteObjectOutput{}, nil
}
