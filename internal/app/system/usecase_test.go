package system_test

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"

	appsystem "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/system"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/connect"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	gormuow "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/uow"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRecordAndLoadServiceHeartbeat(t *testing.T) {
	db := newHeartbeatTestDB(t)
	usecase := appsystem.NewUsecase(gormrepo.NewSystemRepository(db), gormuow.NewSystemUnitOfWork(db))
	if err := usecase.RecordHeartbeat(t.Context(), "worker", "redis", ""); err != nil {
		t.Fatal(err)
	}
	heartbeat, err := usecase.LoadHeartbeat(t.Context(), "worker")
	if err != nil {
		t.Fatal(err)
	}
	if heartbeat.Service != "worker" || heartbeat.Mode != "redis" || heartbeat.Status != "running" || heartbeat.PID == 0 {
		t.Fatalf("heartbeat mismatch: %+v", heartbeat)
	}
	age := appsystem.HeartbeatAgeSeconds(heartbeat, time.Now())
	if age == nil || *age < 0 {
		t.Fatalf("invalid heartbeat age: %v", age)
	}
}

func TestRecordHeartbeatCreatesWithoutRecordNotFoundLog(t *testing.T) {
	var logs bytes.Buffer
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.New(log.New(&logs, "", 0), logger.Config{LogLevel: logger.Warn}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	usecase := appsystem.NewUsecase(gormrepo.NewSystemRepository(db), gormuow.NewSystemUnitOfWork(db))
	if err := usecase.RecordHeartbeat(t.Context(), "telegram_listener", "redis", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "record not found") {
		t.Fatalf("heartbeat creation should not log record not found: %s", logs.String())
	}
}

func TestMarkStopped(t *testing.T) {
	db := newHeartbeatTestDB(t)
	usecase := appsystem.NewUsecase(gormrepo.NewSystemRepository(db), gormuow.NewSystemUnitOfWork(db))
	if err := usecase.MarkStopped(t.Context(), "scheduler", "redis", "shutdown"); err != nil {
		t.Fatal(err)
	}
	heartbeat, err := usecase.LoadHeartbeat(t.Context(), "scheduler")
	if err != nil {
		t.Fatal(err)
	}
	if heartbeat.Status != "stopped" || heartbeat.Error != "shutdown" {
		t.Fatalf("stopped heartbeat mismatch: %+v", heartbeat)
	}
}

func TestDeleteInvalidHeartbeatRemovesPayloadWithoutTimestamp(t *testing.T) {
	db := newHeartbeatTestDB(t)
	repo := gormrepo.NewSystemRepository(db)
	if err := repo.SaveAppSetting(t.Context(), &domainsettings.AppSetting{
		Key:       appsystem.HeartbeatKey("message_subscription_listener"),
		Value:     domainkernel.NewJSON(map[string]any{"service": "message_subscription_listener", "status": "running"}),
		UpdatedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	usecase := appsystem.NewUsecase(repo, gormuow.NewSystemUnitOfWork(db))
	deleted, err := usecase.DeleteInvalidHeartbeat(t.Context(), "message_subscription_listener")
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatalf("expected invalid heartbeat to be deleted")
	}
	if heartbeat, err := usecase.LoadHeartbeat(t.Context(), "message_subscription_listener"); err != nil || heartbeat != nil {
		t.Fatalf("expected deleted heartbeat to be absent")
	}
}

func newHeartbeatTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}
