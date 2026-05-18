package meeting

import (
	"context"
	"encoding/json"
	"fmt"
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/market"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
	domaintelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/telegram"
	domainwake "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/wake"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/connect"
	infrapaper "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/paper"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCloneMeetingContextAndReferences(t *testing.T) {
	db := newMeetingTestDB(t)
	source, err := CreateMeeting(db, "source topic", "manual")
	mustMeeting(t, err)
	target, err := CreateMeeting(db, "target topic", "manual")
	mustMeeting(t, err)
	_, err = AppendEvent(db, source.ID, domainkernel.EventSystem, nil, "context", map[string]any{"status": "meeting_context", "foo": "bar"})
	mustMeeting(t, err)
	note := "note"
	ref := domainmeeting.Reference{SourceMeetingID: source.ID, TargetMeetingID: &target.ID, ReferenceType: "meeting", Note: &note, TargetTopicSnapshot: target.Topic}
	mustMeeting(t, db.Create(&ref).Error)

	result, err := CloneMeetingContextAndReferences(db, source.ID, target.ID)
	mustMeeting(t, err)
	if result["events"] != 1 || result["references"] != 1 {
		t.Fatalf("clone result mismatch: %+v", result)
	}
	var copiedEvents int64
	db.Model(&domainmeeting.Event{}).Where("meeting_id = ? AND content = ?", target.ID, "context").Count(&copiedEvents)
	if copiedEvents != 1 {
		t.Fatalf("expected copied context event, got %d", copiedEvents)
	}
	var copiedRefs int64
	db.Model(&domainmeeting.Reference{}).Where("source_meeting_id = ? AND note = ?", target.ID, note).Count(&copiedRefs)
	if copiedRefs != 1 {
		t.Fatalf("expected copied reference, got %d", copiedRefs)
	}
}

func TestRecapMeetingSummarizesEvents(t *testing.T) {
	db := newMeetingTestDB(t)
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)
	role := "analyst"
	_, err = AppendEvent(db, meeting.ID, domainkernel.EventRoleMessage, &role, "analysis", map[string]any{"status": "role_completed"})
	mustMeeting(t, err)
	_, err = AppendEvent(db, meeting.ID, domainkernel.EventConclusion, nil, "final conclusion", nil)
	mustMeeting(t, err)

	mustMeeting(t, RecapMeeting(db, meeting, "admin"))
	if meeting.RecapStatus == nil || *meeting.RecapStatus != "completed" || meeting.Summary == nil || meeting.Conclusion == nil {
		t.Fatalf("recap fields missing: %+v", meeting)
	}
	if *meeting.Conclusion != "final conclusion" {
		t.Fatalf("conclusion mismatch: %s", *meeting.Conclusion)
	}
}

func TestProcessDueWakePlansDispatchesCreatedMeeting(t *testing.T) {
	db := newMeetingTestDB(t)
	due := time.Now().Add(-time.Minute)
	plan := domainwake.Plan{TriggerType: domainkernel.WakeTime, TriggerConfig: JSON(map[string]any{"topic": "wake topic"}), Reason: "reason", Status: domainkernel.WakeActive, NextCheckAt: &due}
	mustMeeting(t, db.Create(&plan).Error)
	dispatched := 0
	stats, err := ProcessDueWakePlansWithDispatcher(db, 10, func(meeting *domainmeeting.Meeting) error {
		dispatched++
		if meeting.TriggerSource != "wake_plan" {
			t.Fatalf("trigger source mismatch: %s", meeting.TriggerSource)
		}
		return nil
	})
	mustMeeting(t, err)
	if stats["checked"] != 1 || stats["fired"] != 1 || stats["dispatched"] != 1 || dispatched != 1 {
		t.Fatalf("wake stats mismatch: stats=%+v dispatched=%d", stats, dispatched)
	}
}

func TestProcessDueWakePlansCancelsInvalidIndicatorPlan(t *testing.T) {
	db := newMeetingTestDB(t)
	due := time.Now().Add(-time.Minute)
	plan := domainwake.Plan{TriggerType: domainkernel.WakeIndicator, TriggerConfig: JSON(map[string]any{"topic": "missing condition"}), Reason: "invalid", Status: domainkernel.WakeActive, NextCheckAt: &due}
	mustMeeting(t, db.Create(&plan).Error)

	stats, err := ProcessDueWakePlansWithDispatcher(db, 10, nil)
	mustMeeting(t, err)
	if stats["checked"] != 1 || stats["fired"] != 0 || stats["failed"] != 0 {
		t.Fatalf("invalid wake stats mismatch: %+v", stats)
	}
	var cancelled domainwake.Plan
	mustMeeting(t, db.First(&cancelled, plan.ID).Error)
	if cancelled.Status != domainkernel.WakeCancelled || cancelled.ResultSummary == nil || !strings.Contains(*cancelled.ResultSummary, "Invalid wake plan cancelled") {
		t.Fatalf("expected invalid plan cancellation, got %+v", cancelled)
	}
}

func TestFireWakePlanUsesSourceMeetingWhenTopicMissing(t *testing.T) {
	db := newMeetingTestDB(t)
	source, err := CreateMeeting(db, "source topic", "manual")
	mustMeeting(t, err)
	plan := domainwake.Plan{MeetingID: &source.ID, TriggerType: domainkernel.WakeTime, TriggerConfig: JSON(map[string]any{}), Reason: "reason", Status: domainkernel.WakeActive}
	mustMeeting(t, db.Create(&plan).Error)

	meeting, err := FireWakePlan(db, &plan)
	mustMeeting(t, err)
	if meeting.Topic != "Wake follow-up: source topic" {
		t.Fatalf("unexpected wake topic: %s", meeting.Topic)
	}
	var event domainmeeting.Event
	mustMeeting(t, db.First(&event, "meeting_id = ? AND type = ? AND content LIKE ?", meeting.ID, domainkernel.EventSystem, "Wake plan #%").Error)
	if !strings.Contains(event.Content, "Source meeting: source topic") || !strings.Contains(string(event.Payload), "source_topic") {
		t.Fatalf("unexpected wake event: %+v payload=%s", event, event.Payload)
	}
}

func TestApplyMeetingRecapActionsRejectsInvalidWakePlan(t *testing.T) {
	db := newMeetingTestDB(t)
	meeting, err := CreateMeeting(db, "source topic", "manual")
	mustMeeting(t, err)
	roleKey := "moderator"
	recap := map[string]any{
		"wake_plans": []map[string]any{
			{"trigger_type": "indicator", "reason": "missing condition", "trigger_config": map[string]any{"topic": "invalid"}},
		},
	}

	mustMeeting(t, ApplyMeetingRecapActions(db, meeting, nil, roleKey, recap))
	var count int64
	db.Model(&domainwake.Plan{}).Count(&count)
	if count != 0 {
		t.Fatalf("invalid recap wake plan should not be created, got %d", count)
	}
	var event domainmeeting.Event
	mustMeeting(t, db.First(&event, "meeting_id = ? AND type = ? AND role_key = ?", meeting.ID, domainkernel.EventError, roleKey).Error)
	if !strings.Contains(event.Content, "Failed to create wake plan") || !strings.Contains(event.Content, "requires code") {
		t.Fatalf("unexpected wake plan error event: %+v", event)
	}
}

func TestProcessDueWakePlansFiresIndicatorTrigger(t *testing.T) {
	db := newMeetingTestDB(t)
	due := time.Now().Add(-time.Minute)
	plan := domainwake.Plan{TriggerType: domainkernel.WakeIndicator, TriggerConfig: JSON(map[string]any{"code": "600519", "field": "price", "operator": ">=", "threshold": "100", "topic": "indicator hit"}), Reason: "price threshold", Status: domainkernel.WakeActive, NextCheckAt: &due}
	mustMeeting(t, db.Create(&plan).Error)
	mustMeeting(t, db.Create(&domainmarket.RealtimeQuote{Code: "600519", QuoteTime: time.Now(), Price: decimal.NewFromInt(101), ChangePct: decimal.NewFromInt(1), Provider: "test"}).Error)
	stats, err := ProcessDueWakePlansWithDispatcher(db, 10, nil)
	mustMeeting(t, err)
	if stats["checked"] != 1 || stats["fired"] != 1 {
		t.Fatalf("indicator stats mismatch: %+v", stats)
	}
	var fired domainwake.Plan
	mustMeeting(t, db.First(&fired, plan.ID).Error)
	if fired.Status != domainkernel.WakeFired || fired.ResultSummary == nil || !strings.Contains(*fired.ResultSummary, "satisfied") {
		t.Fatalf("indicator plan mismatch: %+v", fired)
	}
	var meeting domainmeeting.Meeting
	mustMeeting(t, db.First(&meeting, "topic = ?", "indicator hit").Error)
}

