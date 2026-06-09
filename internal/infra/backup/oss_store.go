package backup

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	appops "github.com/TradingCopilotDevs/TradingCopilot/internal/app/ops"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

type ossArchiveAPI interface {
	ListObjects(...oss.Option) (oss.ListObjectsResult, error)
	PutObject(string, io.Reader, ...oss.Option) error
	GetObjectMeta(string, ...oss.Option) (http.Header, error)
	GetObject(string, ...oss.Option) (io.ReadCloser, error)
	DeleteObject(string, ...oss.Option) error
}

type OSSArchiveStore struct {
	client     ossArchiveAPI
	bucketName string
	prefix     string
	region     string
	root       string
}

func NewOSSArchiveStore(client ossArchiveAPI, bucketName string, prefix string, region string) OSSArchiveStore {
	prefix = cleanObjectPrefix(prefix)
	root := "oss://" + strings.TrimSpace(bucketName)
	if prefix != "" {
		root += "/" + prefix
	}
	return OSSArchiveStore{
		client:     client,
		bucketName: strings.TrimSpace(bucketName),
		prefix:     prefix,
		region:     strings.TrimSpace(region),
		root:       root,
	}
}

func (s OSSArchiveStore) Provider() appops.BackupArchiveProvider {
	status := "ready"
	if s.client == nil || s.bucketName == "" {
		status = "blocked"
	}
	return appops.BackupArchiveProvider{
		Key:            "oss",
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

func (s OSSArchiveStore) Root() string {
	if strings.TrimSpace(s.root) == "" {
		return "oss://"
	}
	return s.root
}

func (s OSSArchiveStore) EnsureRoot(ctx context.Context) error {
	return s.ensureReady(ctx)
}

func (s OSSArchiveStore) ListArchives(ctx context.Context) ([]map[string]any, map[string]any, error) {
	if err := s.ensureReady(ctx); err != nil {
		return nil, nil, err
	}
	backups := []map[string]any{}
	marker := ""
	for {
		options := []oss.Option{oss.WithContext(ctx), oss.MaxKeys(1000)}
		if s.prefix != "" {
			options = append(options, oss.Prefix(s.prefix+"/"))
		}
		if marker != "" {
			options = append(options, oss.Marker(marker))
		}
		result, err := s.client.ListObjects(options...)
		if err != nil {
			return nil, nil, err
		}
		for _, object := range result.Objects {
			key := strings.TrimSpace(object.Key)
			name := path.Base(key)
			if name == "." || name == "" || !strings.HasSuffix(strings.ToLower(name), ".zip") {
				continue
			}
			backups = append(backups, map[string]any{
				"name":      name,
				"path":      s.objectURI(key),
				"sizeBytes": object.Size,
				"createdAt": object.LastModified,
			})
		}
		if !result.IsTruncated || strings.TrimSpace(result.NextMarker) == "" {
			break
		}
		marker = result.NextMarker
	}
	sortBackupArchives(backups)
	var latest map[string]any
	if len(backups) > 0 {
		latest = backups[0]
	}
	return backups, latest, nil
}

func (s OSSArchiveStore) CreateArchive(ctx context.Context, name string) (io.WriteCloser, string, error) {
	if err := s.ensureReady(ctx); err != nil {
		return nil, "", err
	}
	name, err := cleanArchiveName(name)
	if err != nil {
		return nil, "", err
	}
	temp, err := os.CreateTemp("", "tradingcopilot-oss-backup-*.zip")
	if err != nil {
		return nil, "", err
	}
	key := s.objectKey(name)
	return &ossArchiveUploadWriter{
		ctx:      ctx,
		client:   s.client,
		key:      key,
		file:     temp,
		tempPath: temp.Name(),
	}, s.objectURI(key), nil
}

func (s OSSArchiveStore) StatArchive(ctx context.Context, name string) (map[string]any, error) {
	if err := s.ensureReady(ctx); err != nil {
		return nil, err
	}
	name, err := cleanArchiveName(name)
	if err != nil {
		return nil, err
	}
	key := s.objectKey(name)
	header, err := s.client.GetObjectMeta(key, oss.WithContext(ctx))
	if err != nil {
		return nil, mapOSSNotFound(err)
	}
	size := int64(0)
	if value := strings.TrimSpace(header.Get("Content-Length")); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			size = parsed
		}
	}
	createdAt := time.Time{}
	if value := strings.TrimSpace(header.Get("Last-Modified")); value != "" {
		if parsed, err := http.ParseTime(value); err == nil {
			createdAt = parsed
		}
	}
	return map[string]any{
		"name":      name,
		"path":      s.objectURI(key),
		"sizeBytes": size,
		"createdAt": createdAt,
	}, nil
}

