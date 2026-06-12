package dashboard

import (
	"context"
	"testing"
	"time"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmsg "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/messaging"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/connect"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMessageSubscriptionStatusRequiresDecryptableSecrets(t *testing.T) {
	db := newDashboardTestDB(t)
	oldSecret := security.New(config.Settings{AppSecretKey: "old-secret"})
	for _, item := range []struct {
		name  string
		value string
	}{
		{name: "telegram:app_id", value: "12345"},
		{name: "telegram:app_hash", value: "hash"},
		{name: "telegram:mtproto_session", value: "session"},
	} {
		encrypted, err := oldSecret.EncryptSecret(item.value)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&persistmodel.Secret{
			Kind:           domainkernel.SecretKindMessageSubscription,
			Name:           item.name,
			EncryptedValue: encrypted,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&persistmodel.MessageSubscription{
		Provider:  "telegram_channel",
		Title:     "News",
		SourceRef: "@news",
		Enabled:   true,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&persistmodel.AppSetting{
		Key:       "system_heartbeat_message_subscription_listener",
		Value:     datatypes.JSON(domainkernel.NewJSON(map[string]any{"service": "message_subscription_listener", "mode": "redis", "status": "running", "timestamp": time.Now(), "pid": 1})),
		UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	payload, err := NewLoader(db, config.Settings{AppSecretKey: "current-secret", MeetingDispatchMode: "redis"}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	systemStatus := payload["systemStatus"].(map[string]any)
	listener := systemStatus["messageSubscriptionListener"].(map[string]any)
	if listener["status"] != "disabled" {
		t.Fatalf("undecryptable MTProto secrets should disable listener status, got %+v", listener)
	}
	diagnostics := payload["dependencyDiagnostics"].(map[string]any)
	messaging := diagnostics["messaging"].(map[string]any)
	provider := messaging["provider"].(map[string]any)
	mtproto := provider["mtproto"].(map[string]any)
	if mtproto["hasAppId"] != false || mtproto["hasAppHash"] != false || mtproto["hasSession"] != false {
		t.Fatalf("undecryptable MTProto diagnostics should be false, got %+v", mtproto)
	}
}

func TestProviderDiagnosticsExposeFreshnessAndMessageReliability(t *testing.T) {
	db := newDashboardTestDB(t)
	now := time.Now()
	if err := db.Create(&persistmodel.RealtimeQuote{Code: "600519", QuoteTime: now.Add(-10 * time.Minute), Price: decimal.NewFromInt(100), Volume: decimal.NewFromInt(1000), Provider: "fixture"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&persistmodel.DailyBar{Code: "600519", TradeDate: now.AddDate(0, 0, -1), Close: decimal.NewFromInt(99), Provider: "fixture"}).Error; err != nil {
		t.Fatal(err)
	}
	sub := persistmodel.MessageSubscription{Provider: domainmsg.ProviderRSSFeed, Title: "Feed", SourceRef: "https://example.test/feed.xml", Enabled: true, NextCollectAt: &now}
	if err := db.Create(&sub).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&persistmodel.IngestedMessage{SubscriptionID: sub.ID, Provider: sub.Provider, SourceMessageID: "1", MessageTime: now, Text: "failed", FilterStatus: domainmsg.FilterStatusFailed}).Error; err != nil {
		t.Fatal(err)
	}
	helpful := domainmsg.FeedbackHelpful
	if err := db.Create(&persistmodel.IngestedMessage{SubscriptionID: sub.ID, Provider: sub.Provider, SourceMessageID: "2", MessageTime: now, Text: "new", FilterStatus: domainmsg.FilterStatusUnfiltered, FeedbackLabel: &helpful}).Error; err != nil {
		t.Fatal(err)
	}

	payload, err := NewLoader(db, config.Settings{MeetingDispatchMode: "local"}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	diagnostics := payload["dependencyDiagnostics"].(map[string]any)
	market := diagnostics["market"].(map[string]any)
	provider := market["provider"].(map[string]any)
	if provider["freshnessStatus"] != "fresh" || provider["lastQuoteCode"] != "600519" {
		t.Fatalf("market freshness diagnostics mismatch: %+v", provider)
	}
	messaging := diagnostics["messaging"].(map[string]any)
	msgProvider := messaging["provider"].(map[string]any)
	subscriptions := msgProvider["subscriptions"].(map[string]any)
	if subscriptions["failedFilterMessages"] != int64(1) || subscriptions["unfilteredMessages"] != int64(1) || subscriptions["due"] != int64(1) {
		t.Fatalf("message reliability diagnostics mismatch: %+v", subscriptions)
	}
	if _, ok := subscriptions["dedupePolicy"].(map[string]any); !ok {
		t.Fatalf("missing dedupe policy: %+v", subscriptions)
	}
	if subscriptions["sourceTrustScoring"] != "manual_feedback_enabled" {
		t.Fatalf("source trust scoring not enabled: %+v", subscriptions)
	}
	scores := subscriptions["sourceTrustScores"].([]any)
	if len(scores) != 1 {
		t.Fatalf("expected one source trust score, got %+v", scores)
	}
	score := scores[0].(map[string]any)
	if score["trustScore"] != 100 || score["feedbackCount"] != 1 {
		t.Fatalf("source trust score mismatch: %+v", score)
	}
}

func TestAIUsageDiagnosticsSummarizesMeetingEventUsage(t *testing.T) {
	db := newDashboardTestDB(t)
	now := time.Now()
	provider := persistmodel.AiProvider{Name: "OpenAI", BaseURL: "https://api.example.test", DefaultModel: "gpt-test", Enabled: true}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	for _, model := range []persistmodel.AiProviderModel{
		{ProviderID: provider.ID, ModelID: "gpt-test", DisplayName: "GPT Test", Enabled: true, CreatedAt: now, UpdatedAt: now},
		{ProviderID: provider.ID, ModelID: "gpt-missing", DisplayName: "GPT Missing", Enabled: true, CreatedAt: now, UpdatedAt: now},
	} {
		if err := db.Create(&model).Error; err != nil {
			t.Fatal(err)
		}
	}
	account := persistmodel.PaperAccount{Name: "Default", InitialCash: decimal.NewFromInt(100000), Cash: decimal.NewFromInt(100000), Active: true}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	accountID := account.ID
	team := persistmodel.ResearchTeam{Name: "Default Team", PaperAccountID: &accountID, Active: true}
	if err := db.Create(&team).Error; err != nil {
		t.Fatal(err)
	}
	meeting := persistmodel.Meeting{ResearchTeamID: team.ID, Topic: "AI usage", Status: domainkernel.MeetingCompleted, CreatedAt: now}
	if err := db.Create(&meeting).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&persistmodel.AppSetting{
		Key: domainsettings.SettingAICostRates,
		Value: datatypes.JSON(domainkernel.NewJSON(map[string]any{
			"value": map[string]any{
				"currency": "USD",
				"rates": []map[string]any{{
					"providerId":       provider.ID,
					"model":            "gpt-test",
					"inputPerMillion":  1.0,
					"outputPerMillion": 2.0,
				}},
			},
		})),
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&persistmodel.AppSetting{
		Key:       domainsettings.SettingAIDailyCostBudget,
		Value:     datatypes.JSON(domainkernel.NewJSON(map[string]any{"value": 0.00001})),
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	rows := []persistmodel.MeetingEvent{
		{
			MeetingID: meeting.ID,
			Sequence:  1,
			Type:      domainkernel.EventToolCall,
			Payload: datatypes.JSON(domainkernel.NewJSON(map[string]any{
				"status":      "model_call_completed",
				"provider_id": provider.ID,
				"model":       "gpt-test",
				"token_usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
				"latency_ms":  1200,
			})),
			CreatedAt: now,
		},
		{
			MeetingID: meeting.ID,
			Sequence:  2,
			Type:      domainkernel.EventRoleMessage,
			Payload: datatypes.JSON(domainkernel.NewJSON(map[string]any{
				"status":      "role_completed",
				"provider_id": provider.ID,
				"model":       "gpt-test",
				"token_usage": map[string]any{"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30},
				"latency_ms":  3000,
			})),
			CreatedAt: now.Add(-48 * time.Hour),
		},
	}
	for _, row := range rows {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}

	payload, err := NewLoader(db, config.Settings{MeetingDispatchMode: "local"}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	metrics := payload["businessMetrics"].(map[string]any)
	if metrics["aiModelCalls24h"] != int64(1) || metrics["aiTotalTokens24h"] != int64(15) || metrics["aiTotalTokensTotal"] != int64(45) {
		t.Fatalf("AI usage business metrics mismatch: %+v", metrics)
	}
	if metrics["aiCostAmount24h"] != 0.00002 || metrics["aiCostBudgetStatus"] != "exceeded" {
		t.Fatalf("AI cost business metrics mismatch: %+v", metrics)
	}
	diagnostics := payload["dependencyDiagnostics"].(map[string]any)
	ai := diagnostics["ai"].(map[string]any)
	usage := ai["usage"].(map[string]any)
	if usage["status"] != "observed" || usage["modelCallsTotal"] != int64(2) {
		t.Fatalf("AI usage diagnostics mismatch: %+v", usage)
	}
	if usage["costCurrency"] != "USD" || usage["costAmountSource"] != "configured_rates" {
		t.Fatalf("AI cost source mismatch: %+v", usage)
	}
	if usage["costBudgetStatus"] != "exceeded" || usage["costBudgetAmount"] != 0.00001 {
		t.Fatalf("AI cost budget mismatch: %+v", usage)
	}
	if usage["costAmount24h"] != 0.00002 || usage["costAmountTotal"] != 0.00006 {
		t.Fatalf("AI estimated cost mismatch: %+v", usage)
	}
	coverage := usage["costRateCoverage"].(map[string]any)
	if coverage["status"] != "warning" || coverage["modelCount"] != 2 || coverage["coveredModelCount"] != 1 || coverage["missingModelCount"] != 1 {
		t.Fatalf("AI cost rate coverage mismatch: %+v", coverage)
	}
	missingModels := coverage["missingModels"].([]any)
	if len(missingModels) != 1 || missingModels[0].(map[string]any)["model"] != "gpt-missing" {
		t.Fatalf("AI cost rate missing models mismatch: %+v", coverage)
	}
	if usage["latencyObservedCallsTotal"] != int64(2) || usage["latencyObservedCalls24h"] != int64(1) {
		t.Fatalf("AI latency observed call counts mismatch: %+v", usage)
	}
	if usage["latencyAvgMsTotal"] != 2100.0 || usage["latencyAvgMs24h"] != 1200.0 || usage["latencyP95Ms24h"] != 1200.0 {
		t.Fatalf("AI latency aggregates mismatch: %+v", usage)
	}
	groups := usage["byProviderModel"].([]any)
	if len(groups) != 1 {
		t.Fatalf("expected one provider/model usage group, got %+v", groups)
	}
	group := groups[0].(map[string]any)
	if group["providerName"] != "OpenAI" || group["totalTokensTotal"] != int64(45) {
		t.Fatalf("provider/model usage group mismatch: %+v", group)
	}
	if group["costAmount24h"] != 0.00002 || group["costEstimatedCallsTotal"] != int64(2) || group["costMissingCallsTotal"] != int64(0) {
		t.Fatalf("provider/model cost group mismatch: %+v", group)
	}
	if group["latencyAvgMsTotal"] != 2100.0 || group["latencyP95Ms24h"] != 1200.0 {
		t.Fatalf("provider/model latency group mismatch: %+v", group)
	}
}

func TestMeetingRuntimeDiagnosticsSummarizesDurationsAndEvents(t *testing.T) {
	db := newDashboardTestDB(t)
	now := time.Now()
	account := persistmodel.PaperAccount{Name: "Default", InitialCash: decimal.NewFromInt(100000), Cash: decimal.NewFromInt(100000), Active: true}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	accountID := account.ID
	team := persistmodel.ResearchTeam{Name: "Default Team", PaperAccountID: &accountID, Active: true}
	if err := db.Create(&team).Error; err != nil {
		t.Fatal(err)
	}
	startedAt := now.Add(2 * time.Minute)
	completedAt := now.Add(7 * time.Minute)
	meeting := persistmodel.Meeting{
		ResearchTeamID: team.ID,
		Topic:          "Runtime diagnostics",
		Status:         domainkernel.MeetingCompleted,
		CreatedAt:      now,
		StartedAt:      &startedAt,
		CompletedAt:    &completedAt,
	}
	if err := db.Create(&meeting).Error; err != nil {
		t.Fatal(err)
	}
	events := []persistmodel.MeetingEvent{
		{MeetingID: meeting.ID, Sequence: 1, Type: domainkernel.EventToolCall, CreatedAt: startedAt.Add(time.Minute)},
		{MeetingID: meeting.ID, Sequence: 2, Type: domainkernel.EventConclusion, CreatedAt: completedAt},
	}
	for _, event := range events {
		if err := db.Create(&event).Error; err != nil {
			t.Fatal(err)
		}
	}

	payload, err := NewLoader(db, config.Settings{MeetingDispatchMode: "local"}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	metrics := payload["businessMetrics"].(map[string]any)
	if metrics["meetingRunAvgSeconds24h"] != float64(300) || metrics["meetingQueueWaitAvgSeconds24h"] != float64(120) {
		t.Fatalf("meeting runtime business metrics mismatch: %+v", metrics)
	}
	diagnostics := payload["dependencyDiagnostics"].(map[string]any)
	meetingDiagnostics := diagnostics["meeting"].(map[string]any)
	runtime := meetingDiagnostics["runtime"].(map[string]any)
	if runtime["status"] != "ok" || runtime["completedCount24h"] != int64(1) || runtime["runP95Seconds"] != float64(300) {
		t.Fatalf("meeting runtime diagnostics mismatch: %+v", runtime)
	}
	eventCounts := runtime["eventCounts24h"].([]any)
	if len(eventCounts) != 2 {
		t.Fatalf("meeting event counts mismatch: %+v", eventCounts)
	}
}

func TestDashboardWakeAlertIdentifiesOverduePlans(t *testing.T) {
	db := newDashboardTestDB(t)
	now := time.Now()
	account := persistmodel.PaperAccount{Name: "Default", InitialCash: decimal.NewFromInt(100000), Cash: decimal.NewFromInt(100000), Active: true}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	accountID := account.ID
	team := persistmodel.ResearchTeam{Name: "Wake Team", PaperAccountID: &accountID, Active: true}
	if err := db.Create(&team).Error; err != nil {
		t.Fatal(err)
	}
	overdueAt := now.Add(-45 * time.Minute)
	futureAt := now.Add(45 * time.Minute)
	overdue := persistmodel.WakePlan{
		ResearchTeamID: team.ID,
		TriggerType:    domainkernel.WakeTime,
		TriggerConfig:  datatypes.JSON(domainkernel.NewJSON(map[string]any{"topic": "overdue follow-up"})),
		Reason:         "check overdue plan details",
		Status:         domainkernel.WakeActive,
		NextCheckAt:    &overdueAt,
	}
	future := persistmodel.WakePlan{
		ResearchTeamID: team.ID,
		TriggerType:    domainkernel.WakeTime,
		TriggerConfig:  datatypes.JSON(domainkernel.NewJSON(map[string]any{"topic": "future follow-up"})),
		Reason:         "future plan",
		Status:         domainkernel.WakeActive,
		NextCheckAt:    &futureAt,
	}
	if err := db.Create(&overdue).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&future).Error; err != nil {
		t.Fatal(err)
	}

	payload, err := NewLoader(db, config.Settings{MeetingDispatchMode: "local"}).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	metrics := payload["businessMetrics"].(map[string]any)
	if metrics["wakeOverdueCount"] != int64(1) {
		t.Fatalf("wake overdue count mismatch: %+v", metrics)
	}
	alerts := payload["alerts"].([]any)
	var wakeAlert map[string]any
	for _, raw := range alerts {
		alert := raw.(map[string]any)
		if alert["title"] == "Overdue wake plans" {
			wakeAlert = alert
			break
		}
	}
	if wakeAlert == nil {
		t.Fatalf("missing overdue wake alert: %+v", alerts)
	}
	if wakeAlert["link"] != "/wake?overdue=true" || wakeAlert["totalCount"] != int64(1) {
		t.Fatalf("wake alert should point to overdue plan focus view: %+v", wakeAlert)
	}
	summaries := wakeAlert["wakePlanSummaries"].([]any)
	if len(summaries) != 1 {
		t.Fatalf("wake alert summaries should include only overdue active plans: %+v", wakeAlert)
	}
	summary := summaries[0].(map[string]any)
	if summary["id"] != overdue.ID || summary["researchTeamName"] != "Wake Team" || summary["reason"] != "check overdue plan details" {
		t.Fatalf("wake alert summary should identify the overdue plan: %+v", summary)
	}
}

func newDashboardTestDB(t *testing.T) *gorm.DB {
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