func TestProcessDueWakePlansIndicatorUsesNormalizedCodeAndDailyFallback(t *testing.T) {
	db := newMeetingTestDB(t)
	due := time.Now().Add(-time.Minute)
	plan := domainwake.Plan{TriggerType: domainkernel.WakeIndicator, TriggerConfig: JSON(map[string]any{"symbol": "SH600519", "field": "close", "operator": "gte", "target_price": "100"}), Reason: "daily close", Status: domainkernel.WakeActive, NextCheckAt: &due}
	mustMeeting(t, db.Create(&plan).Error)
	mustMeeting(t, db.Create(&domainmarket.DailyBar{Code: "600519", TradeDate: time.Now(), Close: decimal.NewFromInt(101), Provider: "test"}).Error)

	stats, err := ProcessDueWakePlansWithDispatcher(db, 10, nil)
	mustMeeting(t, err)
	if stats["checked"] != 1 || stats["fired"] != 1 {
		t.Fatalf("indicator daily fallback stats mismatch: %+v", stats)
	}
}

func TestProcessDueWakePlansReschedulesUnmetIndicatorTrigger(t *testing.T) {
	db := newMeetingTestDB(t)
	due := time.Now().Add(-time.Minute)
	plan := domainwake.Plan{TriggerType: domainkernel.WakeIndicator, TriggerConfig: JSON(map[string]any{"code": "600519", "threshold": "999999", "interval_seconds": 60}), Reason: "price threshold", Status: domainkernel.WakeActive, NextCheckAt: &due}
	mustMeeting(t, db.Create(&plan).Error)
	mustMeeting(t, db.Create(&domainmarket.RealtimeQuote{Code: "600519", QuoteTime: time.Now(), Price: decimal.NewFromInt(99), Provider: "test"}).Error)
	stats, err := ProcessDueWakePlansWithDispatcher(db, 10, nil)
	mustMeeting(t, err)
	if stats["checked"] != 1 || stats["fired"] != 0 {
		t.Fatalf("indicator unmet stats mismatch: %+v", stats)
	}
	var active domainwake.Plan
	mustMeeting(t, db.First(&active, plan.ID).Error)
	if active.Status != domainkernel.WakeActive || active.NextCheckAt == nil || !active.NextCheckAt.After(time.Now()) {
		t.Fatalf("indicator reschedule mismatch: %+v", active)
	}
}

func TestProcessDueWakePlansFiresEventTrigger(t *testing.T) {
	db := newMeetingTestDB(t)
	due := time.Now().Add(-time.Minute)
	decision := domainkernel.NewsMeeting
	plan := domainwake.Plan{TriggerType: domainkernel.WakeEvent, TriggerConfig: JSON(map[string]any{"keywords": []string{"earnings"}, "related_symbols": []string{"600519"}, "topic": "event hit"}), Reason: "event trigger", Status: domainkernel.WakeActive, NextCheckAt: &due}
	mustMeeting(t, db.Create(&plan).Error)
	mustMeeting(t, db.Create(&domaintelegram.Message{ChannelID: 1, MessageID: 99, MessageTime: time.Now(), Text: "600519 earnings update", FilterDecision: &decision, RelatedSymbols: JSON([]string{"600519"})}).Error)
	stats, err := ProcessDueWakePlansWithDispatcher(db, 10, nil)
	mustMeeting(t, err)
	if stats["checked"] != 1 || stats["fired"] != 1 {
		t.Fatalf("event stats mismatch: %+v", stats)
	}
	var meeting domainmeeting.Meeting
	mustMeeting(t, db.First(&meeting, "topic = ?", "event hit").Error)
}

func TestProcessDueWakePlansIgnoresOldEventTrigger(t *testing.T) {
	db := newMeetingTestDB(t)
	decision := domainkernel.NewsMeeting
	oldTime := time.Now().Add(-time.Hour)
	mustMeeting(t, db.Create(&domaintelegram.Message{ChannelID: 1, MessageID: 98, MessageTime: oldTime, Text: "600519 old material event", FilterDecision: &decision, RelatedSymbols: JSON([]string{"600519"})}).Error)
	due := time.Now().Add(-time.Minute)
	plan := domainwake.Plan{TriggerType: domainkernel.WakeEvent, TriggerConfig: JSON(map[string]any{"keywords": []string{"earnings"}, "related_symbols": []string{"600519"}, "topic": "event hit"}), Reason: "event trigger", Status: domainkernel.WakeActive, NextCheckAt: &due}
	mustMeeting(t, db.Create(&plan).Error)

	stats, err := ProcessDueWakePlansWithDispatcher(db, 10, nil)
	mustMeeting(t, err)
	if stats["checked"] != 1 || stats["fired"] != 0 {
		t.Fatalf("old event stats mismatch: %+v", stats)
	}
	var active domainwake.Plan
	mustMeeting(t, db.First(&active, plan.ID).Error)
	if active.Status != domainkernel.WakeActive || active.ResultSummary == nil || !strings.Contains(*active.ResultSummary, "No matching event message") {
		t.Fatalf("old event reschedule mismatch: %+v", active)
	}
}

func TestProcessDueWakePlansSupportsCompoundIndicatorGrammar(t *testing.T) {
	db := newMeetingTestDB(t)
	due := time.Now().Add(-time.Minute)
	plan := domainwake.Plan{
		TriggerType: domainkernel.WakeIndicator,
		TriggerConfig: JSON(map[string]any{
			"logic": "all",
			"topic": "compound indicator hit",
			"conditions": []map[string]any{
				{"ticker": "600519", "metric": "last_price", "op": "gte", "target_value": "100"},
				{"symbol": "600519", "field": "change_rate", "operator": "lt", "threshold": "5"},
			},
		}),
		Reason: "compound indicator", Status: domainkernel.WakeActive, NextCheckAt: &due,
	}
	mustMeeting(t, db.Create(&plan).Error)
	mustMeeting(t, db.Create(&domainmarket.RealtimeQuote{Code: "600519", QuoteTime: time.Now(), Price: decimal.NewFromInt(101), ChangePct: decimal.NewFromInt(3), Provider: "test"}).Error)

	stats, err := ProcessDueWakePlansWithDispatcher(db, 10, nil)
	mustMeeting(t, err)
	if stats["checked"] != 1 || stats["fired"] != 1 {
		t.Fatalf("compound indicator stats mismatch: %+v", stats)
	}
	var meeting domainmeeting.Meeting
	mustMeeting(t, db.First(&meeting, "topic = ?", "compound indicator hit").Error)
}

func TestProcessDueWakePlansSupportsEventProductionGrammar(t *testing.T) {
	db := newMeetingTestDB(t)
	now := time.Now()
	channel := domaintelegram.Channel{Title: "news", ChannelRef: "@news", Enabled: true, CollectFrom: now.Add(-time.Hour)}
	mustMeeting(t, db.Create(&channel).Error)
	decision := domainkernel.NewsMeeting
	mustMeeting(t, db.Create(&domaintelegram.Message{ChannelID: channel.ID, MessageID: 101, MessageTime: now, Text: "600519 earnings beat guidance", FilterDecision: &decision, RelatedSymbols: JSON([]string{"600519"})}).Error)
	due := now.Add(-time.Minute)
	plan := domainwake.Plan{
		CreatedAt:   now.Add(-2 * time.Minute),
		TriggerType: domainkernel.WakeEvent,
		TriggerConfig: JSON(map[string]any{
			"channel_ids":      []uint{channel.ID},
			"decisions":        []string{"meeting", "observe"},
			"keywords":         []string{"earnings", "guidance"},
			"keyword_mode":     "all",
			"exclude_keywords": []string{"rumor"},
			"regex":            "earnings\\s+beat",
			"symbols":          []string{"600519"},
			"symbol_mode":      "all",
			"topic":            "production event hit",
		}),
		Reason: "event grammar", Status: domainkernel.WakeActive, NextCheckAt: &due,
	}
	mustMeeting(t, db.Create(&plan).Error)

	stats, err := ProcessDueWakePlansWithDispatcher(db, 10, nil)
	mustMeeting(t, err)
	if stats["checked"] != 1 || stats["fired"] != 1 {
		t.Fatalf("event grammar stats mismatch: %+v", stats)
	}
	var meeting domainmeeting.Meeting
	mustMeeting(t, db.First(&meeting, "topic = ?", "production event hit").Error)
}

