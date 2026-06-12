package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	appai "github.com/TradingCopilotDevs/TradingCopilot/internal/app/ai"
	appmessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/messaging"
	appresearch "github.com/TradingCopilotDevs/TradingCopilot/internal/app/research"
	appsystem "github.com/TradingCopilotDevs/TradingCopilot/internal/app/system"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainmsg "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/messaging"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	inframessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/messaging"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/connect"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	gormuow "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/uow"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type legacyRuntimePaperAccount struct {
	ID          uint `gorm:"primaryKey"`
	Name        string
	InitialCash int64
	Cash        int64
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (legacyRuntimePaperAccount) TableName() string { return "paper_accounts" }

type legacyRuntimeResearchTeam struct {
	ID             uint `gorm:"primaryKey"`
	Name           string
	Description    string
	PaperAccountID uint `gorm:"uniqueIndex"`
	Active         bool `gorm:"default:true"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (legacyRuntimeResearchTeam) TableName() string { return "research_teams" }

func TestShouldRunLocalBackgroundLoopsOnlyInLocalMode(t *testing.T) {
	if !ShouldRunLocalBackgroundLoops(config.Settings{MeetingDispatchMode: "local"}) {
		t.Fatal("local mode should run local background loops")
	}
	if !ShouldRunLocalBackgroundLoops(config.Settings{}) {
		t.Fatal("empty mode should default to local background loops")
	}
	if ShouldRunLocalBackgroundLoops(config.Settings{MeetingDispatchMode: "auto"}) {
		t.Fatal("auto mode should not run local background loops")
	}
	if ShouldRunLocalBackgroundLoops(config.Settings{MeetingDispatchMode: "redis"}) {
		t.Fatal("redis mode should not run local background loops")
	}
}

func TestSeedDefaultsCreatesDefaultMessageFiltersAndResearchTeamsOnce(t *testing.T) {
	db := newRuntimeTestDB(t)
	runner := New(config.Settings{}, db, security.New(config.Settings{}))

	if err := runner.SeedDefaults(); err != nil {
		t.Fatal(err)
	}
	var filter persistmodel.MessageSubscriptionFilter
	if err := db.First(&filter, "is_default = ?", true).Error; err != nil {
		t.Fatal(err)
	}
	if filter.Name != appmessaging.DefaultFilterSeed.Name {
		t.Fatalf("default filter name = %q", filter.Name)
	}
	var predictionFilter persistmodel.MessageSubscriptionFilter
	if err := db.First(&predictionFilter, "name = ?", appmessaging.DefaultPredictionFilterSeed.Name).Error; err != nil {
		t.Fatal(err)
	}
	if predictionFilter.IsDefault {
		t.Fatalf("prediction filter must not replace the global A-share default filter: %+v", predictionFilter)
	}
	var team persistmodel.ResearchTeam
	if err := db.First(&team, "name = ?", appresearch.DefaultTeamName).Error; err != nil {
		t.Fatal(err)
	}
	var roleCount int64
	if err := db.Model(&persistmodel.ResearchTeamRole{}).Where("research_team_id = ?", team.ID).Count(&roleCount).Error; err != nil {
		t.Fatal(err)
	}
	if roleCount != int64(len(appai.DefaultRoles)) {
		t.Fatalf("default team role count = %d, want %d", roleCount, len(appai.DefaultRoles))
	}
	var predictionTeam persistmodel.ResearchTeam
	if err := db.First(&predictionTeam, "name = ?", appresearch.DefaultPredictionTeamName).Error; err != nil {
		t.Fatal(err)
	}
	if predictionTeam.AssetClass != appresearch.AssetClassPredictionMarket || predictionTeam.PaperAccountID != nil {
		t.Fatalf("prediction team should be isolated from paper accounts: %+v", predictionTeam)
	}
	var predictionRoleCount int64
	if err := db.Model(&persistmodel.ResearchTeamRole{}).Where("research_team_id = ?", predictionTeam.ID).Count(&predictionRoleCount).Error; err != nil {
		t.Fatal(err)
	}
	if predictionRoleCount != int64(len(appresearch.PredictionMarketDefaultRoles)) {
		t.Fatalf("prediction team role count = %d, want %d", predictionRoleCount, len(appresearch.PredictionMarketDefaultRoles))
	}

	if err := db.Model(&persistmodel.ResearchTeamRole{}).
		Where("research_team_id = ? AND key = ?", team.ID, "moderator").
		Update("name", "Custom Moderator").Error; err != nil {
		t.Fatal(err)
	}
	if err := runner.SeedDefaults(); err != nil {
		t.Fatal(err)
	}
	var teamCount int64
	if err := db.Model(&persistmodel.ResearchTeam{}).Count(&teamCount).Error; err != nil {
		t.Fatal(err)
	}
	if teamCount != 2 {
		t.Fatalf("seed defaults should not create duplicate research teams, got %d", teamCount)
	}
	var filterCount int64
	if err := db.Model(&persistmodel.MessageSubscriptionFilter{}).Count(&filterCount).Error; err != nil {
		t.Fatal(err)
	}
	if filterCount != 2 {
		t.Fatalf("seed defaults should not create duplicate filters, got %d", filterCount)
	}
	var moderator persistmodel.ResearchTeamRole
	if err := db.First(&moderator, "research_team_id = ? AND key = ?", team.ID, "moderator").Error; err != nil {
		t.Fatal(err)
	}
	if moderator.Name != "Custom Moderator" {
		t.Fatalf("seed defaults rewrote an existing team role: %+v", moderator)
	}
}

func TestSeedDefaultsAfterLegacySchemaUpgradeCreatesPredictionDefaults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&legacyRuntimePaperAccount{}, &legacyRuntimeResearchTeam{}); err != nil {
		t.Fatal(err)
	}
	account := legacyRuntimePaperAccount{Name: "legacy account", InitialCash: 100000, Cash: 100000, Active: true}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	legacyTeam := legacyRuntimeResearchTeam{Name: "legacy A-share team", PaperAccountID: account.ID, Active: true}
	if err := db.Create(&legacyTeam).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	if err := New(config.Settings{}, db, security.New(config.Settings{})).SeedDefaults(); err != nil {
		t.Fatal(err)
	}

	var upgradedLegacy persistmodel.ResearchTeam
	if err := db.First(&upgradedLegacy, "name = ?", legacyTeam.Name).Error; err != nil {
		t.Fatal(err)
	}
	if upgradedLegacy.AssetClass != appresearch.AssetClassAShare {
		t.Fatalf("legacy team asset class should default to a_share, got %+v", upgradedLegacy)
	}
	var predictionTeam persistmodel.ResearchTeam
	if err := db.First(&predictionTeam, "name = ?", appresearch.DefaultPredictionTeamName).Error; err != nil {
		t.Fatal(err)
	}
	if predictionTeam.AssetClass != appresearch.AssetClassPredictionMarket || predictionTeam.PaperAccountID != nil {
		t.Fatalf("upgraded database should seed paperless prediction team: %+v", predictionTeam)
	}
	var predictionFilter persistmodel.MessageSubscriptionFilter
	if err := db.First(&predictionFilter, "name = ?", appmessaging.DefaultPredictionFilterSeed.Name).Error; err != nil {
		t.Fatal(err)
	}
	if predictionFilter.PromptTemplate == "" || !strings.Contains(predictionFilter.PromptTemplate, "match_confidence") {
		t.Fatalf("prediction filter prompt was not seeded correctly: %+v", predictionFilter)
	}
}

func TestRecoverLocalQueuedMeetingsSchedulesLocalRun(t *testing.T) {
	db := newRuntimeTestDB(t)
	old := time.Now().Add(-time.Minute)
	meeting := domainmeeting.Meeting{Topic: "queued", Status: domainkernel.MeetingQueued, TriggerSource: "manual", CreatedAt: old}
	if err := db.Create(&meeting).Error; err != nil {
		t.Fatal(err)
	}
	var started []uint
	runner := New(config.Settings{MeetingStaleAfter: time.Hour, MeetingAutoRequeueLimit: 2}, db, security.New(config.Settings{})).
		WithLocalMeetingStarter(func(_ *gorm.DB, meetingID uint) bool {
			started = append(started, meetingID)
			return true
		})

	recovered, err := runner.RecoverLocalQueuedMeetings(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 || len(started) != 1 || started[0] != meeting.ID {
		t.Fatalf("local recovery mismatch recovered=%d started=%v", recovered, started)
	}
	var event domainmeeting.Event
	if err := db.First(&event, "meeting_id = ? AND type = ?", meeting.ID, domainkernel.EventSystem).Error; err != nil {
		t.Fatal(err)
	}
	if string(event.Payload) == "" {
		t.Fatal("expected recovery payload")
	}
}

func TestMessageSubscriptionHeartbeatLoopRecordsWithCanceledParentContext(t *testing.T) {
	db := newRuntimeTestDB(t)
	runner := New(config.Settings{MeetingDispatchMode: "local"}, db, security.New(config.Settings{}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner.messageSubscriptionHeartbeatLoop(ctx)

	heartbeat, err := appsystem.NewUsecase(gormrepo.NewSystemRepository(db), gormuow.NewSystemUnitOfWork(db)).
		LoadHeartbeat(context.Background(), appmessaging.SubscriptionListenerServiceName)
	if err != nil {
		t.Fatal(err)
	}
	if heartbeat == nil || heartbeat.Status != "running" || heartbeat.Service != appmessaging.SubscriptionListenerServiceName {
		t.Fatalf("expected running message subscription heartbeat, got %+v", heartbeat)
	}
}

func TestMessageSubscriptionListenerAcquiresLeaseWithoutMTProtoConfig(t *testing.T) {
	db := newRuntimeTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	runner := New(config.Settings{MeetingDispatchMode: "redis"}, db, security.New(config.Settings{}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.runMessageSubscriptionListener(ctx)
	}()
	defer cancel()

	deadline := time.Now().Add(2 * time.Second)
	for {
		var setting persistmodel.AppSetting
		if err := db.First(&setting, "key = ?", appmessaging.SubscriptionListenerLeaseKey).Error; err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("message subscription listener did not acquire lease without MTProto config")
		}
		time.Sleep(10 * time.Millisecond)
	}

	heartbeat, err := appsystem.NewUsecase(gormrepo.NewSystemRepository(db), gormuow.NewSystemUnitOfWork(db)).
		LoadHeartbeat(context.Background(), appmessaging.SubscriptionListenerServiceName)
	if err != nil {
		t.Fatal(err)
	}
	if heartbeat == nil || heartbeat.Status != "running" || heartbeat.Error != "" {
		t.Fatalf("expected running listener heartbeat without MTProto config error, got %+v", heartbeat)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("message subscription listener did not stop after context cancellation")
	}
}

func TestHandleLiveMessageDoesNotReuseCanceledCallbackContext(t *testing.T) {
	db := newRuntimeTestDB(t)
	subscription := persistmodel.MessageSubscription{
		Provider:    domainmsg.ProviderTelegramChannel,
		Title:       "News",
		SourceRef:   "@news",
		Enabled:     true,
		CollectFrom: time.Now().Add(-time.Hour),
	}
	if err := db.Create(&subscription).Error; err != nil {
		t.Fatal(err)
	}
	runner := New(config.Settings{AIChatTimeout: time.Second, AIJSONMaxAttempts: 1}, db, security.New(config.Settings{}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := runner.handleLiveMessage(ctx, inframessaging.LiveMessage{
		SubscriptionID:  subscription.ID,
		SourceMessageID: "42",
		MessageTime:     time.Now(),
		Text:            "market update",
		Raw:             domainkernel.NewJSON(map[string]any{"source": "test"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	var message persistmodel.IngestedMessage
	if err := db.First(&message, "subscription_id = ? AND source_message_id = ?", subscription.ID, "42").Error; err != nil {
		t.Fatal(err)
	}
	if message.FilterStatus != domainmsg.FilterStatusUnfiltered || message.FilterReason != nil {
		t.Fatalf("expected live message to be persisted before async filtering, got status=%q reason=%+v", message.FilterStatus, message.FilterReason)
	}
}

func TestDispatchMessageSubscriptionMeetingsWritesDispatchEvent(t *testing.T) {
	db := newRuntimeTestDB(t)
	meeting := domainmeeting.Meeting{Topic: "message trigger", Status: domainkernel.MeetingQueued, TriggerSource: "message_subscription", Tags: domainkernel.NewJSON(nil)}
	if err := db.Create(&meeting).Error; err != nil {
		t.Fatal(err)
	}
	var started []uint
	runner := New(config.Settings{MeetingDispatchMode: "local"}, db, security.New(config.Settings{})).
		WithLocalMeetingStarter(func(_ *gorm.DB, meetingID uint) bool {
			started = append(started, meetingID)
			return true
		})

	if err := runner.dispatchMessageSubscriptionMeetings(context.Background(), []*domainmeeting.Meeting{&meeting}); err != nil {
		t.Fatal(err)
	}
	if len(started) != 1 {
		t.Fatalf("expected one dispatched meeting, got %v", started)
	}
	var dispatchEvent persistmodel.MeetingEvent
	if err := db.First(&dispatchEvent, "meeting_id = ? AND content = ?", started[0], "Meeting queued for local background execution.").Error; err != nil {
		t.Fatal(err)
	}
}

func TestAcquireMessageSubscriptionLeaseExcludesOtherOwners(t *testing.T) {
	db := newRuntimeTestDB(t)
	runner := New(config.Settings{MeetingDispatchMode: "local"}, db, security.New(config.Settings{}))
	ctx := context.Background()

	acquired, err := runner.acquireMessageSubscriptionLease(ctx, "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("expected owner-a to acquire empty lease")
	}
	acquired, err = runner.acquireMessageSubscriptionLease(ctx, "owner-b")
	if err != nil {
		t.Fatal(err)
	}
	if acquired {
		t.Fatal("expected owner-b to be blocked while owner-a lease is fresh")
	}
	acquired, err = runner.acquireMessageSubscriptionLease(ctx, "owner-a")
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("expected current owner to renew lease")
	}
}

func newRuntimeTestDB(t *testing.T) *gorm.DB {
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
