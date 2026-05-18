package meeting

import (
	"bytes"
	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"
	"log"
	"strings"
	"testing"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/connect"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMeetingDailyTokenBudgetUsesDefaultWithoutRecordNotFoundLog(t *testing.T) {
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

	budget := meetingDailyTokenBudget(db, config.Settings{MeetingDailyTokenBudget: 123})
	if budget != 123 {
		t.Fatalf("expected default budget 123, got %d", budget)
	}
	if strings.Contains(logs.String(), "record not found") {
		t.Fatalf("missing optional token budget setting should not log record not found: %s", logs.String())
	}
}

func TestMeetingDailyTokenBudgetUsesAppSettingOverride(t *testing.T) {
	db := newMeetingTestDB(t)
	if err := db.Create(&domainsettings.AppSetting{Key: "MEETING_DAILY_TOKEN_BUDGET", Value: JSON(map[string]any{"value": 456})}).Error; err != nil {
		t.Fatal(err)
	}

	budget := meetingDailyTokenBudget(db, config.Settings{MeetingDailyTokenBudget: 123})
	if budget != 456 {
		t.Fatalf("expected app setting override 456, got %d", budget)
	}
}

func TestMeetingDailyTokenBudgetAllowsUnlimitedSentinel(t *testing.T) {
	db := newMeetingTestDB(t)
	if err := db.Create(&domainsettings.AppSetting{Key: "MEETING_DAILY_TOKEN_BUDGET", Value: JSON(map[string]any{"value": -1})}).Error; err != nil {
		t.Fatal(err)
	}

	budget := meetingDailyTokenBudget(db, config.Settings{MeetingDailyTokenBudget: 123})
	if budget != -1 {
		t.Fatalf("expected unlimited budget sentinel -1, got %d", budget)
	}
}