func TestRunWakePlanLoopProcessesUntilCancelled(t *testing.T) {
	db := newMeetingTestDB(t)
	due := time.Now().Add(-time.Minute)
	plan := domainwake.Plan{TriggerType: domainkernel.WakeTime, TriggerConfig: JSON(map[string]any{"topic": "loop wake"}), Reason: "loop", Status: domainkernel.WakeActive, NextCheckAt: &due}
	mustMeeting(t, db.Create(&plan).Error)
	ctx, cancel := context.WithCancel(context.Background())
	dispatched := make(chan uint, 1)
	done := make(chan struct{})
	go func() {
		RunWakePlanLoop(ctx, db, time.Hour, 10, func(meeting *domainmeeting.Meeting) error {
			dispatched <- meeting.ID
			return nil
		})
		close(done)
	}()
	select {
	case <-dispatched:
	case <-time.After(2 * time.Second):
		t.Fatal("wake loop did not process due plan")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("wake loop did not stop after cancellation")
	}
}

func TestExecuteRoleToolsBuildsMeetingContext(t *testing.T) {
	db := newMeetingTestDB(t)
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)
	_, err = AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, "context", map[string]any{"status": "meeting_context", "related_symbols": []string{"600519"}})
	mustMeeting(t, err)
	mustMeeting(t, db.Create(&domainmarket.RealtimeQuote{Code: "600519", QuoteTime: time.Now(), Price: decimal.NewFromInt(1688), ChangePct: decimal.NewFromInt(1), Provider: "test"}).Error)
	mustMeeting(t, db.Create(&domaintelegram.Channel{Title: "news", ChannelRef: "@news", Enabled: true, CollectFrom: time.Now()}).Error)
	mustMeeting(t, db.Create(&domaintelegram.Message{ChannelID: 1, MessageID: 10, MessageTime: time.Now(), Text: "600519 update", RelatedSymbols: JSON([]string{"600519"})}).Error)
	mustMeeting(t, db.Create(&domainpaper.Position{AccountID: 1, Code: "600519", Quantity: 100, AvgCost: decimal.NewFromInt(100)}).Error)
	role := domainai.AgentRole{Key: "analyst", ToolNames: JSON([]string{"market.realtime_quote", "telegram.recent_messages", "paper.positions"})}

	results := ExecuteRoleTools(db, *meeting, role)
	if len(results) != 3 {
		t.Fatalf("expected 3 tool results, got %+v", results)
	}
	if results[0].Name != "market.realtime_quote" || results[0].Arguments["code"] != "600519" || len(results[0].Rows) != 1 {
		t.Fatalf("unexpected quote result %+v", results[0])
	}
	if results[1].Name != "telegram.recent_messages" || len(results[1].Rows) != 1 {
		t.Fatalf("unexpected telegram result %+v", results[1])
	}
	if results[2].Name != "paper.positions" || len(results[2].Rows) != 1 {
		t.Fatalf("unexpected positions result %+v", results[2])
	}
	prompt := BuildRolePrompt(db, *meeting, role, results)
	if !strings.Contains(prompt, "market.realtime_quote") || !strings.Contains(prompt, "600519") {
		t.Fatalf("prompt missing tool context: %s", prompt)
	}
}

