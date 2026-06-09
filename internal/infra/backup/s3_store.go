package backup

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	appops "github.com/TradingCopilotDevs/TradingCopilot/internal/app/ops"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type s3ArchiveAPI interface {
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type S3ArchiveStore struct {
	client s3ArchiveAPI
	bucket string
	prefix string
	region string
	root   string
}

func StoreForSettings(settings config.Settings) appops.BackupArchiveStore {
	switch normalizeProvider(settings.BackupArchiveProvider) {
	case "s3":
		return s3StoreForSettings(settings)
	case "oss":
		return ossStoreForSettings(settings)
	default:
		return nil
	}
}

func s3StoreForSettings(settings config.Settings) appops.BackupArchiveStore {
	if !s3SettingsReady(settings) {
		return nil
	}
	cfg := aws.Config{
		Region: settings.BackupArchiveS3Region,
		Credentials: credentials.NewStaticCredentialsProvider(
			settings.BackupArchiveS3AccessKeyID,
			settings.BackupArchiveS3SecretAccessKey,
			"",
		),
	}
	endpoint := strings.TrimSpace(settings.BackupArchiveS3Endpoint)
	client := s3.NewFromConfig(cfg, func(options *s3.Options) {
		if endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
			options.UsePathStyle = true
		}
	})
	return NewS3ArchiveStore(client, settings.BackupArchiveS3Bucket, settings.BackupArchiveS3Prefix, settings.BackupArchiveS3Region)
}

func ossStoreForSettings(settings config.Settings) appops.BackupArchiveStore {
	if !ossSettingsReady(settings) {
		return nil
	}
	client, err := oss.New(settings.BackupArchiveOSSEndpoint, settings.BackupArchiveOSSAccessKeyID, settings.BackupArchiveOSSAccessKeySecret)
	if err != nil {
		return nil
	}
	bucket, err := client.Bucket(settings.BackupArchiveOSSBucket)
	if err != nil {
		return nil
	}
	return NewOSSArchiveStore(bucket, settings.BackupArchiveOSSBucket, settings.BackupArchiveOSSPrefix, settings.BackupArchiveOSSRegion)
}

func NewS3ArchiveStore(client s3ArchiveAPI, bucket string, prefix string, region string) S3ArchiveStore {
	prefix = cleanObjectPrefix(prefix)
	root := "s3://" + strings.TrimSpace(bucket)
	if prefix != "" {
		root += "/" + prefix
	}
	return S3ArchiveStore{
		client: client,
		bucket: strings.TrimSpace(bucket),
		prefix: prefix,
		region: strings.TrimSpace(region),
		root:   root,
	}
}

func (s S3ArchiveStore) Provider() appops.BackupArchiveProvider {
	status := "ready"
	if s.client == nil || s.bucket == "" {
		status = "blocked"
	}
	return appops.BackupArchiveProvider{
		Key:            "s3",
		Kind:           "object_storage",
		Status:         status,
		Root:           s.Root(),
		External:       true,
		SupportsList:   true,
		SupportsWrite:  true,
		SupportsRead:   true,
		SupportsDelete: true,
	}
}

func (s S3ArchiveStore) Root() string {
	if strings.TrimSpace(s.root) == "" {
		return "s3://"
	}
	return s.root
}

func (s S3ArchiveStore) EnsureRoot(ctx context.Context) error {
	return s.ensureReady(ctx)
}

func (s S3ArchiveStore) ListArchives(ctx context.Context) ([]map[string]any, map[string]any, error) {
	if err := s.ensureReady(ctx); err != nil {
		return nil, nil, err
	}
	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
	}
	if s.prefix != "" {
		input.Prefix = aws.String(s.prefix + "/")
	}
	backups := []map[string]any{}
	for {
		output, err := s.client.ListObjectsV2(ctx, input)
		if err != nil {
			return nil, nil, err
		}
		for _, object := range output.Contents {
			key := strings.TrimSpace(aws.ToString(object.Key))
			name := path.Base(key)
			if name == "." || name == "" || !strings.HasSuffix(strings.ToLower(name), ".zip") {
				continue
			}
			createdAt := time.Time{}
			if object.LastModified != nil {
				createdAt = *object.LastModified
			}
			size := int64(0)
			if object.Size != nil {
				size = *object.Size
			}
			backups = append(backups, map[string]any{
				"name":      name,
				"path":      s.objectURI(key),
				"sizeBytes": size,
				"createdAt": createdAt,
			})
		}
		if output.IsTruncated == nil || !*output.IsTruncated || output.NextContinuationToken == nil {
			break
		}
		input.ContinuationToken = output.NextContinuationToken
	}
	sort.Slice(backups, func(i, j int) bool {
		left, _ := backups[i]["createdAt"].(time.Time)
		right, _ := backups[j]["createdAt"].(time.Time)
		return left.After(right)
	})
	var latest map[string]any
	if len(backups) > 0 {
		latest = backups[0]
	}
	return backups, latest, nil
}

