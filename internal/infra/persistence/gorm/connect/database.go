package database

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	infralogging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/logging"
	gormmigrate "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/migrate"
	"github.com/glebarez/sqlite"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type ConnectionInfo struct {
	Backend string
	Target  string
	Driver  string
}

func Open(settings config.Settings) (*gorm.DB, error) {
	dialector, err := dialector(settings.DatabaseURL)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: zapGORMLogger()})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetConnMaxLifetime(time.Hour)
	return db, nil
}

func gormLogger(out io.Writer) logger.Interface {
	return logger.New(log.New(out, "\r\n", log.LstdFlags), logger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  logger.Warn,
		IgnoreRecordNotFoundError: true,
		Colorful:                  true,
	})
}

type zapLogger struct {
	level         logger.LogLevel
	slowThreshold time.Duration
}

func zapGORMLogger() logger.Interface {
	return zapLogger{level: logger.Warn, slowThreshold: 200 * time.Millisecond}
}

func (l zapLogger) LogMode(level logger.LogLevel) logger.Interface {
	l.level = level
	return l
}

func (l zapLogger) Info(context.Context, string, ...any) {}

func (l zapLogger) Warn(_ context.Context, message string, args ...any) {
	if l.level >= logger.Warn {
		infralogging.Logger().Warn(fmt.Sprintf(message, args...),
			zap.String("source", "gorm"),
			infralogging.Event("gorm.warning"),
			infralogging.Group("database"),
			infralogging.Method("query"),
			infralogging.Status(infralogging.StatusWarning),
		)
	}
}

func (l zapLogger) Error(_ context.Context, message string, args ...any) {
	if l.level >= logger.Error {
		infralogging.Logger().Error(fmt.Sprintf(message, args...),
			zap.String("source", "gorm"),
			infralogging.Event("gorm.query_failed"),
			infralogging.Group("database"),
			infralogging.Method("query"),
			infralogging.Status(infralogging.StatusError),
		)
	}
}

func (l zapLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= logger.Silent {
		return
	}
	elapsed := time.Since(begin)
	sql, rows := fc()
	fields := []zap.Field{
		zap.String("source", "gorm"),
		infralogging.Group("database"),
		infralogging.Method("query"),
		zap.Duration("elapsed", elapsed),
		infralogging.DurationMS(elapsed.Milliseconds()),
		zap.Int64("rows", rows),
		zap.String("sql", sql),
	}
	switch {
	case err != nil && l.level >= logger.Error && !errors.Is(err, gorm.ErrRecordNotFound):
		infralogging.Logger().Error("gorm query failed", append(fields, infralogging.Event("gorm.query_failed"), infralogging.Status(infralogging.StatusError), zap.Error(err))...)
	case elapsed > l.slowThreshold && l.level >= logger.Warn:
		infralogging.Logger().Warn("gorm slow query", append(fields, infralogging.Event("gorm.slow_query"), infralogging.Status(infralogging.StatusSlow))...)
	}
}

func DescribeConnection(databaseURL string, db *gorm.DB) ConnectionInfo {
	driver := ""
	if db != nil && db.Dialector != nil {
		driver = db.Dialector.Name()
	}
	backend := databaseBackend(databaseURL, driver)
	target := databaseTarget(databaseURL, backend)
	if target == "" {
		target = "current connection"
	}
	return ConnectionInfo{Backend: backend, Target: target, Driver: driver}
}

func AutoMigrate(db *gorm.DB) error {
	return gormmigrate.AutoMigrate(db)
}

func dialector(databaseURL string) (gorm.Dialector, error) {
	if strings.HasPrefix(databaseURL, "sqlite+aiosqlite:///") {
		path := strings.TrimPrefix(databaseURL, "sqlite+aiosqlite:///")
		if err := ensureParent(path); err != nil {
			return nil, err
		}
		return sqlite.Open(path), nil
	}
	if strings.HasPrefix(databaseURL, "sqlite:///") {
		path := strings.TrimPrefix(databaseURL, "sqlite:///")
		if err := ensureParent(path); err != nil {
			return nil, err
		}
		return sqlite.Open(path), nil
	}
	if strings.HasPrefix(databaseURL, "postgresql+asyncpg://") {
		return postgres.Open("postgresql://" + strings.TrimPrefix(databaseURL, "postgresql+asyncpg://")), nil
	}
	if strings.HasPrefix(databaseURL, "postgres://") || strings.HasPrefix(databaseURL, "postgresql://") {
		return postgres.Open(databaseURL), nil
	}
	if parsed, err := url.Parse(databaseURL); err == nil && parsed.Scheme == "" && databaseURL != "" {
		if err := ensureParent(databaseURL); err != nil {
			return nil, err
		}
		return sqlite.Open(databaseURL), nil
	}
	return nil, fmt.Errorf("unsupported DATABASE_URL: %s", databaseURL)
}

func databaseBackend(databaseURL string, driver string) string {
	normalized := strings.ToLower(strings.TrimSpace(databaseURL))
	switch {
	case strings.HasPrefix(normalized, "sqlite+aiosqlite:///"), strings.HasPrefix(normalized, "sqlite:///"):
		return "SQLite"
	case strings.HasPrefix(normalized, "postgresql+asyncpg://"), strings.HasPrefix(normalized, "postgres://"), strings.HasPrefix(normalized, "postgresql://"):
		return "PostgreSQL"
	}
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "sqlite":
		return "SQLite"
	case "postgres":
		return "PostgreSQL"
	case "":
		return "Database"
	default:
		return driver
	}
}

func databaseTarget(databaseURL string, backend string) string {
	trimmed := strings.TrimSpace(databaseURL)
	if trimmed == "" {
		return ""
	}
	if backend == "SQLite" {
		switch {
		case strings.HasPrefix(trimmed, "sqlite+aiosqlite:///"):
			return strings.TrimPrefix(trimmed, "sqlite+aiosqlite:///")
		case strings.HasPrefix(trimmed, "sqlite:///"):
			return strings.TrimPrefix(trimmed, "sqlite:///")
		default:
			return trimmed
		}
	}
	if backend == "PostgreSQL" {
		normalized := trimmed
		if strings.HasPrefix(normalized, "postgresql+asyncpg://") {
			normalized = "postgresql://" + strings.TrimPrefix(normalized, "postgresql+asyncpg://")
		}
		parsed, err := url.Parse(normalized)
		if err != nil {
			return "configured PostgreSQL server"
		}
		databaseName := strings.TrimPrefix(parsed.EscapedPath(), "/")
		target := parsed.Host
		if databaseName != "" {
			target += "/" + databaseName
		}
		if target == "" {
			return "configured PostgreSQL server"
		}
		return target
	}
	return trimmed
}

func ensureParent(path string) error {
	if path == "" || path == ":memory:" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}