func TestExecuteRoleToolsSupportsWebSearchAndDeferredActionTools(t *testing.T) {
	oldSearchBase := webSearchBaseURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("q"), "600519") {
			t.Fatalf("expected related symbol in query, got %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><rss><channel>
<item><title>Guizhou Maotai news</title><link>https://example.com/a</link><pubDate>Sat, 09 May 2026 10:00:00 GMT</pubDate><source url="https://example.com">Example</source></item>
</channel></rss>`))
	}))
	defer srv.Close()
	webSearchBaseURL = srv.URL
	defer func() { webSearchBaseURL = oldSearchBase }()

	db := newMeetingTestDB(t)
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)
	_, err = AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, "context", map[string]any{"status": "meeting_context", "related_symbols": []string{"600519"}})
	mustMeeting(t, err)
	role := domainai.AgentRole{Key: "portfolio", ToolNames: JSON([]string{"web.search", "market.upsert_watchlist", "paper.create_order", "wake.create_plan"})}
	results := ExecuteRoleTools(db, *meeting, role)
	if len(results) != 4 {
		t.Fatalf("expected 4 tool results, got %+v", results)
	}
	if results[0].Name != "web.search" || results[0].Error != "" || len(results[0].Rows) != 1 || results[0].Rows[0]["title"] != "Guizhou Maotai news" {
		t.Fatalf("unexpected web search result %+v", results[0])
	}
	for _, result := range results[1:] {
		if result.Error != "" || len(result.Rows) != 1 || result.Rows[0]["status"] != "deferred_action_only" {
			t.Fatalf("expected deferred action result, got %+v", result)
		}
	}
}

func TestRunMeetingOnceRecordsToolResults(t *testing.T) {
	db := newMeetingTestDB(t)
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)
	_, err = AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, "context", map[string]any{"status": "meeting_context", "related_symbols": []string{"600519"}})
	mustMeeting(t, err)
	mustMeeting(t, db.Create(&domainmarket.RealtimeQuote{Code: "600519", QuoteTime: time.Now(), Price: decimal.NewFromInt(1688), Provider: "test"}).Error)
	role := domainai.AgentRole{Key: "analyst", Name: "Analyst", Enabled: true, SortOrder: 1, ToolNames: JSON([]string{"market.realtime_quote"})}
	mustMeeting(t, saveTestAgentRole(db, meeting.ResearchTeamID, &role))

	mustMeeting(t, RunMeetingOnce(db, meeting.ID))
	var toolEvents int64
	db.Model(&domainmeeting.Event{}).Where("meeting_id = ? AND type = ?", meeting.ID, domainkernel.EventToolResult).Count(&toolEvents)
	if toolEvents != 1 {
		t.Fatalf("expected one tool result event, got %d", toolEvents)
	}
	var logs []domainmeeting.ToolCallLog
	db.Where("meeting_id = ? AND tool_name = ?", meeting.ID, "market.realtime_quote").Find(&logs)
	if len(logs) != 1 {
		t.Fatalf("expected one tool call log, got %+v", logs)
	}
	var rows []map[string]any
	if err := json.Unmarshal(logs[0].ResultPreview, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["code"] != "600519" {
		t.Fatalf("unexpected tool preview %+v", rows)
	}
}

func TestApplyMeetingRecapActionsCreatesWatchlistWakeAndOrder(t *testing.T) {
	db := newMeetingTestDB(t)
	_, _, err := infrapaper.EnsureDefaultPaperSetup(db)
	mustMeeting(t, err)
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)
	role := "moderator"
	recapEvent, err := AppendEvent(db, meeting.ID, domainkernel.EventRoleMessage, &role, `{"summary":"track bank","conclusion":"pilot buy"}`, map[string]any{"related_symbols": []string{"000001"}})
	mustMeeting(t, err)

	recap := map[string]any{
		"summary":    "track bank",
		"conclusion": "pilot buy",
		"watchlist_actions": []map[string]any{
			{"code": "000001", "name": "Ping An Bank", "note": "watch valuation", "active": true},
		},
		"wake_plans": []map[string]any{
			{"trigger_type": "time", "next_check_at": "2026-05-10 09:30:00", "reason": "check open", "trigger_config": map[string]any{"topic": "follow up"}},
		},
		"orders": []map[string]any{
			{"code": "000001", "side": "buy", "quantity": 100, "suggested_price": 10, "reason": "pilot buy"},
		},
	}
	mustMeeting(t, ApplyMeetingRecapActions(db, meeting, recapEvent, role, recap))

	var item domainmarket.WatchlistItem
	mustMeeting(t, db.First(&item, "code = ?", "000001").Error)
	if !item.Active || item.Note == nil || *item.Note != "watch valuation" {
		t.Fatalf("watchlist mismatch: %+v", item)
	}
	var symbol domainmarket.Symbol
	mustMeeting(t, db.First(&symbol, "code = ?", "000001").Error)
	if symbol.Name != "Ping An Bank" || symbol.Exchange != "SZ" {
		t.Fatalf("symbol mismatch: %+v", symbol)
	}
	var plan domainwake.Plan
	mustMeeting(t, db.First(&plan, "meeting_id = ?", meeting.ID).Error)
	if plan.SourceMeetingEventID == nil || *plan.SourceMeetingEventID != recapEvent.ID || plan.SourceRoleKey == nil || *plan.SourceRoleKey != role {
		t.Fatalf("wake source mismatch: %+v", plan)
	}
	if plan.NextCheckAt == nil || plan.NextCheckAt.In(appTZ).Hour() != 9 || plan.NextCheckAt.In(appTZ).Minute() != 30 {
		t.Fatalf("wake time mismatch: %+v", plan.NextCheckAt)
	}
	var order domainpaper.Order
	mustMeeting(t, db.First(&order, "meeting_id = ?", meeting.ID).Error)
	if order.SourceMeetingEventID == nil || *order.SourceMeetingEventID != recapEvent.ID || order.Code != "000001" || order.Quantity != 100 {
		t.Fatalf("order mismatch: %+v", order)
	}
	if order.Status != domainkernel.OrderPending && order.Status != domainkernel.OrderSuggested {
		t.Fatalf("unexpected order status: %s", order.Status)
	}
}

func TestApplyMeetingRecapActionsAddsOrderSymbolsToWatchlist(t *testing.T) {
	db := newMeetingTestDB(t)
	_, _, err := infrapaper.EnsureDefaultPaperSetup(db)
	mustMeeting(t, err)
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)
	role := "moderator"
	recapEvent, err := AppendEvent(db, meeting.ID, domainkernel.EventRoleMessage, &role, "recap", map[string]any{"related_symbols": []string{"600519"}})
	mustMeeting(t, err)
	recap := map[string]any{
		"summary":    "Worth tracking before action.",
		"conclusion": "Observe 600519 and pilot 000001.",
		"orders": []map[string]any{
			{"code": "000001", "side": "buy", "quantity": 100, "suggested_price": 10, "reason": "pilot order"},
		},
	}
	mustMeeting(t, ApplyMeetingRecapActions(db, meeting, recapEvent, role, recap))

	var count int64
	db.Model(&domainmarket.WatchlistItem{}).Where("code IN ?", []string{"600519", "000001"}).Count(&count)
	if count != 2 {
		t.Fatalf("expected fallback/order watchlist rows, got %d", count)
	}
	var orderItem domainmarket.WatchlistItem
	mustMeeting(t, db.First(&orderItem, "code = ?", "000001").Error)
	if orderItem.Note == nil || !strings.Contains(*orderItem.Note, "Observe 600519") {
		t.Fatalf("order watchlist note mismatch: %+v", orderItem)
	}
}

func TestRunMeetingOnceAppliesModeratorRecapActions(t *testing.T) {
	db := newMeetingTestDB(t)
	_, _, err := infrapaper.EnsureDefaultPaperSetup(db)
	mustMeeting(t, err)
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)
	role := "moderator"
	recap := `{
		"topic":"final topic",
		"tags":["bank","pilot"],
		"summary":"summary from moderator",
		"conclusion":"conclusion from moderator",
		"watchlist_actions":[{"code":"000001","note":"track final","active":true}],
		"wake_plans":[{"trigger_type":"time","next_check_at":"2026-05-10 09:30:00","reason":"follow-up","trigger_config":{"topic":"next"}}],
		"orders":[{"code":"000001","side":"buy","quantity":100,"suggested_price":10,"reason":"pilot"}]
	}`
	_, err = AppendEvent(db, meeting.ID, domainkernel.EventRoleMessage, &role, "```json\n"+recap+"\n```", nil)
	mustMeeting(t, err)

	mustMeeting(t, RunMeetingOnce(db, meeting.ID))
	var completed domainmeeting.Meeting
	mustMeeting(t, db.First(&completed, meeting.ID).Error)
	if completed.Status != domainkernel.MeetingCompleted || completed.Summary == nil || *completed.Summary != "summary from moderator" || completed.Conclusion == nil || *completed.Conclusion != "conclusion from moderator" {
		t.Fatalf("meeting recap fields mismatch: %+v", completed)
	}
	if completed.Topic != "final topic" {
		t.Fatalf("topic not updated: %s", completed.Topic)
	}
	tags := TagsFromJSON(completed.Tags)
	if len(tags) != 2 || tags[0] != "bank" || tags[1] != "pilot" {
		t.Fatalf("tags mismatch: %+v", tags)
	}
	var watchlistCount, wakeCount, orderCount int64
	db.Model(&domainmarket.WatchlistItem{}).Where("code = ?", "000001").Count(&watchlistCount)
	db.Model(&domainwake.Plan{}).Where("meeting_id = ?", meeting.ID).Count(&wakeCount)
	db.Model(&domainpaper.Order{}).Where("meeting_id = ?", meeting.ID).Count(&orderCount)
	if watchlistCount != 1 || wakeCount != 1 || orderCount != 1 {
		t.Fatalf("action counts mismatch: watch=%d wake=%d order=%d", watchlistCount, wakeCount, orderCount)
	}
}

func TestRunMeetingOnceManagedRunnerPlansToolRequestsAndRecap(t *testing.T) {
	responses := []string{
		`{"content":"kickoff","continue_discussion":true,"questions":[{"target":"analyst","question":"check quote"}],"focus_roles":["analyst"]}`,
		`not json`,
		`{"type":"tool_request","tool_calls":[{"tool":"market.realtime_quote","arguments":{"code":"600519"},"reason":"need quote"}]}`,
		`{"type":"analysis","content":"quote reviewed","questions":[{"target":"all","question":"any risk?"}],"mentions":["moderator"],"citations":["market.realtime_quote"],"confidence":"high"}`,
		`{"content":"enough","continue_discussion":false,"questions":[],"focus_roles":[]}`,
		`{"topic":"final 600519","tags":["quote"],"summary":"managed summary","conclusion":"managed conclusion","watchlist_actions":[{"code":"600519","note":"track quote","active":true}],"wake_plans":[],"orders":[]}`,
	}
	var calls int
	var requestPayloads []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		requestPayloads = append(requestPayloads, payload)
		if calls >= len(responses) {
			t.Fatalf("unexpected extra model call %d", calls+1)
		}
		content := responses[calls]
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}}}})
	}))
	defer srv.Close()

	db := newMeetingTestDB(t)
	settings := config.Load()
	encrypted, err := security.New(settings).EncryptSecret("test-key")
	mustMeeting(t, err)
	secret := domainsettings.Secret{Kind: domainkernel.SecretKindAIProvider, Name: "provider:test", EncryptedValue: encrypted}
	mustMeeting(t, db.Create(&secret).Error)
	provider := domainai.Provider{Name: "test", BaseURL: srv.URL, DefaultModel: "m", APIKeySecretID: &secret.ID, Enabled: true}
	mustMeeting(t, db.Create(&provider).Error)
	model := "m"
	moderator := domainai.AgentRole{Key: "moderator", Name: "Moderator", Responsibility: "moderate", PromptTemplate: "moderate", ProviderID: &provider.ID, Model: &model, Enabled: true, SortOrder: 1, ToolNames: JSON([]string{"meeting.transcript"}), SkillNames: JSONList(nil)}
	analyst := domainai.AgentRole{Key: "analyst", Name: "Analyst", Responsibility: "analyze", PromptTemplate: "analyze", ProviderID: &provider.ID, Model: &model, Enabled: true, SortOrder: 2, ToolNames: JSON([]string{"market.realtime_quote"}), SkillNames: JSONList(nil)}
	teamID := ensureTestResearchTeam(db)
	mustMeeting(t, saveTestAgentRole(db, teamID, &moderator))
	mustMeeting(t, saveTestAgentRole(db, teamID, &analyst))
	mustMeeting(t, db.Create(&domainsettings.AppSetting{Key: "MEETING_MAX_ROUNDS", Value: JSON(map[string]any{"value": 2})}).Error)
	mustMeeting(t, db.Create(&domainmarket.RealtimeQuote{Code: "600519", QuoteTime: time.Now(), Price: decimal.NewFromInt(1688), Provider: "test"}).Error)
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)
	_, err = AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, "context", map[string]any{"status": "meeting_context", "related_symbols": []string{"600519"}})
	mustMeeting(t, err)

	mustMeeting(t, RunMeetingOnce(db, meeting.ID))
	if calls != len(responses) {
		t.Fatalf("expected %d model calls, got %d", len(responses), calls)
	}
	if len(requestPayloads) != len(responses) {
		t.Fatalf("expected %d captured payloads, got %d", len(responses), len(requestPayloads))
	}
	golden := managedPayloadGoldenFragments(t)
	kickoffMessages := requestMessages(t, requestPayloads[0])
	assertGoldenFragments(t, "moderator kickoff system", kickoffMessages[0]["content"], golden["moderator_kickoff_system"])
	assertGoldenFragments(t, "moderator kickoff user", kickoffMessages[1]["content"], golden["moderator_kickoff_user"])
	roleMessages := requestMessages(t, requestPayloads[1])
	if roleMessages[0]["role"] != "system" {
		t.Fatalf("role system prompt lost JSON/tool schema: %+v", roleMessages[0])
	}
	assertGoldenFragments(t, "role system", roleMessages[0]["content"], golden["role_system"])
	assertGoldenFragments(t, "role user", roleMessages[1]["content"], golden["role_user"])
	retryMessages := requestMessages(t, requestPayloads[2])
	if len(retryMessages) != 3 {
		t.Fatalf("JSON repair retry payload drifted: %+v", retryMessages)
	}
	assertGoldenFragments(t, "JSON repair retry", retryMessages[2]["content"], golden["json_repair_user"])
	toolFollowupMessages := requestMessages(t, requestPayloads[3])
	assertGoldenFragments(t, "tool follow-up", toolFollowupMessages[1]["content"], golden["tool_followup_user"])
	recapMessages := requestMessages(t, requestPayloads[len(requestPayloads)-1])
	assertGoldenFragments(t, "recap system", recapMessages[0]["content"], golden["recap_system"])
	var completed domainmeeting.Meeting
	mustMeeting(t, db.First(&completed, meeting.ID).Error)
	if completed.Status != domainkernel.MeetingCompleted || completed.Topic != "final 600519" || completed.Summary == nil || *completed.Summary != "managed summary" || completed.Conclusion == nil || *completed.Conclusion != "managed conclusion" {
		t.Fatalf("managed completion mismatch: %+v", completed)
	}
	var toolLog domainmeeting.ToolCallLog
	mustMeeting(t, db.First(&toolLog, "meeting_id = ? AND role_key = ? AND tool_name = ?", meeting.ID, "analyst", "market.realtime_quote").Error)
	var analystEvent domainmeeting.Event
	mustMeeting(t, db.First(&analystEvent, "meeting_id = ? AND role_key = ? AND type = ?", meeting.ID, "analyst", domainkernel.EventRoleMessage).Error)
	if !strings.Contains(string(analystEvent.Payload), `"confidence":"high"`) || !strings.Contains(string(analystEvent.Payload), "market.realtime_quote") {
		t.Fatalf("analyst payload missing metadata: %s", analystEvent.Payload)
	}
	var watchlist domainmarket.WatchlistItem
	mustMeeting(t, db.First(&watchlist, "code = ?", "600519").Error)
}

func TestExtractMeetingRecapJSONToleratesTrailingTextAfterObject(t *testing.T) {
	data, err := extractMeetingRecapJSON(`{"content":"# Zijin Mining (601899) recap\nFollow-up notes","continue_discussion":true}` + " extra trailing prose")
	if err != nil {
		t.Fatal(err)
	}
	if data["content"] == "" || data["continue_discussion"] != true {
		t.Fatalf("unexpected extracted JSON: %+v", data)
	}
}

func TestFallbackModeratorPlanAcceptsSubstantiveMarkdown(t *testing.T) {
	moderator := domainai.AgentRole{Key: "moderator"}
	roles := []domainai.AgentRole{
		moderator,
		{Key: "fundamental", Name: "Fundamental"},
		{Key: "risk", Name: "Risk"},
	}
	raw := "# Zijin Mining (601899) research Round 1 kickoff summary\n\nCore financial and production validation: 2025 revenue and profit need verification.\n\n- Fundamental analyst should verify copper and gold production and costs.\n- Risk officer should evaluate commodity price and FX risks."
	plan, ok := fallbackModeratorPlanFromText(raw, roles, moderator, true)
	if !ok {
		t.Fatal("expected substantive markdown fallback")
	}
	if !plan.ContinueDiscussion || !strings.Contains(plan.Content, "Zijin Mining") || len(plan.Questions) != 2 {
		t.Fatalf("unexpected fallback plan: %+v", plan)
	}
	if _, ok := fallbackModeratorPlanFromText("not json", roles, moderator, true); ok {
		t.Fatal("short invalid text should not be accepted as a moderator plan")
	}
}

func TestRunMeetingOnceFailsStartedManagedMeetingWhenModeratorKickoffCrashes(t *testing.T) {
	fastMeetingAITestSettings(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "not json"}}}})
	}))
	defer srv.Close()
	db := newMeetingTestDB(t)
	providerID := seedMeetingAIProvider(t, db, srv.URL)
	model := "m"
	moderator := domainai.AgentRole{Key: "moderator", Name: "Moderator", Responsibility: "moderate", PromptTemplate: "moderate", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 1}
	participant := domainai.AgentRole{Key: "fundamental", Name: "Fundamental", Responsibility: "analyze", PromptTemplate: "analyze", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 2}
	teamID := ensureTestResearchTeam(db)
	mustMeeting(t, saveTestAgentRole(db, teamID, &moderator))
	mustMeeting(t, saveTestAgentRole(db, teamID, &participant))
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)

	mustMeeting(t, RunMeetingOnce(db, meeting.ID))
	var failed domainmeeting.Meeting
	mustMeeting(t, db.First(&failed, meeting.ID).Error)
	if failed.Status != domainkernel.MeetingFailed || failed.RunID != nil || failed.CompletedAt == nil || failed.HeartbeatAt == nil {
		t.Fatalf("expected failed meeting with cleared run id, got %+v", failed)
	}
	if failed.Conclusion == nil {
		t.Fatalf("failure conclusion mismatch: %+v", failed.Conclusion)
	}
	assertGoldenFragments(t, "kickoff crash failure", *failed.Conclusion, managedPayloadGoldenFragments(t)["kickoff_crash_failure"])
	var started, errors int64
	db.Model(&domainmeeting.Event{}).Where("meeting_id = ? AND content = ?", meeting.ID, "Virtual research meeting started.").Count(&started)
	db.Model(&domainmeeting.Event{}).Where("meeting_id = ? AND type = ? AND content LIKE ?", meeting.ID, domainkernel.EventError, "%invalid JSON%").Count(&errors)
	if started != 1 || errors != 1 || calls != 2 {
		t.Fatalf("failure events/calls mismatch started=%d errors=%d calls=%d", started, errors, calls)
	}
}

func TestRunMeetingOnceManagedRoleProviderErrorDoesNotAbortRecap(t *testing.T) {
	fastMeetingAITestSettings(t)
	var moderatorCalls, badRoleCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] == "bad-role" {
			badRoleCalls++
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		moderatorCalls++
		response := `{"content":"kickoff","continue_discussion":true,"questions":[{"target":"analyst","question":"answer"}]}`
		if moderatorCalls == 2 {
			response = `{"topic":"final","tags":[],"summary":"summary despite role error","conclusion":"conclusion","watchlist_actions":[],"wake_plans":[],"orders":[]}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": response}}}})
	}))
	defer srv.Close()
	db := newMeetingTestDB(t)
	providerID := seedMeetingAIProvider(t, db, srv.URL)
	moderatorModel := "moderator-model"
	badModel := "bad-role"
	moderator := domainai.AgentRole{Key: "moderator", Name: "Moderator", Responsibility: "moderate", PromptTemplate: "moderate", ProviderID: &providerID, Model: &moderatorModel, Enabled: true, SortOrder: 1}
	analyst := domainai.AgentRole{Key: "analyst", Name: "Analyst", Responsibility: "analyze", PromptTemplate: "analyze", ProviderID: &providerID, Model: &badModel, Enabled: true, SortOrder: 2}
	teamID := ensureTestResearchTeam(db)
	mustMeeting(t, saveTestAgentRole(db, teamID, &moderator))
	mustMeeting(t, saveTestAgentRole(db, teamID, &analyst))
	mustMeeting(t, db.Create(&domainsettings.AppSetting{Key: "MEETING_MAX_ROUNDS", Value: JSON(map[string]any{"value": 1})}).Error)
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)

	mustMeeting(t, RunMeetingOnce(db, meeting.ID))
	var completed domainmeeting.Meeting
	mustMeeting(t, db.First(&completed, meeting.ID).Error)
	if completed.Status != domainkernel.MeetingCompleted || completed.RunID != nil || completed.Summary == nil || *completed.Summary != "summary despite role error" {
		t.Fatalf("expected completed meeting after role error, got %+v", completed)
	}
	var roleErrors int64
	db.Model(&domainmeeting.Event{}).Where("meeting_id = ? AND role_key = ? AND type = ? AND content LIKE ?", meeting.ID, "analyst", domainkernel.EventError, "%status=401%").Count(&roleErrors)
	var roleErrorEvent domainmeeting.Event
	mustMeeting(t, db.First(&roleErrorEvent, "meeting_id = ? AND role_key = ? AND type = ?", meeting.ID, "analyst", domainkernel.EventError).Error)
	assertGoldenFragments(t, "role provider error", roleErrorEvent.Content, managedPayloadGoldenFragments(t)["role_provider_error"])
	if roleErrors != 1 || badRoleCalls != 2 || moderatorCalls != 2 {
		t.Fatalf("role error/call mismatch roleErrors=%d badCalls=%d moderatorCalls=%d", roleErrors, badRoleCalls, moderatorCalls)
	}
}