func (s OSSArchiveStore) ResolveArchive(ctx context.Context, name string) (string, func(), error) {
	if err := s.ensureReady(ctx); err != nil {
		return "", nil, err
	}
	name, err := cleanArchiveName(name)
	if err != nil {
		return "", nil, err
	}
	key := s.objectKey(name)
	body, err := s.client.GetObject(key, oss.WithContext(ctx))
	if err != nil {
		return "", nil, mapOSSNotFound(err)
	}
	defer body.Close()
	temp, err := os.CreateTemp("", "tradingcopilot-oss-restore-*.zip")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.Remove(temp.Name())
	}
	if _, err := io.Copy(temp, body); err != nil {
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

func (s OSSArchiveStore) DeleteArchive(ctx context.Context, name string) error {
	if err := s.ensureReady(ctx); err != nil {
		return err
	}
	name, err := cleanArchiveName(name)
	if err != nil {
		return err
	}
	return s.client.DeleteObject(s.objectKey(name), oss.WithContext(ctx))
}

func (s OSSArchiveStore) ensureReady(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.client == nil || strings.TrimSpace(s.bucketName) == "" {
		return errors.New("oss archive store is not configured")
	}
	return nil
}

func (s OSSArchiveStore) objectKey(name string) string {
	if s.prefix == "" {
		return name
	}
	return s.prefix + "/" + name
}

func (s OSSArchiveStore) objectURI(key string) string {
	return "oss://" + s.bucketName + "/" + strings.TrimLeft(key, "/")
}

type ossArchiveUploadWriter struct {
	ctx      context.Context
	client   ossArchiveAPI
	key      string
	file     *os.File
	tempPath string
	closed   bool
}

func (w *ossArchiveUploadWriter) Write(p []byte) (int, error) {
	if w.closed {
		return 0, os.ErrClosed
	}
	return w.file.Write(p)
}

func (w *ossArchiveUploadWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	var errs []error
	if _, err := w.file.Seek(0, 0); err != nil {
		errs = append(errs, err)
	} else if err := w.client.PutObject(w.key, w.file, oss.WithContext(w.ctx)); err != nil {
		errs = append(errs, err)
	}
	if err := w.file.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := os.Remove(w.tempPath); err != nil && !os.IsNotExist(err) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func sortBackupArchives(backups []map[string]any) {
	sort.Slice(backups, func(i, j int) bool {
		left, _ := backups[i]["createdAt"].(time.Time)
		right, _ := backups[j]["createdAt"].(time.Time)
		return left.After(right)
	})
}

func mapOSSNotFound(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return os.ErrNotExist
	}
	var serviceErr oss.ServiceError
	if errors.As(err, &serviceErr) && isOSSNotFound(serviceErr.Code, serviceErr.StatusCode) {
		return os.ErrNotExist
	}
	var serviceErrPtr *oss.ServiceError
	if errors.As(err, &serviceErrPtr) && serviceErrPtr != nil && isOSSNotFound(serviceErrPtr.Code, serviceErrPtr.StatusCode) {
		return os.ErrNotExist
	}
	return err
}

func isOSSNotFound(code string, statusCode int) bool {
	switch code {
	case "NoSuchKey", "NoSuchBucket", "NoSuchObject", "NotFound":
		return true
	default:
		return statusCode == http.StatusNotFound
	}
}
