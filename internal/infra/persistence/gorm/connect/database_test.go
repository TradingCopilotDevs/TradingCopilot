package database

import (
	"bytes"
	"encoding/json"
	"errors"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	persistmodel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/model"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGORMLoggerIgnoresRecordNotFound(t *testing.T) {
	var logs bytes.Buffer
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: gormLogger(&logs)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&persistmodel.AdminUser{}); err != nil {
		t.Fatal(err)
	}
	err = db.Where("username = ?", "").First(&persistmodel.AdminUser{}).Error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected record not found, got %v", err)
	}
	if strings.Contains(logs.String(), "record not found") {
		t.Fatalf("expected record-not-found queries to stay out of runtime logs, got %s", logs.String())
	}
}

func TestDescribeConnectionReportsSQLiteAndPostgreSQLTargets(t *testing.T) {
	sqliteDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	info := DescribeConnection("sqlite+aiosqlite:///./data/local-dev.db", sqliteDB)
	if info.Backend != "SQLite" || info.Target != "./data/local-dev.db" {
		t.Fatalf("sqlite connection info mismatch: %+v", info)
	}

	info = DescribeConnection("postgresql+asyncpg://user:secret@localhost:5432/treadingcopilot?sslmode=disable", nil)
	if info.Backend != "PostgreSQL" || info.Target != "localhost:5432/treadingcopilot" {
		t.Fatalf("postgres connection info mismatch: %+v", info)
	}
}

func TestAutoMigrateCreatesSchemaTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []any{
		&persistmodel.PromptTemplate{},
		&persistmodel.ToolCallLog{},
		&persistmodel.PaperEquitySnapshot{},
	} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("expected table for %T", table)
		}
	}
	if err := db.Create(&persistmodel.Secret{Kind: domainkernel.SecretKindApp, Name: "runtime", EncryptedValue: "x"}).Error; err != nil {
		t.Fatalf("expected app secret kind insert: %v", err)
	}
	if err := db.Create(&persistmodel.PromptTemplate{Key: "recap", Title: "Recap", Content: "Hello", Variables: []byte(`{"topic":"string"}`)}).Error; err != nil {
		t.Fatalf("expected prompt template insert: %v", err)
	}
}