func TestRunMeetingOnceManagedRunnerRetriesInvalidWakePlanRecap(t *testing.T) {
	fastMeetingAITestSettings(t)
	responses := []string{
		`{"content":"kickoff","continue_discussion":true,"questions":[{"target":"analyst","question":"answer"}]}`,
		`{"type":"analysis","content":"Use 510300 only if it crosses 4.25.","confidence":"medium"}`,
		`{"topic":"final","tags":["etf"],"summary":"summary","conclusion":"watch trigger","watchlist_actions":[],"wake_plans":[{"trigger_type":"indicator","reason":"price trigger","trigger_config":{"threshold":4.25}}],"orders":[]}`,
		`{"topic":"final","tags":["etf"],"summary":"summary","conclusion":"watch trigger","watchlist_actions":[],"wake_plans":[{"trigger_type":"indicator","reason":"price trigger","trigger_config":{"code":"510300","threshold":4.25}}],"orders":[]}`,
	}
	var calls int
	var requestPayloads []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		requestPayloads = append(requestPayloads, payload)
		if calls >= len(responses) {
			t.Fatalf("unexpected extra model call %d", calls+1)
		}
		content := responses[calls]
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}}}})
	}))
	defer srv.Close()
	db := newMeetingTestDB(t)
	providerID := seedMeetingAIProvider(t, db, srv.URL)
	model := "m"
	moderator := domainai.AgentRole{Key: "moderator", Name: "Moderator", Responsibility: "moderate", PromptTemplate: "moderate", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 1}
	analyst := domainai.AgentRole{Key: "analyst", Name: "Analyst", Responsibility: "analyze", PromptTemplate: "analyze", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 2}
	teamID := ensureTestResearchTeam(db)
	mustMeeting(t, saveTestAgentRole(db, teamID, &moderator))
	mustMeeting(t, saveTestAgentRole(db, teamID, &analyst))
	mustMeeting(t, db.Create(&domainsettings.AppSetting{Key: "MEETING_MAX_ROUNDS", Value: JSON(map[string]any{"value": 1})}).Error)
	meeting, err := CreateMeeting(db, "510300 trigger research", "manual")
	mustMeeting(t, err)

	mustMeeting(t, RunMeetingOnce(db, meeting.ID))
	if calls != len(responses) {
		t.Fatalf("expected %d model calls, got %d", len(responses), calls)
	}
	retryMessages := requestMessages(t, requestPayloads[3])
	if len(retryMessages) != 3 || !strings.Contains(retryMessages[2]["content"], "indicator wake condition 1 requires code") {
		t.Fatalf("semantic recap retry payload mismatch: %+v", retryMessages)
	}
	var errorCount int64
	db.Model(&domainmeeting.Event{}).Where("meeting_id = ? AND type = ? AND payload LIKE ?", meeting.ID, domainkernel.EventError, "%wake_plan_error%").Count(&errorCount)
	if errorCount != 0 {
		t.Fatalf("semantic retry should avoid wake_plan_error, got %d", errorCount)
	}
	var plan domainwake.Plan
	mustMeeting(t, db.First(&plan, "meeting_id = ?", meeting.ID).Error)
	if !strings.Contains(string(plan.TriggerConfig), "510300") {
		t.Fatalf("wake plan missing corrected code: %s", plan.TriggerConfig)
	}
}

