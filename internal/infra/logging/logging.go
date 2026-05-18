package logging

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	rotatelogs "github.com/lestrrat-go/file-rotatelogs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	RotationModeSize = "size"
	RotationModeTime = "time"
)

var (
	currentMu       sync.RWMutex
	currentSettings config.Settings
	currentRole     string
	currentLogger   *zap.Logger
)

type Shutdown func() error

func Setup(settings config.Settings, role string) (Shutdown, error) {
	role = sanitizeRole(role)
	if err := validate(settings); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(settings.LogDir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}

	writer, closeWriter, err := rotationWriter(settings, role)
	if err != nil {
		return nil, err
	}
	level, err := zapcore.ParseLevel(strings.ToLower(strings.TrimSpace(settings.LogLevel)))
	if err != nil {
		_ = closeWriter()
		return nil, fmt.Errorf("invalid LOG_LEVEL: %w", err)
	}

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "time"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig),
		zapcore.Lock(zapcore.AddSync(writer)),
		level,
	)
	core = newDefaultFieldsCore(core)
	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)).With(zap.String("role", role))
	restoreStd := zap.RedirectStdLog(logger.Named("stdlog"))
	previousGlobals := zap.ReplaceGlobals(logger)

	currentMu.Lock()
	currentSettings = settings
	currentRole = role
	currentLogger = logger
	currentMu.Unlock()

	stopCleaner := startCleaner(settings, role)
	logger.Info("logging initialized",
		zap.String("source", "logging"),
		Event("app.lifecycle"),
		Group("app"),
		Method("setup"),
		Status(StatusOK),
		zap.Any(FieldDuration, nil),
		zap.String("dir", settings.LogDir),
		zap.String("rotationMode", settings.LogRotationMode),
	)
	return func() error {
		stopCleaner()
		restoreStd()
		previousGlobals()
		currentMu.Lock()
		currentLogger = nil
		currentMu.Unlock()
		err := logger.Sync()
		if closeErr := closeWriter(); err == nil {
			err = closeErr
		}
		return err
	}, nil
}

func Logger() *zap.Logger {
	currentMu.RLock()
	defer currentMu.RUnlock()
	if currentLogger != nil {
		return currentLogger
	}
	return zap.NewNop()
}

func Settings() (config.Settings, string) {
	currentMu.RLock()
	defer currentMu.RUnlock()
	return currentSettings, currentRole
}

func validate(settings config.Settings) error {
	mode := strings.ToLower(strings.TrimSpace(settings.LogRotationMode))
	if mode == "" {
		mode = RotationModeSize
	}
	switch mode {
	case RotationModeSize:
		if settings.LogRotationSizeMB <= 0 {
			return errors.New("LOG_ROTATION_SIZE_MB must be greater than zero")
		}
		if settings.LogRotationTotalSizeMB <= 0 {
			return errors.New("LOG_ROTATION_TOTAL_SIZE_MB must be greater than zero")
		}
	case RotationModeTime:
		if settings.LogRotationMaxAgeDays <= 0 {
			return errors.New("LOG_ROTATION_MAX_AGE_DAYS must be greater than zero")
		}
	default:
		return fmt.Errorf("unsupported LOG_ROTATION_MODE %q", settings.LogRotationMode)
	}
	if strings.TrimSpace(settings.LogDir) == "" {
		return errors.New("LOG_DIR is required")
	}
	return nil
}

func rotationWriter(settings config.Settings, role string) (io.Writer, func() error, error) {
	mode := strings.ToLower(strings.TrimSpace(settings.LogRotationMode))
	if mode == "" {
		mode = RotationModeSize
	}
	switch mode {
	case RotationModeTime:
		pattern := filepath.Join(settings.LogDir, role+".%Y%m%d.log")
		writer, err := rotatelogs.New(
			pattern,
			rotatelogs.WithRotationTime(24*time.Hour),
			rotatelogs.WithMaxAge(time.Duration(settings.LogRotationMaxAgeDays)*24*time.Hour),
		)
		if err != nil {
			return nil, nil, err
		}
		return writer, writer.Close, nil
	default:
		writer := &lumberjack.Logger{
			Filename:   filepath.Join(settings.LogDir, role+".log"),
			MaxSize:    settings.LogRotationSizeMB,
			MaxBackups: 0,
			MaxAge:     0,
			Compress:   false,
		}
		return writer, writer.Close, nil
	}
}

func startCleaner(settings config.Settings, role string) func() {
	done := make(chan struct{})
	var once sync.Once
	clean := func() {
		if err := Clean(settings, role); err != nil {
			Logger().Error("log cleanup failed",
				Event("app.lifecycle"),
				Group("logging"),
				Method("cleanup"),
				Status(StatusError),
				DurationMS(-1),
				zap.Error(err),
			)
		}
	}
	clean()
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				clean()
			case <-done:
				return
			}
		}
	}()
	return func() {
		once.Do(func() { close(done) })
	}
}

func Clean(settings config.Settings, activeRole string) error {
	entries, err := os.ReadDir(settings.LogDir)
	if err != nil {
		return err
	}
	type candidate struct {
		path     string
		modified time.Time
		size     int64
		active   bool
	}
	candidates := make([]candidate, 0, len(entries))
	var total int64
	now := time.Now()
	maxAge := time.Duration(settings.LogRotationMaxAgeDays) * 24 * time.Hour
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := entry.Name()
		if !isLogFile(name) {
			continue
		}
		path := filepath.Join(settings.LogDir, name)
		active := activeLogFile(name, activeRole)
		if strings.EqualFold(settings.LogRotationMode, RotationModeTime) && maxAge > 0 && now.Sub(info.ModTime()) > maxAge && !active {
			_ = os.Remove(path)
			continue
		}
		candidates = append(candidates, candidate{path: path, modified: info.ModTime(), size: info.Size(), active: active})
		total += info.Size()
	}
	if !strings.EqualFold(settings.LogRotationMode, RotationModeSize) {
		return nil
	}
	limit := int64(settings.LogRotationTotalSizeMB) * 1024 * 1024
	if limit <= 0 || total <= limit {
		return nil
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modified.Before(candidates[j].modified)
	})
	for _, item := range candidates {
		if total <= limit {
			break
		}
		if item.active {
			continue
		}
		if err := os.Remove(item.path); err == nil {
			total -= item.size
		}
	}
	return nil
}

func isLogFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".log")
}

func activeLogFile(name string, currentRole string) bool {
	if !strings.HasSuffix(strings.ToLower(name), ".log") {
		return false
	}
	role := strings.TrimSuffix(name, ".log")
	if currentRole != "" && role == sanitizeRole(currentRole) {
		return true
	}
	switch role {
	case "serve", "worker", "scheduler", "message-subscription-listener", "migrate",
		"message-subscription-login", "message-subscription-login-start",
		"message-subscription-login-verify", "message-subscription-mtproto-test":
		return true
	default:
		return false
	}
}

func sanitizeRole(role string) string {
	role = strings.TrimSpace(strings.ToLower(role))
	if role == "" {
		return "app"
	}
	var b strings.Builder
	for _, r := range role {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}