func TestAutoMigrateClearsCollectFromForEmptyMessageSubscriptions(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	emptySubscription := persistmodel.MessageSubscription{
		Provider:    "telegram_channel",
		Title:       "empty",
		SourceRef:   "@empty",
		Enabled:     true,
		CollectFrom: time.Now(),
	}
	withMessages := persistmodel.MessageSubscription{
		Provider:    "telegram_channel",
		Title:       "with-messages",
		SourceRef:   "@with_messages",
		Enabled:     true,
		CollectFrom: time.Now(),
	}
	if err := db.Create(&emptySubscription).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&withMessages).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&persistmodel.IngestedMessage{
		SubscriptionID:  withMessages.ID,
		Provider:        withMessages.Provider,
		SourceMessageID: "1",
		MessageTime:     time.Now(),
		Text:            "already collected",
	}).Error; err != nil {
		t.Fatal(err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	var loadedEmpty persistmodel.MessageSubscription
	if err := db.First(&loadedEmpty, emptySubscription.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !loadedEmpty.CollectFrom.IsZero() {
		t.Fatalf("empty subscription collect_from should be cleared, got %s", loadedEmpty.CollectFrom)
	}
	var loadedWithMessages persistmodel.MessageSubscription
	if err := db.First(&loadedWithMessages, withMessages.ID).Error; err != nil {
		t.Fatal(err)
	}
	if loadedWithMessages.CollectFrom.IsZero() {
		t.Fatalf("subscription with messages should keep collect_from")
	}
}

func TestAutoMigrateCancelsInvalidActiveWakePlans(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	invalid := persistmodel.WakePlan{TriggerType: domainkernel.WakeIndicator, Status: domainkernel.WakeActive, Reason: "invalid", TriggerConfig: datatypes.JSON([]byte(`{"topic":"missing condition"}`))}
	valid := persistmodel.WakePlan{TriggerType: domainkernel.WakeIndicator, Status: domainkernel.WakeActive, Reason: "valid", TriggerConfig: datatypes.JSON([]byte(`{"code":"600519","threshold":"100"}`))}
	if err := db.Create(&invalid).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&valid).Error; err != nil {
		t.Fatal(err)
	}

	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	var loadedInvalid persistmodel.WakePlan
	if err := db.First(&loadedInvalid, invalid.ID).Error; err != nil {
		t.Fatal(err)
	}
	if loadedInvalid.Status != domainkernel.WakeCancelled || loadedInvalid.ResultSummary == nil || !strings.Contains(*loadedInvalid.ResultSummary, "Invalid wake plan cancelled") {
		t.Fatalf("expected invalid active wake plan to be cancelled, got %+v", loadedInvalid)
	}
	var loadedValid persistmodel.WakePlan
	if err := db.First(&loadedValid, valid.ID).Error; err != nil {
		t.Fatal(err)
	}
	if loadedValid.Status != domainkernel.WakeActive {
		t.Fatalf("valid active wake plan should remain active, got %+v", loadedValid)
	}
}

func TestGORMTableSurfaceMatchesPersistenceModels(t *testing.T) {
	want := []string{
		"admin_users",
		"agent_roles",
		"ai_provider_models",
		"ai_providers",
		"app_settings",
		"daily_bars",
		"market_symbols",
		"message_subscription_research_teams",
		"message_subscription_filters",
		"message_subscriptions",
		"meeting_events",
		"meeting_references",
		"meetings",
		"ingested_messages",
		"paper_accounts",
		"paper_equity_snapshots",
		"paper_fills",
		"paper_orders",
		"paper_positions",
		"prompt_templates",
		"platform_adapters",
		"realtime_quotes",
		"research_team_roles",
		"research_teams",
		"risk_configs",
		"secrets",
		"telegram_channels",
		"telegram_messages",
		"tool_call_logs",
		"wake_plans",
		"watchlist_items",
	}
	var got []string
	for _, model := range persistmodel.Models() {
		table, ok := model.(interface{ TableName() string })
		if !ok {
			t.Fatalf("%T does not expose TableName", model)
		}
		got = append(got, table.TableName())
	}
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("table surface mismatch\nmissing:\n%s\nextra:\n%s", strings.Join(diffStrings(want, got), "\n"), strings.Join(diffStrings(got, want), "\n"))
	}
}

func TestTimescaleSetupSQLPreparesHypertables(t *testing.T) {
	sql := TimescaleSetupSQL()
	joined := strings.Join(sql, "\n")
	for _, expected := range []string{
		"CREATE EXTENSION IF NOT EXISTS timescaledb",
		"ALTER TABLE daily_bars DROP CONSTRAINT IF EXISTS daily_bars_pkey",
		"ALTER TABLE realtime_quotes DROP CONSTRAINT IF EXISTS realtime_quotes_pkey",
		"SELECT create_hypertable('daily_bars', 'trade_date', if_not_exists => TRUE, migrate_data => TRUE)",
		"SELECT create_hypertable('realtime_quotes', 'quote_time', if_not_exists => TRUE, migrate_data => TRUE)",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("missing Timescale SQL %q in:\n%s", expected, joined)
		}
	}
	if strings.Index(joined, "DROP CONSTRAINT IF EXISTS daily_bars_pkey") > strings.Index(joined, "create_hypertable('daily_bars'") {
		t.Fatalf("daily_bars primary key must be dropped before hypertable conversion:\n%s", joined)
	}
	if strings.Index(joined, "DROP CONSTRAINT IF EXISTS realtime_quotes_pkey") > strings.Index(joined, "create_hypertable('realtime_quotes'") {
		t.Fatalf("realtime_quotes primary key must be dropped before hypertable conversion:\n%s", joined)
	}
}

func TestTimescalePartitionColumnsStayNotNullInGORMModels(t *testing.T) {
	assertGORMTagContains(t, persistmodel.DailyBar{}, "TradeDate", "not null")
	assertGORMTagContains(t, persistmodel.RealtimeQuote{}, "QuoteTime", "not null")
}

func TestRiskConfigColumnNames(t *testing.T) {
	assertGORMTagContains(t, persistmodel.RiskConfig{}, "AllowChiNext", "column:allow_chinext")
	assertGORMTagContains(t, persistmodel.RiskConfig{}, "AllowETFLOF", "column:allow_etf_lof")
}