func TestRunMeetingOnceManagedRunnerMarksRecapFailure(t *testing.T) {
	fastMeetingAITestSettings(t)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"content":"kickoff","continue_discussion":true,"questions":[{"target":"analyst","question":"answer"}]}`}}}})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"type":"analysis","content":"analysis"}`}}}})
		default:
			http.Error(w, "recap unavailable", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	db := newMeetingTestDB(t)
	providerID := seedMeetingAIProvider(t, db, srv.URL)
	model := "m"
	moderator := domainai.AgentRole{Key: "moderator", Name: "Moderator", Responsibility: "moderate", PromptTemplate: "moderate", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 1}
	analyst := domainai.AgentRole{Key: "analyst", Name: "Analyst", Responsibility: "analyze", PromptTemplate: "analyze", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 2}
	teamID := ensureTestResearchTeam(db)
	mustMeeting(t, saveTestAgentRole(db, teamID, &moderator))
	mustMeeting(t, saveTestAgentRole(db, teamID, &analyst))
	mustMeeting(t, db.Create(&domainsettings.AppSetting{Key: "MEETING_MAX_ROUNDS", Value: JSON(map[string]any{"value": 1})}).Error)
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)

	mustMeeting(t, RunMeetingOnce(db, meeting.ID))
	var failed domainmeeting.Meeting
	mustMeeting(t, db.First(&failed, meeting.ID).Error)
	if failed.Status != domainkernel.MeetingFailed || failed.RunID != nil || failed.Conclusion == nil {
		t.Fatalf("expected recap failure, got %+v", failed)
	}
	assertGoldenFragments(t, "recap failure", *failed.Conclusion, managedPayloadGoldenFragments(t)["recap_failure"])
	var failureEvent domainmeeting.Event
	mustMeeting(t, db.First(&failureEvent, "meeting_id = ? AND type = ? AND content LIKE ?", meeting.ID, domainkernel.EventError, "Moderator recap failed:%").Error)
	assertGoldenFragments(t, "recap failure event", failureEvent.Content, managedPayloadGoldenFragments(t)["recap_failure"])
}