func (s S3ArchiveStore) CreateArchive(ctx context.Context, name string) (io.WriteCloser, string, error) {
	if err := s.ensureReady(ctx); err != nil {
		return nil, "", err
	}
	name, err := cleanArchiveName(name)
	if err != nil {
		return nil, "", err
	}
	temp, err := os.CreateTemp("", "tradingcopilot-s3-backup-*.zip")
	if err != nil {
		return nil, "", err
	}
	key := s.objectKey(name)
	return &s3ArchiveUploadWriter{
		ctx:      ctx,
		client:   s.client,
		bucket:   s.bucket,
		key:      key,
		file:     temp,
		tempPath: temp.Name(),
	}, s.objectURI(key), nil
}

func (s S3ArchiveStore) StatArchive(ctx context.Context, name string) (map[string]any, error) {
	if err := s.ensureReady(ctx); err != nil {
		return nil, err
	}
	name, err := cleanArchiveName(name)
	if err != nil {
		return nil, err
	}
	key := s.objectKey(name)
	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, mapS3NotFound(err)
	}
	createdAt := time.Time{}
	if output.LastModified != nil {
		createdAt = *output.LastModified
	}
	size := int64(0)
	if output.ContentLength != nil {
		size = *output.ContentLength
	}
	return map[string]any{
		"name":      name,
		"path":      s.objectURI(key),
		"sizeBytes": size,
		"createdAt": createdAt,
	}, nil
}

func (s S3ArchiveStore) ResolveArchive(ctx context.Context, name string) (string, func(), error) {
	if err := s.ensureReady(ctx); err != nil {
		return "", nil, err
	}
	name, err := cleanArchiveName(name)
	if err != nil {
		return "", nil, err
	}
	key := s.objectKey(name)
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return "", nil, mapS3NotFound(err)
	}
	defer output.Body.Close()
	temp, err := os.CreateTemp("", "tradingcopilot-s3-restore-*.zip")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.Remove(temp.Name())
	}
	if _, err := io.Copy(temp, output.Body); err != nil {
		_ = temp.Close()
		cleanup()
		return "", nil, err
	}
	if err := temp.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return temp.Name(), cleanup, nil
}

func (s S3ArchiveStore) DeleteArchive(ctx context.Context, name string) error {
	if err := s.ensureReady(ctx); err != nil {
		return err
	}
	name, err := cleanArchiveName(name)
	if err != nil {
		return err
	}
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(name)),
	})
	return err
}

func (s S3ArchiveStore) ensureReady(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.client == nil || strings.TrimSpace(s.bucket) == "" {
		return errors.New("s3 archive store is not configured")
	}
	return nil
}

func (s S3ArchiveStore) objectKey(name string) string {
	if s.prefix == "" {
		return name
	}
	return s.prefix + "/" + name
}

func (s S3ArchiveStore) objectURI(key string) string {
	return "s3://" + s.bucket + "/" + strings.TrimLeft(key, "/")
}

type s3ArchiveUploadWriter struct {
	ctx      context.Context
	client   s3ArchiveAPI
	bucket   string
	key      string
	file     *os.File
	tempPath string
	closed   bool
}

func (w *s3ArchiveUploadWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, os.ErrClosed
	}
	return w.file.Write(p)
}

func (w *s3ArchiveUploadWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	var errs []error
	if _, err := w.file.Seek(0, 0); err != nil {
		errs = append(errs, err)
	} else {
		_, err := w.client.PutObject(w.ctx, &s3.PutObjectInput{
			Bucket: aws.String(w.bucket),
			Key:    aws.String(w.key),
			Body:   w.file,
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	if err := w.file.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := os.Remove(w.tempPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func cleanArchiveName(name string) (string, error) {
	name = strings.TrimSpace(name)
	base := filepath.Base(name)
	if base == "." || base == "" || base != name || !strings.HasSuffix(strings.ToLower(base), ".zip") {
		return "", os.ErrInvalid
	}
	return base, nil
}

func cleanObjectPrefix(prefix string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "." {
		return ""
	}
	cleaned := path.Clean(prefix)
	if cleaned == "." {
		return ""
	}
	return strings.Trim(cleaned, "/")
}

func s3SettingsReady(settings config.Settings) bool {
	return strings.TrimSpace(settings.BackupArchiveS3Bucket) != "" &&
		strings.TrimSpace(settings.BackupArchiveS3Region) != "" &&
		strings.TrimSpace(settings.BackupArchiveS3AccessKeyID) != "" &&
		strings.TrimSpace(settings.BackupArchiveS3SecretAccessKey) != ""
}

func ossSettingsReady(settings config.Settings) bool {
	return strings.TrimSpace(settings.BackupArchiveOSSBucket) != "" &&
		strings.TrimSpace(settings.BackupArchiveOSSRegion) != "" &&
		strings.TrimSpace(settings.BackupArchiveOSSEndpoint) != "" &&
		strings.TrimSpace(settings.BackupArchiveOSSAccessKeyID) != "" &&
		strings.TrimSpace(settings.BackupArchiveOSSAccessKeySecret) != ""
}

func normalizeProvider(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "s3", "aws_s3", "s3_object_storage":
		return "s3"
	case "oss", "aliyun_oss", "oss_object_storage":
		return "oss"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func mapS3NotFound(err error) error {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "NoSuchBucket", "404":
			return os.ErrNotExist
		}
	}
	return err
}