func TestAutoMigratePreservesJSONDefaultPayloadShapes(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	setting := persistmodel.AppSetting{Key: "JSON_DEFAULT_SETTING"}
	if err := db.Create(&setting).Error; err != nil {
		t.Fatal(err)
	}
	var loadedSetting persistmodel.AppSetting
	if err := db.First(&loadedSetting, "key = ?", setting.Key).Error; err != nil {
		t.Fatal(err)
	}
	assertJSONKind(t, loadedSetting.Value, map[string]any{})

	provider := persistmodel.AiProvider{Name: "json-default-provider", BaseURL: "https://example.test", DefaultModel: "m"}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	model := persistmodel.AiProviderModel{ProviderID: provider.ID, ModelID: "m", DisplayName: "m"}
	if err := db.Create(&model).Error; err != nil {
		t.Fatal(err)
	}
	var loadedModel persistmodel.AiProviderModel
	if err := db.First(&loadedModel, model.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertJSONKind(t, loadedModel.Raw, map[string]any{})

	role := persistmodel.AgentRole{Key: "json_defaults", Name: "JSON", Responsibility: "r", PromptTemplate: "p"}
	if err := db.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	var loadedRole persistmodel.AgentRole
	if err := db.First(&loadedRole, "key = ?", role.Key).Error; err != nil {
		t.Fatal(err)
	}
	assertJSONKind(t, loadedRole.ToolNames, []any{})
	assertJSONKind(t, loadedRole.SkillNames, []any{})

	quote := persistmodel.RealtimeQuote{Code: "600519"}
	if err := db.Create(&quote).Error; err != nil {
		t.Fatal(err)
	}
	var loadedQuote persistmodel.RealtimeQuote
	if err := db.First(&loadedQuote, quote.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertJSONKind(t, loadedQuote.Raw, map[string]any{})

	channel := persistmodel.TelegramChannel{Title: "json", ChannelRef: "@json"}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	message := persistmodel.TelegramMessage{ChannelID: channel.ID, MessageID: 1, Text: "json"}
	if err := db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	var loadedMessage persistmodel.TelegramMessage
	if err := db.First(&loadedMessage, message.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertJSONKind(t, loadedMessage.Raw, map[string]any{})
	assertJSONKind(t, loadedMessage.RelatedSymbols, []any{})

	meeting := persistmodel.Meeting{Topic: "defaults"}
	if err := db.Create(&meeting).Error; err != nil {
		t.Fatal(err)
	}
	var loadedMeeting persistmodel.Meeting
	if err := db.First(&loadedMeeting, meeting.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertJSONKind(t, loadedMeeting.Tags, []any{})

	event := persistmodel.MeetingEvent{MeetingID: meeting.ID, Type: domainkernel.EventSystem, Content: "json"}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	var loadedEvent persistmodel.MeetingEvent
	if err := db.First(&loadedEvent, event.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertJSONKind(t, loadedEvent.Payload, map[string]any{})

	template := persistmodel.PromptTemplate{Key: "json_defaults", Title: "JSON", Content: "content"}
	if err := db.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	var loadedTemplate persistmodel.PromptTemplate
	if err := db.First(&loadedTemplate, "key = ?", template.Key).Error; err != nil {
		t.Fatal(err)
	}
	assertJSONKind(t, loadedTemplate.Variables, map[string]any{})

	toolLog := persistmodel.ToolCallLog{MeetingID: &meeting.ID, ToolName: "json.tool"}
	if err := db.Create(&toolLog).Error; err != nil {
		t.Fatal(err)
	}
	var loadedToolLog persistmodel.ToolCallLog
	if err := db.First(&loadedToolLog, toolLog.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertJSONKind(t, loadedToolLog.Arguments, map[string]any{})
	assertJSONKind(t, loadedToolLog.ResultPreview, map[string]any{})

	wake := persistmodel.WakePlan{MeetingID: &meeting.ID, TriggerType: domainkernel.WakeTime, Reason: "json"}
	if err := db.Create(&wake).Error; err != nil {
		t.Fatal(err)
	}
	var loadedWake persistmodel.WakePlan
	if err := db.First(&loadedWake, wake.ID).Error; err != nil {
		t.Fatal(err)
	}
	assertJSONKind(t, loadedWake.TriggerConfig, map[string]any{})
}

func TestAutoMigrateCreatesForeignKeyConstraints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"telegram_messages", "meeting_events", "meeting_references", "wake_plans", "paper_orders", "paper_positions", "paper_fills", "paper_equity_snapshots"} {
		var sql string
		if err := db.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&sql).Error; err != nil {
			t.Fatalf("%s DDL lookup failed: %v", table, err)
		}
		if !strings.Contains(strings.ToUpper(sql), "FOREIGN KEY") {
			t.Fatalf("%s expected foreign key constraint in DDL: %s", table, sql)
		}
	}
}

func assertGORMTagContains(t *testing.T, model any, fieldName string, want string) {
	t.Helper()
	field, ok := reflect.TypeOf(model).FieldByName(fieldName)
	if !ok {
		t.Fatalf("%T missing field %s", model, fieldName)
	}
	tag := string(field.Tag.Get("gorm"))
	if !strings.Contains(tag, want) {
		t.Fatalf("%T.%s gorm tag %q must contain %q", model, fieldName, tag, want)
	}
}

func assertJSONKind(t *testing.T, raw []byte, want any) {
	t.Helper()
	var got any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("invalid JSON default %q: %v", string(raw), err)
	}
	switch want.(type) {
	case []any:
		if _, ok := got.([]any); !ok {
			t.Fatalf("expected JSON array default, got %T %v", got, got)
		}
	case map[string]any:
		if _, ok := got.(map[string]any); !ok {
			t.Fatalf("expected JSON object default, got %T %v", got, got)
		}
	default:
		t.Fatalf("unsupported JSON kind %T", want)
	}
}

func diffStrings(left []string, right []string) []string {
	rightSet := make(map[string]bool, len(right))
	for _, value := range right {
		rightSet[value] = true
	}
	var diff []string
	for _, value := range left {
		if !rightSet[value] {
			diff = append(diff, value)
		}
	}
	return diff
}

func TestAutoMigrateCreatesCompositeUniqueConstraints(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	expectUniqueViolation := func(label string, first any, second any) {
		t.Helper()
		if err := db.Create(first).Error; err != nil {
			t.Fatalf("%s first insert failed: %v", label, err)
		}
		if err := db.Create(second).Error; err == nil {
			t.Fatalf("%s expected duplicate insert to fail", label)
		}
	}
	expectUniqueViolation("secret kind/name",
		&persistmodel.Secret{Kind: domainkernel.SecretKindApp, Name: "runtime", EncryptedValue: "a"},
		&persistmodel.Secret{Kind: domainkernel.SecretKindApp, Name: "runtime", EncryptedValue: "b"},
	)
	expectUniqueViolation("provider model",
		&persistmodel.AiProviderModel{ProviderID: 1, ModelID: "m", DisplayName: "m", Raw: []byte(`{}`)},
		&persistmodel.AiProviderModel{ProviderID: 1, ModelID: "m", DisplayName: "m2", Raw: []byte(`{}`)},
	)
	expectUniqueViolation("telegram channel/message",
		&persistmodel.TelegramMessage{ChannelID: 1, MessageID: 10, Text: "a", RelatedSymbols: []byte(`[]`)},
		&persistmodel.TelegramMessage{ChannelID: 1, MessageID: 10, Text: "b", RelatedSymbols: []byte(`[]`)},
	)
	expectUniqueViolation("paper position account/code",
		&persistmodel.PaperPosition{AccountID: 1, Code: "600519", Quantity: 100, AvgCost: decimal.NewFromInt(1)},
		&persistmodel.PaperPosition{AccountID: 1, Code: "600519", Quantity: 200, AvgCost: decimal.NewFromInt(2)},
	)
	if err := db.Create(&persistmodel.PaperPosition{AccountID: 2, Code: "600519", Quantity: 100}).Error; err != nil && !errors.Is(err, gorm.ErrDuplicatedKey) {
		t.Fatalf("same code in another account should be allowed: %v", err)
	}
}