func TestRunMeetingOnceManagedRunnerDoesNotCompleteSupersededDuringRecap(t *testing.T) {
	fastMeetingAITestSettings(t)
	var calls int
	var meetingID uint
	db := newMeetingTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"content":"kickoff","continue_discussion":true,"questions":[{"target":"analyst","question":"answer"}]}`}}}})
		case 2:
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"type":"analysis","content":"analysis"}`}}}})
		default:
			replacementRun := "replacement-run"
			mustMeeting(t, db.Model(&domainmeeting.Meeting{}).Where("id = ?", meetingID).Updates(map[string]any{"status": domainkernel.MeetingQueued, "run_id": replacementRun}).Error)
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"topic":"should not win","tags":[],"summary":"stale","conclusion":"stale","watchlist_actions":[],"wake_plans":[],"orders":[]}`}}}})
		}
	}))
	defer srv.Close()
	providerID := seedMeetingAIProvider(t, db, srv.URL)
	model := "m"
	moderator := domainai.AgentRole{Key: "moderator", Name: "Moderator", Responsibility: "moderate", PromptTemplate: "moderate", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 1}
	analyst := domainai.AgentRole{Key: "analyst", Name: "Analyst", Responsibility: "analyze", PromptTemplate: "analyze", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 2}
	teamID := ensureTestResearchTeam(db)
	mustMeeting(t, saveTestAgentRole(db, teamID, &moderator))
	mustMeeting(t, saveTestAgentRole(db, teamID, &analyst))
	mustMeeting(t, db.Create(&domainsettings.AppSetting{Key: "MEETING_MAX_ROUNDS", Value: JSON(map[string]any{"value": 1})}).Error)
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)
	meetingID = meeting.ID

	mustMeeting(t, RunMeetingOnce(db, meeting.ID))
	var current domainmeeting.Meeting
	mustMeeting(t, db.First(&current, meeting.ID).Error)
	if current.Status != domainkernel.MeetingQueued || current.RunID == nil || *current.RunID != "replacement-run" || current.Summary != nil || current.Conclusion != nil {
		t.Fatalf("stale recap overwrote superseded run: %+v", current)
	}
	assertGoldenFragments(t, "superseded recap guard", *current.RunID, managedPayloadGoldenFragments(t)["superseded_recap_guard"])
	var conclusions int64
	db.Model(&domainmeeting.Event{}).Where("meeting_id = ? AND type = ?", meeting.ID, domainkernel.EventConclusion).Count(&conclusions)
	if conclusions != 0 {
		t.Fatalf("superseded run must not append conclusion, got %d", conclusions)
	}
}

func TestRunMeetingOnceFailsWhenNoEnabledRoles(t *testing.T) {
	db := newMeetingTestDB(t)
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)

	mustMeeting(t, RunMeetingOnce(db, meeting.ID))
	var failed domainmeeting.Meeting
	mustMeeting(t, db.First(&failed, meeting.ID).Error)
	if failed.Status != domainkernel.MeetingFailed || failed.RunID != nil || failed.Conclusion == nil {
		t.Fatalf("expected no-role failure, got %+v", failed)
	}
	assertGoldenFragments(t, "no roles failure", *failed.Conclusion, managedPayloadGoldenFragments(t)["no_roles_failure"])
}

func TestRunMeetingOnceEnforcesDailyTokenBudget(t *testing.T) {
	db := newMeetingTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("model provider should not be called after budget rejection")
	}))
	defer srv.Close()
	providerID := seedMeetingAIProvider(t, db, srv.URL)
	model := "m"
	moderator := domainai.AgentRole{Key: "moderator", Name: "Moderator", Responsibility: "moderate", PromptTemplate: "moderate", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 1}
	analyst := domainai.AgentRole{Key: "analyst", Name: "Analyst", Responsibility: "analyze", PromptTemplate: "analyze", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 2}
	teamID := ensureTestResearchTeam(db)
	mustMeeting(t, saveTestAgentRole(db, teamID, &moderator))
	mustMeeting(t, saveTestAgentRole(db, teamID, &analyst))
	mustMeeting(t, db.Create(&domainsettings.AppSetting{Key: "MEETING_DAILY_TOKEN_BUDGET", Value: JSON(map[string]any{"value": 1})}).Error)
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)

	mustMeeting(t, RunMeetingOnce(db, meeting.ID))
	var failed domainmeeting.Meeting
	mustMeeting(t, db.First(&failed, meeting.ID).Error)
	if failed.Status != domainkernel.MeetingFailed || failed.Conclusion == nil {
		t.Fatalf("expected token budget failure, got %+v", failed)
	}
	assertGoldenFragments(t, "token budget failure", *failed.Conclusion, managedPayloadGoldenFragments(t)["token_budget_failure"])
	if failed.TokenBudget != 0 {
		t.Fatalf("budget rejection should happen before reservation, got token_budget=%d", failed.TokenBudget)
	}
}

func TestRunMeetingOnceUsesProviderUsageForTokenBudget(t *testing.T) {
	db := newMeetingTestDB(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": "provider usage response"}}},
			"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 7, "total_tokens": 17},
		})
	}))
	defer srv.Close()
	providerID := seedMeetingAIProvider(t, db, srv.URL)
	model := "m"
	role := domainai.AgentRole{Key: "analyst", Name: "Analyst", Responsibility: "analyze", PromptTemplate: "analyze", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 1}
	teamID := ensureTestResearchTeam(db)
	mustMeeting(t, saveTestAgentRole(db, teamID, &role))
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)

	mustMeeting(t, RunMeetingOnce(db, meeting.ID))
	var completed domainmeeting.Meeting
	mustMeeting(t, db.First(&completed, meeting.ID).Error)
	if completed.TokenBudget != 17 {
		t.Fatalf("expected provider usage token budget 17, got %d", completed.TokenBudget)
	}
	var event domainmeeting.Event
	mustMeeting(t, db.First(&event, "meeting_id = ? AND role_key = ?", meeting.ID, "analyst").Error)
	if !strings.Contains(string(event.Payload), "token_usage") {
		t.Fatalf("expected token usage in payload: %s", event.Payload)
	}
}

func TestActualChatTokensUsesTokenizerFallback(t *testing.T) {
	messages := []map[string]string{{"role": "system", "content": "You are an A-share research assistant."}, {"role": "user", "content": "Analyze risks for 600519."}}
	got := actualChatTokens("gpt-4o-mini", messages, "Short answer", nil)
	want := chatPromptTokens("gpt-4o-mini", messages) + completionTokens("gpt-4o-mini", "Short answer")
	if got != want || got <= 0 {
		t.Fatalf("tokenizer fallback mismatch got=%d want=%d", got, want)
	}
}

func TestTokenizerAliasesCoverNonOpenAIProviderModels(t *testing.T) {
	messages := []map[string]string{{"role": "system", "content": "You are an A-share research assistant."}, {"role": "user", "content": "Analyze risks for 600519."}}
	for _, model := range []string{
		"deepseek-chat", "deepseek-ai/deepseek-chat", "qwen-plus", "dashscope/qwen-plus",
		"moonshot-v1-8k", "moonshotai/kimi-k2", "kimi-k2", "glm-4", "zhipu/glm-4-plus",
		"doubao-pro-32k", "volcengine/doubao-pro-32k", "baichuan2-turbo", "ernie-4.0",
		"abab6.5s", "anthropic/claude-3-5-sonnet", "claude-3-5-sonnet",
		"google/gemini-1.5-pro", "gemini-1.5-pro", "mistral-large", "meta/llama-3.1-70b",
		"mixtral-8x7b", "xai/grok-2", "openrouter/anthropic/claude-3.5-sonnet",
	} {
		if alias := tokenizerModelAlias(model); alias != "gpt-4o-mini" {
			t.Fatalf("%s expected gpt-4o-mini tokenizer alias, got %s", model, alias)
		}
		got := chatPromptTokens(model, messages)
		want := chatPromptTokens("gpt-4o-mini", messages)
		if got != want || got <= 0 {
			t.Fatalf("%s tokenizer alias mismatch got=%d want=%d", model, got, want)
		}
	}
}

func TestLocalMeetingRunCanCancelProviderRequest(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-requestStarted:
		default:
			close(requestStarted)
		}
		<-releaseRequest
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"content":"late","continue_discussion":false,"questions":[]}`}}}})
	}))
	defer srv.Close()
	db := newMeetingTestDB(t)
	providerID := seedMeetingAIProvider(t, db, srv.URL)
	model := "m"
	moderator := domainai.AgentRole{Key: "moderator", Name: "Moderator", Responsibility: "moderate", PromptTemplate: "moderate", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 1}
	analyst := domainai.AgentRole{Key: "analyst", Name: "Analyst", Responsibility: "analyze", PromptTemplate: "analyze", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 2}
	teamID := ensureTestResearchTeam(db)
	mustMeeting(t, saveTestAgentRole(db, teamID, &moderator))
	mustMeeting(t, saveTestAgentRole(db, teamID, &analyst))
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)

	if !StartLocalMeetingRun(db, meeting.ID) {
		t.Fatal("expected local run to start")
	}
	if StartLocalMeetingRun(db, meeting.ID) {
		t.Fatal("expected duplicate local run to be ignored")
	}
	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("model request did not start")
	}
	if !LocalMeetingRunActive(meeting.ID) {
		t.Fatal("expected local run to be registered")
	}
	if !CancelLocalMeetingRun(meeting.ID) {
		t.Fatal("expected local run cancellation")
	}
	close(releaseRequest)
	deadline := time.Now().Add(2 * time.Second)
	for LocalMeetingRunActive(meeting.ID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if LocalMeetingRunActive(meeting.ID) {
		t.Fatal("local run registry did not clear")
	}
}

func TestRunMeetingOnceWithContextCancellationDoesNotComplete(t *testing.T) {
	fastMeetingAITestSettings(t)
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-requestStarted:
		default:
			close(requestStarted)
		}
		<-releaseRequest
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"content":"late","continue_discussion":false,"questions":[]}`}}}})
	}))
	defer srv.Close()
	db := newMeetingTestDB(t)
	providerID := seedMeetingAIProvider(t, db, srv.URL)
	model := "m"
	moderator := domainai.AgentRole{Key: "moderator", Name: "Moderator", Responsibility: "moderate", PromptTemplate: "moderate", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 1}
	analyst := domainai.AgentRole{Key: "analyst", Name: "Analyst", Responsibility: "analyze", PromptTemplate: "analyze", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 2}
	teamID := ensureTestResearchTeam(db)
	mustMeeting(t, saveTestAgentRole(db, teamID, &moderator))
	mustMeeting(t, saveTestAgentRole(db, teamID, &analyst))
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- RunMeetingOnceWithContext(ctx, db, meeting.ID)
	}()
	select {
	case <-requestStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("model request did not start")
	}
	cancel()
	close(releaseRequest)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("cancellation should be swallowed for active managed runs, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("meeting run did not return after cancellation")
	}
	var current domainmeeting.Meeting
	mustMeeting(t, db.First(&current, meeting.ID).Error)
	if current.Status == domainkernel.MeetingCompleted || current.RunID == nil {
		t.Fatalf("cancelled context should not complete or clear active run by itself: %+v", current)
	}
}

func requestMessages(t *testing.T, payload map[string]any) []map[string]string {
	t.Helper()
	raw, ok := payload["messages"].([]any)
	if !ok {
		t.Fatalf("payload missing messages: %+v", payload)
	}
	out := make([]map[string]string, 0, len(raw))
	for _, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("message is not object: %+v", item)
		}
		out = append(out, map[string]string{"role": fmt.Sprint(obj["role"]), "content": fmt.Sprint(obj["content"])})
	}
	return out
}

func managedPayloadGoldenFragments(t *testing.T) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile("testdata/golden/meeting_managed_payload_fragments.json")
	if err != nil {
		t.Fatal(err)
	}
	var fragments map[string][]string
	if err := json.Unmarshal(raw, &fragments); err != nil {
		t.Fatal(err)
	}
	return fragments
}

func assertGoldenFragments(t *testing.T, label string, text string, fragments []string) {
	t.Helper()
	if len(fragments) == 0 {
		t.Fatalf("%s has no golden fragments", label)
	}
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			t.Fatalf("%s missing golden fragment %q in: %s", label, fragment, text)
		}
	}
}

func TestNotifyMeetingFinishedSendsTelegramBotMessage(t *testing.T) {
	db := newMeetingTestDB(t)
	settings := testTelegramBotSettings()
	settings.AppName = "TradingCopilot"
	settings.PublicBaseURL = "https://public.example"
	sec := seedTelegramBotSecrets(t, db, "token", "chat-1")
	mustMeeting(t, db.Create(&domainsettings.AppSetting{Key: "PUBLIC_BASE_URL", Value: JSON(map[string]any{"value": "https://runtime.example/"})}).Error)
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)
	meeting.Status = domainkernel.MeetingCompleted
	meeting.Conclusion = strPtr(strings.Repeat("x", 2600))
	mustMeeting(t, db.Save(meeting).Error)

	oldSender := telegramBotSendMessageWithClient
	defer func() { telegramBotSendMessageWithClient = oldSender }()
	var sentText string
	telegramBotSendMessageWithClient = func(apiBase string, token string, chatID string, text string, _ *http.Client) (map[string]any, error) {
		if token != "token" || chatID != "chat-1" {
			t.Fatalf("unexpected bot credentials token=%s chat=%s", token, chatID)
		}
		sentText = text
		return map[string]any{"chat_id": chatID, "message_id": float64(42)}, nil
	}

	NotifyMeetingFinished(db, meeting, settings, sec)
	if !strings.Contains(sentText, "TradingCopilot meeting finished") || !strings.Contains(sentText, "https://runtime.example/meetings/") {
		t.Fatalf("unexpected notification text: %s", sentText)
	}
	if len(sentText) >= 2800 || strings.Contains(sentText, strings.Repeat("x", 2501)) {
		t.Fatalf("conclusion was not truncated: len=%d", len(sentText))
	}
	var event domainmeeting.Event
	mustMeeting(t, db.First(&event, "meeting_id = ? AND type = ? AND content = ?", meeting.ID, domainkernel.EventSystem, "Telegram completion notification sent.").Error)
	if !strings.Contains(string(event.Payload), "telegram_notified") {
		t.Fatalf("notification payload mismatch: %s", event.Payload)
	}
}

func TestNotifyMeetingFinishedRecordsTelegramFailure(t *testing.T) {
	db := newMeetingTestDB(t)
	settings := testTelegramBotSettings()
	settings.PublicBaseURL = "https://public.example"
	meeting, err := CreateMeeting(db, "maotai research", "manual")
	mustMeeting(t, err)
	meeting.Status = domainkernel.MeetingCompleted
	mustMeeting(t, db.Save(meeting).Error)

	NotifyMeetingFinished(db, meeting, settings, security.New(settings))
	var event domainmeeting.Event
	mustMeeting(t, db.First(&event, "meeting_id = ? AND type = ?", meeting.ID, domainkernel.EventError).Error)
	if !strings.Contains(event.Content, "Telegram completion notification failed") || !strings.Contains(string(event.Payload), "telegram_notify_failed") {
		t.Fatalf("failure event mismatch: %+v payload=%s", event, event.Payload)
	}
}

func newMeetingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	installAgentRoleMirror(db)
	return db
}

func mustMeeting(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func seedMeetingAIProvider(t *testing.T, db *gorm.DB, baseURL string) uint {
	t.Helper()
	settings := config.Load()
	encrypted, err := security.New(settings).EncryptSecret("test-key")
	mustMeeting(t, err)
	secret := domainsettings.Secret{Kind: domainkernel.SecretKindAIProvider, Name: "provider:test:" + baseURL, EncryptedValue: encrypted}
	mustMeeting(t, db.Create(&secret).Error)
	provider := domainai.Provider{Name: "test-" + fmt.Sprint(time.Now().UnixNano()), BaseURL: baseURL, DefaultModel: "m", APIKeySecretID: &secret.ID, Enabled: true}
	mustMeeting(t, db.Create(&provider).Error)
	return provider.ID
}

func fastMeetingAITestSettings(t *testing.T) {
	t.Helper()
	t.Setenv("TC_ENV_FILE", "__missing_meeting_test_env__")
	t.Setenv("APP_SECRET_KEY", "meeting-test-secret")
	t.Setenv("AI_CHAT_MAX_ATTEMPTS", "1")
	t.Setenv("AI_JSON_MAX_ATTEMPTS", "2")
	t.Setenv("AI_CHAT_BACKOFF_BASE_SECONDS", "0.001")
	t.Setenv("AI_CHAT_BACKOFF_MAX_SECONDS", "0.001")
	t.Setenv("AI_CHAT_TIMEOUT_SECONDS", "2")
}
