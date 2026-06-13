package meeting

import (
	"context"
	"encoding/json"
	"fmt"
	appprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/app/prediction"
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/market"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"
	domainprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/prediction"
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
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	infrapaper "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/paper"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
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
	var ref domainmeeting.Reference
	mustMeeting(t, db.First(&ref, "source_meeting_id = ? AND target_meeting_id = ? AND reference_type = ?", meeting.ID, source.ID, "meeting").Error)
	if ref.TargetTopicSnapshot != source.Topic || ref.Note == nil || !strings.Contains(*ref.Note, "wake plan") {
		t.Fatalf("wake follow-up reference mismatch: %+v", ref)
	}
}

func TestApplyMeetingRecapActionsRejectsInvalidWakePlan(t *testing.T) {
	db := newMeetingTestDB(t)
	meeting, err := CreateMeeting(db, "source topic", "manual")
	mustMeeting(t, err)
	roleKey := "moderator"
	recap := map[string]any{
		"facts":     []string{"The meeting proposed an indicator follow-up."},
		"citations": []string{"@moderator"},
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
	mustMeeting(t, db.First(&event, "meeting_id = ? AND type = ? AND role_key = ? AND payload LIKE ?", meeting.ID, domainkernel.EventSystem, roleKey, "%recap_actions_blocked%").Error)
	if !strings.Contains(event.Content, "blocked by evidence policy") || !strings.Contains(event.Content, "requires code") {
		t.Fatalf("unexpected wake plan error event: %+v", event)
	}
}

func TestApplyMeetingRecapActionsBlocksExecutableActionsWithoutEvidence(t *testing.T) {
	db := newMeetingTestDB(t)
	meeting, err := CreateMeeting(db, "evidence gate", "manual")
	mustMeeting(t, err)
	roleKey := "moderator"
	recap := map[string]any{
		"summary": "track 000001",
		"watchlist_actions": []map[string]any{
			{"code": "000001", "active": true},
		},
		"wake_plans": []map[string]any{
			{"trigger_type": "time", "next_check_at": "2026-05-10 09:30:00", "trigger_config": map[string]any{"topic": "follow up"}},
		},
		"orders": []map[string]any{
			{"code": "000001", "side": "buy", "quantity": 100, "suggested_price": 10},
		},
	}

	mustMeeting(t, ApplyMeetingRecapActions(db, meeting, nil, roleKey, recap))
	var watchlistCount, wakeCount, orderCount int64
	db.Model(&domainmarket.WatchlistItem{}).Count(&watchlistCount)
	db.Model(&domainwake.Plan{}).Count(&wakeCount)
	db.Model(&domainpaper.Order{}).Count(&orderCount)
	if watchlistCount != 0 || wakeCount != 0 || orderCount != 0 {
		t.Fatalf("blocked recap actions should not create records, watchlist=%d wake=%d orders=%d", watchlistCount, wakeCount, orderCount)
	}
	var event domainmeeting.Event
	mustMeeting(t, db.First(&event, "meeting_id = ? AND type = ? AND role_key = ? AND payload LIKE ?", meeting.ID, domainkernel.EventSystem, roleKey, "%recap_actions_blocked%").Error)
	if !strings.Contains(event.Content, "blocked by evidence policy") || !strings.Contains(event.Content, "structured fact or inference") {
		t.Fatalf("unexpected blocked action event: %+v", event)
	}
	payload := meetingTestPayload(t, event)
	if payload["suggestion_count"] != float64(3) {
		t.Fatalf("blocked action suggestion count mismatch: %+v", payload)
	}
	assertRecapActionSuggestion(t, payload, 0, "watchlist", "blocked")
	assertRecapActionSuggestion(t, payload, 1, "wake_plan", "blocked")
	assertRecapActionSuggestion(t, payload, 2, "paper_order", "blocked")
}

func TestApplyMeetingRecapActionsRequiresReviewForWeakRoleOnlyCitations(t *testing.T) {
	db := newMeetingTestDB(t)
	meeting, err := CreateMeeting(db, "weak evidence", "manual")
	mustMeeting(t, err)
	roleKey := "moderator"
	recap := map[string]any{
		"facts":      []string{"000001 was mentioned by the moderator."},
		"inferences": []string{"A follow-up action may be useful."},
		"citations":  []string{"@moderator"},
		"watchlist_actions": []map[string]any{
			{"code": "000001", "active": true},
		},
		"wake_plans": []map[string]any{
			{"trigger_type": "time", "next_check_at": "2026-05-10 09:30:00", "trigger_config": map[string]any{"topic": "follow up"}},
		},
		"orders": []map[string]any{
			{"code": "000001", "side": "buy", "quantity": 100, "suggested_price": 10},
		},
	}

	mustMeeting(t, ApplyMeetingRecapActions(db, meeting, nil, roleKey, recap))
	var watchlistCount, wakeCount, orderCount int64
	db.Model(&domainmarket.WatchlistItem{}).Count(&watchlistCount)
	db.Model(&domainwake.Plan{}).Count(&wakeCount)
	db.Model(&domainpaper.Order{}).Count(&orderCount)
	if watchlistCount != 0 || wakeCount != 0 || orderCount != 0 {
		t.Fatalf("weakly cited recap actions should wait for review, watchlist=%d wake=%d orders=%d", watchlistCount, wakeCount, orderCount)
	}
	var event domainmeeting.Event
	mustMeeting(t, db.First(&event, "meeting_id = ? AND type = ? AND role_key = ? AND payload LIKE ?", meeting.ID, domainkernel.EventSystem, roleKey, "%recap_actions_review_required%").Error)
	if !strings.Contains(event.Content, "manual review") || !strings.Contains(string(event.Payload), "weak_reference_review") {
		t.Fatalf("unexpected review-required event: %+v payload=%s", event, event.Payload)
	}
	payload := meetingTestPayload(t, event)
	if payload["suggestion_count"] != float64(3) || payload["disposition"] != "manual_review_required" {
		t.Fatalf("review action suggestion payload mismatch: %+v", payload)
	}
	assertRecapActionSuggestion(t, payload, 0, "watchlist", "manual_review_required")
	assertRecapActionSuggestion(t, payload, 1, "wake_plan", "manual_review_required")
	assertRecapActionSuggestion(t, payload, 2, "paper_order", "manual_review_required")
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
		"facts":      []string{"000001 was discussed as a China-listed candidate."},
		"inferences": []string{"A small pilot order and follow-up wake plan are justified."},
		"citations":  []string{"@analyst", "market.realtime_quote"},
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
		"facts":      []string{"600519 and 000001 were discussed in the recap."},
		"inferences": []string{"Both symbols should stay visible for follow-up review."},
		"citations":  []string{"@moderator", "meeting.context"},
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

func TestApplyMeetingRecapActionsBlocksPaperOrdersForPredictionMarketTeam(t *testing.T) {
	db := newMeetingTestDB(t)
	team := persistmodel.ResearchTeam{Name: "Prediction Team", AssetClass: "prediction_market", Active: true}
	mustMeeting(t, db.Create(&team).Error)
	meeting, err := CreateMeeting(db, "prediction market research", "manual")
	mustMeeting(t, err)
	meeting.ResearchTeamID = team.ID
	mustMeeting(t, db.Model(&domainmeeting.Meeting{}).Where("id = ?", meeting.ID).Update("research_team_id", team.ID).Error)
	role := "moderator"
	recapEvent, err := AppendEvent(db, meeting.ID, domainkernel.EventRoleMessage, &role, "prediction recap", map[string]any{"status": "recap_completed"})
	mustMeeting(t, err)
	recap := map[string]any{
		"summary":       "prediction market only",
		"conclusion":    "observe market, do not trade",
		"facts":         []string{"prediction.market_snapshot provided the market state."},
		"inferences":    []string{"the event should remain under observation."},
		"citations":     []string{"prediction.market_snapshot"},
		"evidence_gaps": []string{},
		"watchlist_actions": []map[string]any{
			{"code": "000001", "note": "should not become A-share watchlist", "active": true},
		},
		"wake_plans": []map[string]any{},
		"orders": []map[string]any{
			{"code": "000001", "side": "buy", "quantity": 100, "suggested_price": 10, "reason": "should be blocked"},
		},
	}
	mustMeeting(t, ApplyMeetingRecapActions(db, meeting, recapEvent, role, recap))

	var orderCount int64
	db.Model(&domainpaper.Order{}).Count(&orderCount)
	var watchlistCount int64
	db.Model(&domainmarket.WatchlistItem{}).Count(&watchlistCount)
	if orderCount != 0 || watchlistCount != 0 {
		t.Fatalf("prediction meeting should not create executable actions, watchlist=%d orders=%d", watchlistCount, orderCount)
	}
	var event domainmeeting.Event
	mustMeeting(t, db.First(&event, "meeting_id = ? AND type = ? AND payload LIKE ?", meeting.ID, domainkernel.EventSystem, "%prediction_market_actions_blocked%").Error)
}

func TestApplyMeetingRecapActionsAppliesPredictionWatchlistActions(t *testing.T) {
	db := newMeetingTestDB(t)
	team := persistmodel.ResearchTeam{Name: "Prediction Team", AssetClass: "prediction_market", Active: true}
	mustMeeting(t, db.Create(&team).Error)
	market := persistmodel.PredictionMarket{
		Provider:         "polymarket",
		ExternalMarketID: "m-1",
		Question:         "Will the US and Iran sign a permanent peace deal?",
		Slug:             "us-iran-peace-deal",
		Outcomes:         datatypes.JSON([]byte(`["Yes","No"]`)),
		OutcomePrices:    datatypes.JSON([]byte(`["0.36","0.64"]`)),
		CLOBTokenIDs:     datatypes.JSON([]byte(`["yes-token","no-token"]`)),
		Active:           true,
		EnableOrderBook:  true,
	}
	mustMeeting(t, db.Create(&market).Error)
	meeting, err := CreateMeeting(db, "prediction market watchlist action", "manual")
	mustMeeting(t, err)
	meeting.ResearchTeamID = team.ID
	mustMeeting(t, db.Model(&domainmeeting.Meeting{}).Where("id = ?", meeting.ID).Update("research_team_id", team.ID).Error)
	role := "moderator"
	recapEvent, err := AppendEvent(db, meeting.ID, domainkernel.EventRoleMessage, &role, "prediction recap", map[string]any{"status": "recap_completed"})
	mustMeeting(t, err)
	recap := map[string]any{
		"summary":    "prediction market only",
		"conclusion": "Keep the Polymarket peace-deal market visible for follow-up evidence.",
		"facts":      []string{"prediction.market_snapshot provided the market state."},
		"inferences": []string{"the market should remain under observation."},
		"citations":  []string{"prediction.market_snapshot"},
		"wake_plans": []map[string]any{},
		"orders":     []map[string]any{},
		"watchlist_actions": []map[string]any{
			{"code": "000001", "note": "should still be blocked", "active": true},
		},
		"prediction_watchlist_actions": []map[string]any{
			{"market_id": market.ID, "note": "Follow official settlement evidence and liquidity.", "active": true},
		},
	}
	mustMeeting(t, ApplyMeetingRecapActions(db, meeting, recapEvent, role, recap))

	var predictionItems []persistmodel.PredictionWatchlistItem
	mustMeeting(t, db.Where("research_team_id = ? AND market_id = ?", team.ID, market.ID).Find(&predictionItems).Error)
	if len(predictionItems) != 1 || predictionItems[0].Note == nil || !strings.Contains(*predictionItems[0].Note, "settlement evidence") {
		t.Fatalf("expected prediction watchlist item, got %+v", predictionItems)
	}
	if predictionItems[0].SourceMeetingID == nil || *predictionItems[0].SourceMeetingID != meeting.ID || predictionItems[0].SourceMeetingEventID == nil || *predictionItems[0].SourceMeetingEventID != recapEvent.ID {
		t.Fatalf("prediction watchlist item should link back to source meeting recap, got %+v", predictionItems[0])
	}
	var stockWatchlistCount int64
	mustMeeting(t, db.Model(&domainmarket.WatchlistItem{}).Count(&stockWatchlistCount).Error)
	if stockWatchlistCount != 0 {
		t.Fatalf("prediction recap should not write A-share watchlist rows, got %d", stockWatchlistCount)
	}
	var updateEvent domainmeeting.Event
	mustMeeting(t, db.First(&updateEvent, "meeting_id = ? AND type = ? AND payload LIKE ?", meeting.ID, domainkernel.EventToolResult, "%prediction_watchlist_updated%").Error)
	var updatePayload map[string]any
	mustMeeting(t, json.Unmarshal(updateEvent.Payload, &updatePayload))
	if updatePayload["tool"] != "prediction.upsert_watchlist" || uintFromAny(updatePayload["market_id"]) != market.ID || updatePayload["market_question"] != market.Question || updatePayload["market_slug"] != market.Slug {
		t.Fatalf("prediction watchlist update should expose market evidence payload, got %+v", updatePayload)
	}
	if uintFromAny(updatePayload["source_meeting_id"]) != meeting.ID || uintFromAny(updatePayload["source_meeting_event_id"]) != recapEvent.ID || updatePayload["source_role_key"] != role {
		t.Fatalf("prediction watchlist update should expose source meeting link, got %+v", updatePayload)
	}
}

func TestApplyMeetingRecapActionsFiltersAShareWakePlansForPredictionMarketTeam(t *testing.T) {
	db := newMeetingTestDB(t)
	team := persistmodel.ResearchTeam{Name: "Prediction Team", AssetClass: "prediction_market", Active: true}
	mustMeeting(t, db.Create(&team).Error)
	meeting, err := CreateMeeting(db, "prediction market wake boundary", "manual")
	mustMeeting(t, err)
	meeting.ResearchTeamID = team.ID
	mustMeeting(t, db.Model(&domainmeeting.Meeting{}).Where("id = ?", meeting.ID).Update("research_team_id", team.ID).Error)
	role := "moderator"
	recapEvent, err := AppendEvent(db, meeting.ID, domainkernel.EventRoleMessage, &role, "prediction recap", map[string]any{"status": "recap_completed"})
	mustMeeting(t, err)
	recap := map[string]any{
		"summary":           "prediction market only",
		"conclusion":        "observe market follow-up",
		"facts":             []string{"prediction.market_snapshot provided the market state."},
		"inferences":        []string{"the event should remain under observation."},
		"citations":         []string{"prediction.market_snapshot"},
		"evidence_gaps":     []string{},
		"watchlist_actions": []map[string]any{},
		"orders":            []map[string]any{},
		"wake_plans": []map[string]any{
			{"trigger_type": "time", "next_check_at": "2026-07-01 09:00:00", "reason": "scheduled evidence refresh", "trigger_config": map[string]any{"topic": "Check Polymarket Iran peace deal market"}},
			{"trigger_type": "event", "reason": "watch official-source updates", "trigger_config": map[string]any{"keywords": []string{"Iran peace deal", "Polymarket"}, "prediction_market_ids": []uint{42}, "topic": "Prediction market follow-up"}},
			{"trigger_type": "indicator", "reason": "must be blocked", "trigger_config": map[string]any{"code": "600519", "threshold": "100"}},
			{"trigger_type": "event", "reason": "must be blocked", "trigger_config": map[string]any{"related_symbols": []string{"600519"}, "topic": "A-share spillover"}},
		},
	}
	mustMeeting(t, ApplyMeetingRecapActions(db, meeting, recapEvent, role, recap))

	var plans []domainwake.Plan
	mustMeeting(t, db.Where("meeting_id = ?", meeting.ID).Order("id").Find(&plans).Error)
	if len(plans) != 2 {
		t.Fatalf("prediction meeting should keep only two valid wake plans, got %+v", plans)
	}
	for _, plan := range plans {
		if plan.TriggerType == domainkernel.WakeIndicator || strings.Contains(string(plan.TriggerConfig), "600519") {
			t.Fatalf("prediction wake plan leaked A-share trigger config: %+v", plan)
		}
	}
	var blockedCount int64
	mustMeeting(t, db.Model(&domainmeeting.Event{}).Where("meeting_id = ? AND payload LIKE ?", meeting.ID, "%prediction_market_wake_plan_blocked%").Count(&blockedCount).Error)
	if blockedCount != 2 {
		t.Fatalf("expected two blocked wake plan audit events, got %d", blockedCount)
	}
}

func TestPredictionMarketRecapValidationRejectsAShareText(t *testing.T) {
	bad := map[string]any{
		"topic":             "prediction market recap",
		"summary":           "A股 600519 建议加入自选池。",
		"conclusion":        "观察 Polymarket 事件，但同时买入相关股票。",
		"facts":             []string{"prediction.market_snapshot provided market state."},
		"inferences":        []string{"manual evidence review remains necessary."},
		"citations":         []string{"prediction.market_snapshot"},
		"watchlist_actions": []map[string]any{},
		"orders":            []map[string]any{},
	}
	err := validateModeratorRecapForMeeting(true)(bad)
	if err == nil || !strings.Contains(err.Error(), "prediction market recap text violates asset boundary") {
		t.Fatalf("expected prediction recap text boundary error, got %v", err)
	}
	if err := validateModeratorRecapForMeeting(false)(bad); err != nil {
		t.Fatalf("A-share meeting validator should not use prediction text boundary: %v", err)
	}
	good := map[string]any{
		"topic":             "prediction market recap",
		"summary":           "The Polymarket market remains an observation candidate.",
		"conclusion":        "Follow event evidence and resolution criteria before changing confidence.",
		"facts":             []string{"prediction.market_snapshot provided market state."},
		"inferences":        []string{"manual evidence review remains necessary."},
		"citations":         []string{"prediction.market_snapshot"},
		"watchlist_actions": []map[string]any{},
		"orders":            []map[string]any{},
	}
	if err := validateModeratorRecapForMeeting(true)(good); err != nil {
		t.Fatalf("valid prediction recap should pass text boundary: %v", err)
	}
	badPredictionWatchlist := map[string]any{
		"topic":             "prediction market recap",
		"summary":           "The Polymarket market remains an observation candidate.",
		"conclusion":        "Follow event evidence and resolution criteria before changing confidence.",
		"facts":             []string{"prediction.market_snapshot provided market state."},
		"inferences":        []string{"manual evidence review remains necessary."},
		"citations":         []string{"prediction.market_snapshot"},
		"orders":            []map[string]any{},
		"watchlist_actions": []map[string]any{},
		"prediction_watchlist_actions": []map[string]any{
			{"market_id": 42, "note": "also add 600519 to 自选池", "code": "600519"},
		},
	}
	if err := validateModeratorRecapForMeeting(true)(badPredictionWatchlist); err == nil || !strings.Contains(err.Error(), "prediction market recap text violates asset boundary") {
		t.Fatalf("expected prediction watchlist action boundary error, got %v", err)
	}
}

func TestTryApplyPredictionMarketRecapBlocksAShareText(t *testing.T) {
	db := newMeetingTestDB(t)
	team := persistmodel.ResearchTeam{Name: "Prediction Team", AssetClass: "prediction_market", Active: true}
	mustMeeting(t, db.Create(&team).Error)
	meeting, err := CreateMeeting(db, "prediction market text boundary", "manual")
	mustMeeting(t, err)
	meeting.ResearchTeamID = team.ID
	mustMeeting(t, db.Model(&domainmeeting.Meeting{}).Where("id = ?", meeting.ID).Update("research_team_id", team.ID).Error)
	role := "moderator"
	recap := `{"topic":"polluted recap","tags":["A股"],"summary":"A股 600519 可以关注。","conclusion":"加入证券自选池。","facts":["prediction.market_snapshot provided the market state."],"inferences":["manual review remains necessary."],"citations":["prediction.market_snapshot"],"watchlist_actions":[],"wake_plans":[],"orders":[]}`
	_, err = AppendEvent(db, meeting.ID, domainkernel.EventRoleMessage, &role, recap, map[string]any{"status": "recap_completed"})
	mustMeeting(t, err)

	_, applied, err := TryApplyModeratorRecapFromEvents(db, meeting)
	if err != nil {
		t.Fatalf("TryApplyModeratorRecapFromEvents: %v", err)
	}
	if applied {
		t.Fatal("polluted prediction recap should not be applied")
	}
	var event domainmeeting.Event
	mustMeeting(t, db.First(&event, "meeting_id = ? AND type = ? AND payload LIKE ?", meeting.ID, domainkernel.EventSystem, "%prediction_market_recap_text_blocked%").Error)
	if !strings.Contains(event.Content, "Prediction market recap text was blocked") {
		t.Fatalf("unexpected block event content: %s", event.Content)
	}
}

func TestModeratorRecapPromptForPredictionMarketForbidsExecutableActions(t *testing.T) {
	db := newMeetingTestDB(t)
	team := persistmodel.ResearchTeam{Name: "Prediction Team", AssetClass: "prediction_market", Active: true}
	mustMeeting(t, db.Create(&team).Error)
	meeting, err := CreateMeeting(db, "prediction market research", "manual")
	mustMeeting(t, err)
	meeting.ResearchTeamID = team.ID
	prompt := moderatorRecapPrompt(db, meeting, domainai.AgentRole{Name: "Moderator"})
	if !strings.Contains(prompt, "Prediction-market meetings are observation-only") {
		t.Fatalf("prediction recap prompt missing observation-only policy: %s", prompt)
	}
	if !strings.Contains(prompt, `"watchlist_actions":[]`) || !strings.Contains(prompt, `"prediction_watchlist_actions"`) || !strings.Contains(prompt, `"orders":[]`) {
		t.Fatalf("prediction recap prompt should require empty executable actions: %s", prompt)
	}
	for _, forbidden := range []string{"indicator wake_plans", "related_symbols", "code, symbol, ticker", "Do not include A-share sectors"} {
		if !strings.Contains(prompt, forbidden) {
			t.Fatalf("prediction recap prompt should explicitly forbid %q in wake plans: %s", forbidden, prompt)
		}
	}
}

func TestManagedPromptsForPredictionMarketAvoidAShareTradingFrame(t *testing.T) {
	db := newMeetingTestDB(t)
	team := persistmodel.ResearchTeam{Name: "Prediction Team", AssetClass: "prediction_market", Active: true}
	mustMeeting(t, db.Create(&team).Error)
	market := persistmodel.PredictionMarket{
		Provider:         "polymarket",
		ExternalMarketID: "m-1",
		Question:         "Will the Fed cut rates in June?",
		Slug:             "fed-cut-june",
		Outcomes:         datatypes.JSON([]byte(`["Yes","No"]`)),
		OutcomePrices:    datatypes.JSON([]byte(`["0.44","0.56"]`)),
		CLOBTokenIDs:     datatypes.JSON([]byte(`["yes-token","no-token"]`)),
		Active:           true,
		EnableOrderBook:  true,
	}
	mustMeeting(t, db.Create(&market).Error)
	meeting, err := CreateMeeting(db, "prediction market research", "manual")
	mustMeeting(t, err)
	meeting.ResearchTeamID = team.ID
	mustMeeting(t, db.Model(&domainmeeting.Meeting{}).Where("id = ?", meeting.ID).Update("research_team_id", team.ID).Error)
	_, err = AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, "context", map[string]any{"status": "meeting_context", "prediction_market_ids": []uint{market.ID}, "related_symbols": []string{"600519"}})
	mustMeeting(t, err)
	role := domainai.AgentRole{
		Key:            "odds_analyst",
		Name:           "赔率盘口分析师",
		Responsibility: "Analyze odds.",
		PromptTemplate: "Use prediction-market evidence.",
		ToolNames:      domainkernel.JSON([]byte(`["prediction.market_snapshot","prediction.orderbook"]`)),
		SkillNames:     domainkernel.JSON([]byte(`["odds-market-analysis"]`)),
	}

	systemPrompt := managedRoleSystemPrompt(role, "analysis", true)
	if !strings.Contains(systemPrompt, "prediction-market research meeting") || !strings.Contains(systemPrompt, "resolution criteria") {
		t.Fatalf("prediction system prompt should focus on prediction-market research: %s", systemPrompt)
	}
	if strings.Contains(systemPrompt, "A-share multi-agent") || strings.Contains(systemPrompt, "A-share research and paper trading only") {
		t.Fatalf("prediction system prompt leaked A-share trading frame: %s", systemPrompt)
	}
	roleContext, _ := buildManagedRoleContext(db, *meeting, role, "analysis", 1, nil, nil)
	if !strings.Contains(roleContext, "Prediction market context") || !strings.Contains(roleContext, "fed-cut-june") {
		t.Fatalf("prediction role context should include linked market evidence: %s", roleContext)
	}
	if strings.Contains(roleContext, "The project targets A-share research and paper trading only") {
		t.Fatalf("prediction role context leaked A-share execution constraints: %s", roleContext)
	}
	if strings.Contains(roleContext, "Related symbols:") {
		t.Fatalf("prediction role context should not expose A-share related symbols: %s", roleContext)
	}
	recapContext := buildRecapContext(db, *meeting)
	if !strings.Contains(recapContext, "Prediction market context") || !strings.Contains(recapContext, "fed-cut-june") {
		t.Fatalf("prediction recap context should include linked market evidence: %s", recapContext)
	}
	if strings.Contains(recapContext, "Paper accounts and positions") || strings.Contains(recapContext, "Paper risk configs") {
		t.Fatalf("prediction recap context leaked A-share paper-trading context: %s", recapContext)
	}
	planPrompt := moderatorPlanSystemPrompt(true, true)
	if !strings.Contains(planPrompt, "prediction-market research meeting") || strings.Contains(planPrompt, "A-share multi-agent") {
		t.Fatalf("prediction moderator plan prompt should not use A-share frame: %s", planPrompt)
	}
	planContext := moderatorPlanContext(db, *meeting, []domainai.AgentRole{role}, []string{"- @odds_analyst: analyze"}, nil, nil, 1, true)
	if strings.Contains(planContext, "Related symbols: 600") {
		t.Fatalf("prediction moderator plan context should not expose A-share symbols: %s", planContext)
	}
	blockedRole := domainai.AgentRole{Key: "bad_tool", Name: "Bad Tool", ToolNames: JSON([]string{"market.realtime_quote"})}
	results := ExecuteRoleTools(db, *meeting, blockedRole)
	if len(results) != 1 || !strings.Contains(results[0].Error, "not available in prediction-market meetings") {
		t.Fatalf("prediction meeting should block A-share market tools, got %+v", results)
	}
}

func TestPredictionMarketRoleTurnValidationRejectsAShareAnalysisAndToolRequests(t *testing.T) {
	validate := validateManagedRoleTurnForMeeting(true)
	badAnalysis := map[string]any{
		"type":        "analysis",
		"content":     "A股 600519 建议买入并加入自选池。",
		"facts":       []string{"600519 came from market.realtime_quote."},
		"inferences":  []string{"buy is reasonable"},
		"citations":   []string{"market.realtime_quote"},
		"confidence":  "medium",
		"related_sym": "ignored",
	}
	if err := validate(badAnalysis); err == nil || !strings.Contains(err.Error(), "prediction market role analysis violates asset boundary") {
		t.Fatalf("expected A-share role analysis boundary error, got %v", err)
	}
	badTool := map[string]any{
		"type":       "tool_request",
		"tool_calls": []map[string]any{{"tool": "market.realtime_quote", "arguments": map[string]any{"code": "600519"}, "reason": "check stock quote"}},
	}
	if err := validate(badTool); err == nil || !strings.Contains(err.Error(), "cannot use A-share code") {
		t.Fatalf("expected A-share tool request boundary error, got %v", err)
	}
	good := map[string]any{
		"type":          "analysis",
		"content":       "The Polymarket market remains an observation candidate.",
		"facts":         []string{"prediction.market_snapshot shows the linked market."},
		"assumptions":   []string{"Resolution source remains unchanged."},
		"inferences":    []string{"The evidence is not strong enough to change confidence."},
		"evidence_gaps": []string{"Need official-source confirmation."},
		"citations":     []string{"prediction.market_snapshot"},
		"confidence":    "medium",
	}
	if err := validate(good); err != nil {
		t.Fatalf("valid prediction role analysis should pass: %v", err)
	}
}

func TestRunMeetingOnceManagedPredictionRoleBlocksAShareAnalysisBeforeRecap(t *testing.T) {
	fastMeetingAITestSettings(t)
	responses := []string{
		`{"content":"Kick off with Polymarket event-fit checks only.","continue_discussion":true,"questions":[{"target":"analyst","question":"Check market fit without A-share framing."}],"focus_roles":["analyst"]}`,
		`{"type":"analysis","content":"A股 600519 建议买入并加入自选池。","facts":["600519 came from market.realtime_quote"],"assumptions":[],"inferences":["buy is reasonable"],"evidence_gaps":[],"citations":["market.realtime_quote"],"confidence":"medium"}`,
		`{"type":"analysis","content":"继续给出 paper order 和 600519 仓位建议。","facts":["600519 still relevant"],"assumptions":[],"inferences":["sell later"],"evidence_gaps":[],"citations":["paper.orders"],"confidence":"medium"}`,
		`{"topic":"Polymarket event follow-up","tags":["prediction"],"summary":"The linked Polymarket market remains an observation candidate.","conclusion":"Continue event-source verification and market-resolution review before changing confidence.","facts":["The analyst turn was blocked by boundary policy."],"assumptions":[],"inferences":["No additional market confidence change is supported yet."],"evidence_gaps":["Need official-source confirmation and settlement-rule review."],"citations":["@analyst"],"watchlist_actions":[],"wake_plans":[],"orders":[]}`,
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
	team := persistmodel.ResearchTeam{Name: "Prediction Team", AssetClass: "prediction_market", Active: true}
	mustMeeting(t, db.Create(&team).Error)
	market := persistmodel.PredictionMarket{
		Provider:         "polymarket",
		ExternalMarketID: "m-1",
		Question:         "Will the Iran peace deal happen?",
		Slug:             "iran-peace-deal",
		Outcomes:         datatypes.JSON([]byte(`["Yes","No"]`)),
		OutcomePrices:    datatypes.JSON([]byte(`["0.44","0.56"]`)),
		CLOBTokenIDs:     datatypes.JSON([]byte(`["yes-token","no-token"]`)),
		Active:           true,
		EnableOrderBook:  true,
	}
	mustMeeting(t, db.Create(&market).Error)
	moderator := domainai.AgentRole{Key: "moderator", Name: "Moderator", Responsibility: "moderate", PromptTemplate: "moderate", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 1}
	analyst := domainai.AgentRole{Key: "analyst", Name: "Analyst", Responsibility: "analyze prediction market evidence", PromptTemplate: "Use only prediction-market evidence.", ProviderID: &providerID, Model: &model, Enabled: true, SortOrder: 2}
	mustMeeting(t, saveTestAgentRole(db, team.ID, &moderator))
	mustMeeting(t, saveTestAgentRole(db, team.ID, &analyst))
	mustMeeting(t, db.Create(&domainsettings.AppSetting{Key: "MEETING_MAX_ROUNDS", Value: JSON(map[string]any{"value": 1})}).Error)
	meeting, err := CreateMeeting(db, "Polymarket Iran peace deal", "manual")
	mustMeeting(t, err)
	meeting.ResearchTeamID = team.ID
	mustMeeting(t, db.Model(&domainmeeting.Meeting{}).Where("id = ?", meeting.ID).Update("research_team_id", team.ID).Error)
	_, err = AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, "prediction context", map[string]any{"status": "meeting_context", "prediction_market_ids": []uint{market.ID}, "related_symbols": []string{"600519"}})
	mustMeeting(t, err)

	mustMeeting(t, RunMeetingOnce(db, meeting.ID))
	if calls != len(responses) {
		t.Fatalf("expected %d model calls, got %d", len(responses), calls)
	}
	recapMessages := requestMessages(t, requestPayloads[len(requestPayloads)-1])
	if strings.Contains(recapMessages[1]["content"], "600519") || strings.Contains(recapMessages[1]["content"], "A股") || strings.Contains(recapMessages[1]["content"], "paper order") {
		t.Fatalf("recap context should not include blocked A-share role text: %s", recapMessages[1]["content"])
	}
	var blockedEvent domainmeeting.Event
	mustMeeting(t, db.First(&blockedEvent, "meeting_id = ? AND role_key = ? AND type = ? AND payload LIKE ?", meeting.ID, "analyst", domainkernel.EventRoleMessage, "%prediction_market_role_analysis_blocked%").Error)
	if strings.Contains(blockedEvent.Content, "600519") || strings.Contains(string(blockedEvent.Payload), "600519") {
		t.Fatalf("blocked role event leaked polluted text: content=%s payload=%s", blockedEvent.Content, blockedEvent.Payload)
	}
	var completed domainmeeting.Meeting
	mustMeeting(t, db.First(&completed, meeting.ID).Error)
	if completed.Status != domainkernel.MeetingCompleted || completed.Conclusion == nil || strings.Contains(*completed.Conclusion, "600519") || strings.Contains(*completed.Conclusion, "A股") {
		t.Fatalf("prediction meeting conclusion leaked A-share content: %+v", completed)
	}
}

func TestMixedTeamMeetingWithPredictionMarketContextUsesPredictionBoundary(t *testing.T) {
	db := newMeetingTestDB(t)
	team := persistmodel.ResearchTeam{Name: "Mixed Team", AssetClass: "mixed", Active: true}
	mustMeeting(t, db.Create(&team).Error)
	market := persistmodel.PredictionMarket{
		Provider:         "polymarket",
		ExternalMarketID: "m-1",
		Question:         "Will the Iran peace deal happen?",
		Slug:             "iran-peace-deal",
		Outcomes:         datatypes.JSON([]byte(`["Yes","No"]`)),
		OutcomePrices:    datatypes.JSON([]byte(`["0.44","0.56"]`)),
		CLOBTokenIDs:     datatypes.JSON([]byte(`["yes-token","no-token"]`)),
		Active:           true,
		EnableOrderBook:  true,
	}
	mustMeeting(t, db.Create(&market).Error)
	meeting, err := CreateMeeting(db, "mixed team prediction market research", "manual")
	mustMeeting(t, err)
	meeting.ResearchTeamID = team.ID
	mustMeeting(t, db.Model(&domainmeeting.Meeting{}).Where("id = ?", meeting.ID).Update("research_team_id", team.ID).Error)
	_, err = AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, "context", map[string]any{"status": "meeting_context", "prediction_market_ids": []uint{market.ID}, "related_symbols": []string{"600519"}})
	mustMeeting(t, err)
	role := domainai.AgentRole{
		Key:            "odds_analyst",
		Name:           "赔率盘口分析师",
		Responsibility: "Analyze odds.",
		PromptTemplate: "Use prediction-market evidence.",
		ToolNames:      domainkernel.JSON([]byte(`["prediction.market_snapshot","market.realtime_quote"]`)),
		SkillNames:     domainkernel.JSON([]byte(`["odds-market-analysis"]`)),
	}

	if !isPredictionMarketMeeting(db, meeting) {
		t.Fatal("mixed team meeting with prediction market ids should use prediction boundary")
	}
	systemPrompt := managedRoleSystemPrompt(role, "analysis", isPredictionMarketMeeting(db, meeting))
	if !strings.Contains(systemPrompt, "prediction-market research meeting") || strings.Contains(systemPrompt, "A-share multi-agent") {
		t.Fatalf("mixed prediction meeting should use prediction prompt: %s", systemPrompt)
	}
	roleContext, _ := buildManagedRoleContext(db, *meeting, role, "analysis", 1, nil, nil)
	if !strings.Contains(roleContext, "Prediction market context") || strings.Contains(roleContext, "Related symbols:") || strings.Contains(roleContext, "Paper accounts and positions") || strings.Contains(roleContext, "market.realtime_quote") {
		t.Fatalf("mixed prediction role context leaked non-prediction context: %s", roleContext)
	}
	results := ExecuteRoleTools(db, *meeting, role)
	blocked := false
	for _, result := range results {
		if result.Name == "market.realtime_quote" && strings.Contains(result.Error, "not available in prediction-market meetings") {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("mixed prediction meeting should block A-share market tool, got %+v", results)
	}
}

func TestSearchWebForPredictionMeetingUsesGlobalNewsLocale(t *testing.T) {
	db := newMeetingTestDB(t)
	team := persistmodel.ResearchTeam{Name: "Prediction Team", AssetClass: "prediction_market", Active: true}
	mustMeeting(t, db.Create(&team).Error)
	meeting, err := CreateMeeting(db, "US Iran peace deal", "manual")
	mustMeeting(t, err)
	meeting.ResearchTeamID = team.ID
	mustMeeting(t, db.Model(&domainmeeting.Meeting{}).Where("id = ?", meeting.ID).Update("research_team_id", team.ID).Error)
	_, err = AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, "legacy context", map[string]any{"status": "meeting_context", "related_symbols": []string{"600519"}})
	mustMeeting(t, err)

	var gotQuery, gotHL, gotGL, gotCEID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		gotHL = r.URL.Query().Get("hl")
		gotGL = r.URL.Query().Get("gl")
		gotCEID = r.URL.Query().Get("ceid")
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<rss><channel><item><title>Official source</title><link>https://example.com/source</link></item></channel></rss>`))
	}))
	defer server.Close()
	oldBaseURL := webSearchBaseURL
	webSearchBaseURL = server.URL
	defer func() { webSearchBaseURL = oldBaseURL }()

	rows, args, err := ExecuteMeetingToolWithArgs(db, *meeting, "web.search", map[string]any{"limit": 1})
	mustMeeting(t, err)
	if len(rows) != 1 || rows[0]["title"] != "Official source" {
		t.Fatalf("unexpected search rows: %+v", rows)
	}
	if strings.Contains(gotQuery, "600519") || strings.Contains(stringFromAny(args["query"]), "600519") {
		t.Fatalf("prediction web search should not prepend A-share symbols, query=%q args=%+v", gotQuery, args)
	}
	if gotHL != "en-US" || gotGL != "US" || gotCEID != "US:en" {
		t.Fatalf("prediction web search locale = hl=%s gl=%s ceid=%s args=%+v", gotHL, gotGL, gotCEID, args)
	}
}

func TestPredictionMeetingReferencesRedactNonPredictionSnapshots(t *testing.T) {
	db := newMeetingTestDB(t)
	team := persistmodel.ResearchTeam{Name: "Prediction Team", AssetClass: "prediction_market", Active: true}
	aShareTeam := persistmodel.ResearchTeam{Name: "A-share Team", AssetClass: "ashare", Active: true}
	mixedTeam := persistmodel.ResearchTeam{Name: "Mixed Team", AssetClass: "mixed", Active: true}
	mustMeeting(t, db.Create(&team).Error)
	mustMeeting(t, db.Create(&aShareTeam).Error)
	mustMeeting(t, db.Create(&mixedTeam).Error)
	source, err := CreateMeeting(db, "Iran prediction research", "manual")
	mustMeeting(t, err)
	source.ResearchTeamID = team.ID
	mustMeeting(t, db.Model(&domainmeeting.Meeting{}).Where("id = ?", source.ID).Update("research_team_id", team.ID).Error)
	ashareSummary := "A股 600519 买入并加入证券自选池"
	ashareTarget := persistmodel.Meeting{ResearchTeamID: aShareTeam.ID, Topic: "A股 600519 research", Status: domainkernel.MeetingCompleted, Summary: &ashareSummary, Conclusion: &ashareSummary, Tags: datatypes.JSON([]byte(`["A股"]`))}
	predictionSummary := "Polymarket peace deal resolution criteria reviewed"
	predictionTarget := persistmodel.Meeting{ResearchTeamID: mixedTeam.ID, Topic: "Polymarket peace deal reference", Status: domainkernel.MeetingCompleted, Summary: &predictionSummary, Conclusion: &predictionSummary, Tags: datatypes.JSON([]byte(`["prediction"]`))}
	mustMeeting(t, db.Create(&ashareTarget).Error)
	mustMeeting(t, db.Create(&predictionTarget).Error)
	predictionMarket := persistmodel.PredictionMarket{Provider: "polymarket", ExternalMarketID: "m-1", Question: "Will there be a peace deal?", Slug: "peace-deal", Active: true}
	mustMeeting(t, db.Create(&predictionMarket).Error)
	_, err = AppendEvent(db, predictionTarget.ID, domainkernel.EventSystem, nil, "prediction context", map[string]any{"status": "meeting_context", "prediction_market_ids": []uint{predictionMarket.ID}})
	mustMeeting(t, err)
	ashareNote := "A股 note mentions 600519"
	predictionNote := "Prediction reference"
	mustMeeting(t, db.Create(&persistmodel.MeetingReference{SourceMeetingID: source.ID, TargetMeetingID: &ashareTarget.ID, ReferenceType: "meeting", Note: &ashareNote, TargetTopicSnapshot: ashareTarget.Topic, TargetSummarySnapshot: &ashareSummary}).Error)
	mustMeeting(t, db.Create(&persistmodel.MeetingReference{SourceMeetingID: source.ID, TargetMeetingID: &predictionTarget.ID, ReferenceType: "meeting", Note: &predictionNote, TargetTopicSnapshot: predictionTarget.Topic, TargetSummarySnapshot: &predictionSummary}).Error)

	rows, _, err := ExecuteMeetingToolWithArgs(db, *source, "meeting.references", nil)
	mustMeeting(t, err)
	if len(rows) != 2 {
		t.Fatalf("expected two reference rows, got %+v", rows)
	}
	redacted := rows[0]
	if redacted["redacted"] != true || redacted["note"] != nil || redacted["target_topic_snapshot"] != "" || redacted["target_summary_snapshot"] != nil {
		t.Fatalf("A-share reference should be redacted in prediction meeting, got %+v", redacted)
	}
	kept := rows[1]
	if kept["redacted"] == true || stringFromAny(kept["target_topic_snapshot"]) != predictionTarget.Topic || stringFromAny(kept["target_summary_snapshot"]) != predictionSummary {
		t.Fatalf("prediction reference should remain visible, got %+v", kept)
	}
	recapContext := buildRecapContext(db, *source)
	if strings.Contains(recapContext, "600519") || strings.Contains(recapContext, "A股") || strings.Contains(recapContext, "买入") {
		t.Fatalf("prediction recap context leaked non-prediction reference text: %s", recapContext)
	}
	if !strings.Contains(recapContext, predictionTarget.Topic) || !strings.Contains(recapContext, "non_prediction_reference") {
		t.Fatalf("prediction recap context should keep prediction references and mark redaction: %s", recapContext)
	}
}

func TestPredictionMeetingBlocksUnknownMeetingNamespaceTools(t *testing.T) {
	db := newMeetingTestDB(t)
	team := persistmodel.ResearchTeam{Name: "Prediction Team", AssetClass: "prediction_market", Active: true}
	mustMeeting(t, db.Create(&team).Error)
	meeting, err := CreateMeeting(db, "Prediction meeting", "manual")
	mustMeeting(t, err)
	meeting.ResearchTeamID = team.ID
	mustMeeting(t, db.Model(&domainmeeting.Meeting{}).Where("id = ?", meeting.ID).Update("research_team_id", team.ID).Error)

	_, _, err = ExecuteMeetingToolWithArgs(db, *meeting, "meeting.future_stock_context", nil)
	if err == nil || !strings.Contains(err.Error(), "not available in prediction-market meetings") {
		t.Fatalf("prediction meeting should block non-allowlisted meeting.* tool, got %v", err)
	}
}

func TestPredictionSearchToolPersistsAndReturnsLocalMarketID(t *testing.T) {
	db := newMeetingTestDB(t)
	oldProvider := predictionProviderForTool
	predictionProviderForTool = func(*gorm.DB) appprediction.Provider {
		return fakeMeetingPredictionProvider{search: appprediction.SearchResult{
			Events: []domainprediction.Event{{
				Provider:        domainprediction.ProviderPolymarket,
				ExternalEventID: "event-iran",
				Slug:            "us-x-iran-permanent-peace-deal-by",
				Title:           "US x Iran permanent peace deal by 2026?",
				Active:          true,
			}},
			Markets: []domainprediction.Market{{
				Provider:         domainprediction.ProviderPolymarket,
				ExternalMarketID: "1919417",
				ConditionID:      "cond-iran",
				Question:         "US x Iran permanent peace deal by 2026?",
				Slug:             "us-x-iran-permanent-peace-deal-by",
				Active:           true,
				EnableOrderBook:  true,
				CLOBTokenIDs:     domainkernel.NewJSON([]string{"token-yes", "token-no"}),
				Raw:              domainkernel.NewJSON(map[string]any{"event_id": "event-iran"}),
			}},
		}}
	}
	defer func() { predictionProviderForTool = oldProvider }()

	inputURL := "https://polymarket.com/events/US-X-Iran-Permanent-Peace-Deal-By#qTlpdC7"
	rows, args, err := predictionSearchForTool(db, inputURL, 5)
	if err != nil {
		t.Fatalf("predictionSearchForTool: %v", err)
	}
	if args["query"] != "us-x-iran-permanent-peace-deal-by" {
		t.Fatalf("tool query should be normalized, args=%+v", args)
	}
	if args["original_query"] != inputURL || args["normalized_query"] != "us-x-iran-permanent-peace-deal-by" {
		t.Fatalf("tool args should preserve original and normalized query, args=%+v", args)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one search row, got %+v", rows)
	}
	marketID := uintFromAny(rows[0]["id"])
	if marketID == 0 {
		t.Fatalf("search tool should return persisted local market id, got %+v", rows[0])
	}
	if rows[0]["event_slug"] != "us-x-iran-permanent-peace-deal-by" || rows[0]["event_title"] != "US x Iran permanent peace deal by 2026?" || rows[0]["external_event_id"] != "event-iran" {
		t.Fatalf("search tool should expose readable event identity, got %+v", rows[0])
	}
	snapshots := predictionMarketSnapshotsForTool(db, []uint{marketID}, 10)
	if len(snapshots) != 1 || uintFromAny(snapshots[0]["id"]) != marketID {
		t.Fatalf("persisted search result should be usable by snapshot tool, got %+v", snapshots)
	}
	if snapshots[0]["event_slug"] != "us-x-iran-permanent-peace-deal-by" || snapshots[0]["event_title"] != "US x Iran permanent peace deal by 2026?" {
		t.Fatalf("snapshot should preserve readable event identity from search, got %+v", snapshots[0])
	}
}

func TestPredictionWatchlistToolReturnsTeamScopedMarketSnapshots(t *testing.T) {
	db := newMeetingTestDB(t)
	team := persistmodel.ResearchTeam{Name: "Prediction Team", AssetClass: "prediction_market", Active: true}
	otherTeam := persistmodel.ResearchTeam{Name: "Other Prediction Team", AssetClass: "prediction_market", Active: true}
	mustMeeting(t, db.Create(&team).Error)
	mustMeeting(t, db.Create(&otherTeam).Error)
	event := persistmodel.PredictionEvent{
		Provider:        "polymarket",
		ExternalEventID: "event-iran",
		Slug:            "us-x-iran-permanent-peace-deal-by",
		Title:           "US x Iran permanent peace deal by 2026?",
		Active:          true,
	}
	mustMeeting(t, db.Create(&event).Error)
	market := persistmodel.PredictionMarket{
		EventID:          &event.ID,
		Provider:         "polymarket",
		ExternalMarketID: "m-iran",
		ConditionID:      "cond-iran",
		Question:         "US x Iran permanent peace deal by June 30, 2026?",
		Slug:             "us-x-iran-permanent-peace-deal-by-june-30-2026",
		Outcomes:         datatypes.JSON([]byte(`["Yes","No"]`)),
		OutcomePrices:    datatypes.JSON([]byte(`["0.41","0.59"]`)),
		CLOBTokenIDs:     datatypes.JSON([]byte(`["yes-token","no-token"]`)),
		Active:           true,
		EnableOrderBook:  true,
	}
	otherMarket := market
	otherMarket.ID = 0
	otherMarket.ExternalMarketID = "m-other"
	otherMarket.Question = "Other market"
	otherMarket.Slug = "other-market"
	mustMeeting(t, db.Create(&market).Error)
	mustMeeting(t, db.Create(&otherMarket).Error)
	note := "Track settlement evidence and liquidity."
	mustMeeting(t, db.Create(&persistmodel.PredictionWatchlistItem{ResearchTeamID: team.ID, MarketID: market.ID, Note: &note, Active: true}).Error)
	mustMeeting(t, db.Create(&persistmodel.PredictionWatchlistItem{ResearchTeamID: otherTeam.ID, MarketID: otherMarket.ID, Active: true}).Error)
	meeting, err := CreateMeeting(db, "prediction watchlist read", "manual")
	mustMeeting(t, err)
	meeting.ResearchTeamID = team.ID
	mustMeeting(t, db.Model(&domainmeeting.Meeting{}).Where("id = ?", meeting.ID).Update("research_team_id", team.ID).Error)

	rows, args, err := ExecuteMeetingToolWithArgs(db, *meeting, "prediction.watchlist", map[string]any{"limit": 10})
	mustMeeting(t, err)
	if args["researchTeamId"] != team.ID {
		t.Fatalf("tool should be scoped to meeting team, args=%+v", args)
	}
	if len(rows) != 1 || uintFromAny(rows[0]["market_id"]) != market.ID {
		t.Fatalf("expected one team-scoped watchlist row, got %+v", rows)
	}
	if rows[0]["note"] != note || rows[0]["active"] != true {
		t.Fatalf("watchlist metadata missing, got %+v", rows[0])
	}
	marketSnapshot, ok := rows[0]["market"].(map[string]any)
	if !ok {
		t.Fatalf("watchlist row should include market snapshot, got %+v", rows[0])
	}
	if marketSnapshot["event_slug"] != event.Slug || marketSnapshot["event_title"] != event.Title || marketSnapshot["external_event_id"] != event.ExternalEventID {
		t.Fatalf("watchlist market should expose readable event identity, got %+v", marketSnapshot)
	}
	if uintFromAny(marketSnapshot["id"]) != market.ID || marketSnapshot["question"] != market.Question {
		t.Fatalf("watchlist market snapshot mismatch, got %+v", marketSnapshot)
	}
}

func TestPredictionToolsExposeOutcomeTokenMapping(t *testing.T) {
	db := newMeetingTestDB(t)
	oldProvider := predictionProviderForTool
	predictionProviderForTool = func(*gorm.DB) appprediction.Provider {
		return fakeMeetingPredictionProvider{
			orderbook: map[string]any{"best_bid": "0.42", "best_ask": "0.45"},
			history:   []map[string]any{{"t": float64(1710000000), "p": "0.44"}},
		}
	}
	defer func() { predictionProviderForTool = oldProvider }()
	event := persistmodel.PredictionEvent{
		Provider:        "polymarket",
		ExternalEventID: "event-iran",
		Slug:            "us-x-iran-permanent-peace-deal-by",
		Title:           "US x Iran permanent peace deal by 2026?",
		Active:          true,
	}
	mustMeeting(t, db.Create(&event).Error)
	market := persistmodel.PredictionMarket{
		EventID:          &event.ID,
		Provider:         "polymarket",
		ExternalMarketID: "m-yes-no",
		Question:         "Will the event happen?",
		Outcomes:         datatypes.JSON([]byte(`["Yes","No"]`)),
		OutcomePrices:    datatypes.JSON([]byte(`["0.44","0.56"]`)),
		CLOBTokenIDs:     datatypes.JSON([]byte(`["yes-token","no-token"]`)),
		Active:           true,
		EnableOrderBook:  true,
	}
	mustMeeting(t, db.Create(&market).Error)

	snapshots := predictionMarketSnapshotsForTool(db, []uint{market.ID}, 10)
	if len(snapshots) != 1 {
		t.Fatalf("expected one snapshot, got %+v", snapshots)
	}
	if uintFromAny(snapshots[0]["event_id"]) != event.ID {
		t.Fatalf("snapshot should expose local event id, got %+v", snapshots[0])
	}
	if snapshots[0]["event_slug"] != event.Slug || snapshots[0]["event_title"] != event.Title || snapshots[0]["external_event_id"] != event.ExternalEventID {
		t.Fatalf("snapshot should expose readable event identity, got %+v", snapshots[0])
	}
	outcomeTokens, ok := snapshots[0]["outcome_tokens"].([]map[string]any)
	if !ok || len(outcomeTokens) != 2 || outcomeTokens[0]["outcome"] != "Yes" || outcomeTokens[0]["token_id"] != "yes-token" || outcomeTokens[1]["outcome"] != "No" || outcomeTokens[1]["token_id"] != "no-token" {
		t.Fatalf("snapshot should expose outcome-token mapping, got %+v", snapshots[0]["outcome_tokens"])
	}

	orderRows, orderArgs, err := predictionOrderbookForTool(db, market.ID, "no-token")
	mustMeeting(t, err)
	if len(orderRows) != 1 || orderRows[0]["outcome"] != "No" || orderRows[0]["outcome_price"] != "0.56" || orderRows[0]["outcome_index"] != 1 {
		t.Fatalf("orderbook should identify selected outcome, rows=%+v args=%+v", orderRows, orderArgs)
	}
	if uintFromAny(orderRows[0]["event_id"]) != event.ID {
		t.Fatalf("orderbook should expose local event id, rows=%+v", orderRows)
	}
	if orderRows[0]["event_slug"] != event.Slug || orderRows[0]["event_title"] != event.Title {
		t.Fatalf("orderbook should expose readable event identity, rows=%+v", orderRows)
	}
	if orderArgs["outcome"] != "No" || orderArgs["outcome_price"] != "0.56" {
		t.Fatalf("orderbook args should carry selected outcome metadata: %+v", orderArgs)
	}
	historyRows, historyArgs, err := predictionPriceHistoryForTool(db, market.ID, "")
	mustMeeting(t, err)
	if len(historyRows) != 1 || historyRows[0]["token_id"] != "yes-token" || historyRows[0]["outcome"] != "Yes" || historyArgs["outcome"] != "Yes" {
		t.Fatalf("price history should default to first token and label outcome, rows=%+v args=%+v", historyRows, historyArgs)
	}
	if uintFromAny(historyRows[0]["event_id"]) != event.ID {
		t.Fatalf("price history should expose local event id, rows=%+v", historyRows)
	}
	if historyRows[0]["event_slug"] != event.Slug || historyRows[0]["event_title"] != event.Title {
		t.Fatalf("price history should expose readable event identity, rows=%+v", historyRows)
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
		"facts":["000001 was discussed in the meeting"],
		"inferences":["tracking and a pilot order are justified"],
		"citations":["@moderator","meeting.context"],
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
		`{"type":"analysis","content":"quote reviewed","facts":["600519 latest quote came from market.realtime_quote"],"assumptions":["liquidity remains normal"],"inferences":["quote supports continued watchlist tracking"],"evidence_gaps":["need next trading day volume confirmation"],"questions":[{"target":"all","question":"any risk?"}],"mentions":["moderator"],"citations":["market.realtime_quote"],"confidence":"high"}`,
		`{"content":"enough","continue_discussion":false,"questions":[],"focus_roles":[]}`,
		`{"topic":"final 600519","tags":["quote"],"summary":"managed summary","conclusion":"managed conclusion","facts":["600519 quote came from market.realtime_quote"],"assumptions":["liquidity remains normal"],"inferences":["quote supports continued watchlist tracking"],"evidence_gaps":["need next trading day volume confirmation"],"citations":["@analyst","market.realtime_quote"],"watchlist_actions":[{"code":"600519","note":"track quote","active":true}],"wake_plans":[],"orders":[]}`,
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
	if !strings.Contains(string(analystEvent.Payload), `"confidence":"high"`) || !strings.Contains(string(analystEvent.Payload), "market.realtime_quote") || !strings.Contains(string(analystEvent.Payload), "liquidity remains normal") || !strings.Contains(string(analystEvent.Payload), "need next trading day volume confirmation") {
		t.Fatalf("analyst payload missing metadata: %s", analystEvent.Payload)
	}
	if !strings.Contains(string(analystEvent.Payload), `"prompt_snapshot"`) || !strings.Contains(string(analystEvent.Payload), managedMeetingPromptVersion) || !strings.Contains(string(analystEvent.Payload), `"promptHash"`) {
		t.Fatalf("analyst payload missing prompt snapshot: %s", analystEvent.Payload)
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
		`{"topic":"final","tags":["etf"],"summary":"summary","conclusion":"watch trigger","facts":["510300 was discussed as the target instrument"],"assumptions":[],"inferences":["price 4.25 is the follow-up threshold"],"evidence_gaps":[],"citations":["@analyst"],"watchlist_actions":[],"wake_plans":[{"trigger_type":"indicator","reason":"price trigger","trigger_config":{"threshold":4.25}}],"orders":[]}`,
		`{"topic":"final","tags":["etf"],"summary":"summary","conclusion":"watch trigger","facts":["510300 was discussed as the target instrument"],"assumptions":[],"inferences":["price 4.25 is the follow-up threshold"],"evidence_gaps":[],"citations":["@analyst","meeting.context"],"watchlist_actions":[],"wake_plans":[{"trigger_type":"indicator","reason":"price trigger","trigger_config":{"code":"510300","threshold":4.25}}],"orders":[]}`,
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

func TestRunMeetingOnceManagedRunnerRetriesActionRecapWithoutEvidence(t *testing.T) {
	fastMeetingAITestSettings(t)
	responses := []string{
		`{"content":"kickoff","continue_discussion":true,"questions":[{"target":"analyst","question":"answer"}]}`,
		`{"type":"analysis","content":"Track 600519 after quote review.","facts":["600519 was reviewed"],"assumptions":[],"inferences":["tracking is reasonable"],"evidence_gaps":[],"mentions":["moderator"],"citations":["market.realtime_quote"],"confidence":"medium"}`,
		`{"topic":"final","tags":["quote"],"summary":"summary","conclusion":"track 600519","watchlist_actions":[{"code":"600519","note":"track","active":true}],"wake_plans":[],"orders":[]}`,
		`{"topic":"final","tags":["quote"],"summary":"summary","conclusion":"track 600519","facts":["600519 was reviewed by analyst"],"assumptions":[],"inferences":["watchlist tracking is reasonable"],"evidence_gaps":[],"citations":["@analyst","market.realtime_quote"],"watchlist_actions":[{"code":"600519","note":"track","active":true}],"wake_plans":[],"orders":[]}`,
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
	meeting, err := CreateMeeting(db, "600519 evidence gated action", "manual")
	mustMeeting(t, err)

	mustMeeting(t, RunMeetingOnce(db, meeting.ID))
	if calls != len(responses) {
		t.Fatalf("expected %d model calls, got %d", len(responses), calls)
	}
	retryMessages := requestMessages(t, requestPayloads[3])
	if len(retryMessages) != 3 || !strings.Contains(retryMessages[2]["content"], "executable actions require at least one structured fact or inference") || !strings.Contains(retryMessages[2]["content"], "include facts or inferences plus citations") {
		t.Fatalf("action evidence retry payload mismatch: %+v", retryMessages)
	}
	var watchlist domainmarket.WatchlistItem
	mustMeeting(t, db.First(&watchlist, "research_team_id = ? AND code = ?", teamID, "600519").Error)
	var recapEvent domainmeeting.Event
	mustMeeting(t, db.First(&recapEvent, "meeting_id = ? AND role_key = ? AND type = ? AND payload LIKE ?", meeting.ID, "moderator", domainkernel.EventRoleMessage, "%recap_completed%").Error)
	var payload map[string]any
	mustMeeting(t, json.Unmarshal(recapEvent.Payload, &payload))
	rawRecap, err := extractMeetingRecapJSON(stringFromAny(payload["raw_json"]))
	mustMeeting(t, err)
	if got := stringList(rawRecap["citations"]); len(got) != 2 || got[0] != "@analyst" || got[1] != "market.realtime_quote" {
		t.Fatalf("recap citations missing: %+v payload=%s", rawRecap, recapEvent.Payload)
	}
	if got := stringList(rawRecap["facts"]); len(got) != 1 || got[0] != "600519 was reviewed by analyst" {
		t.Fatalf("recap facts missing: %+v payload=%s", rawRecap, recapEvent.Payload)
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

func meetingTestPayload(t *testing.T, event domainmeeting.Event) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func assertRecapActionSuggestion(t *testing.T, payload map[string]any, index int, actionType string, disposition string) {
	t.Helper()
	raw, ok := payload["suggested_actions"].([]any)
	if !ok || index < 0 || index >= len(raw) {
		t.Fatalf("missing suggested action %d: %+v", index, payload)
	}
	suggestion, ok := raw[index].(map[string]any)
	if !ok {
		t.Fatalf("unexpected suggested action %d: %+v", index, raw[index])
	}
	if suggestion["action_type"] != actionType || suggestion["disposition"] != disposition {
		t.Fatalf("suggested action %d mismatch: %+v", index, suggestion)
	}
	if spec, ok := suggestion["spec"].(map[string]any); !ok || len(spec) == 0 {
		t.Fatalf("suggested action %d missing spec: %+v", index, suggestion)
	}
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

func TestAppendPredictionRealtimeSnapshotEventRecordsSkippedWhenTokensMissing(t *testing.T) {
	db := newMeetingTestDB(t)
	meeting, err := CreateMeeting(db, "prediction market event", "manual")
	mustMeeting(t, err)
	market := persistmodel.PredictionMarket{
		Provider:         "polymarket",
		ExternalMarketID: "mkt-no-token",
		Question:         "Will the event resolve yes?",
		Outcomes:         datatypes.JSON([]byte(`["Yes","No"]`)),
		CLOBTokenIDs:     datatypes.JSON([]byte(`[]`)),
		Active:           true,
	}
	mustMeeting(t, db.Create(&market).Error)
	externalRef := fmt.Sprintf("prediction_market:%d", market.ID)
	ref := domainmeeting.Reference{SourceMeetingID: meeting.ID, ReferenceType: "prediction_market", ExternalRef: &externalRef}
	mustMeeting(t, db.Create(&ref).Error)

	appendPredictionRealtimeSnapshotEvent(context.Background(), db, meeting.ID)

	var event domainmeeting.Event
	mustMeeting(t, db.First(&event, "meeting_id = ? AND payload LIKE ?", meeting.ID, "%prediction_realtime_snapshot%").Error)
	var payload map[string]any
	mustMeeting(t, json.Unmarshal(event.Payload, &payload))
	if payload["source"] != "skipped" || !strings.Contains(fmt.Sprint(payload["error"]), "clob token ids") {
		t.Fatalf("unexpected realtime skipped payload: %+v", payload)
	}
}

func TestPredictionRealtimeTokenIDsEnrichesMarketMetadata(t *testing.T) {
	markets := []persistmodel.PredictionMarket{
		{
			ID:               7,
			ExternalMarketID: "mkt-7",
			ConditionID:      "cond-7",
			Question:         "Will the Fed cut rates?",
			Slug:             "fed-cut",
			Outcomes:         datatypes.JSON([]byte(`["Yes","No"]`)),
			CLOBTokenIDs:     datatypes.JSON([]byte(`["yes-token","no-token"]`)),
		},
	}

	tokenIDs, byToken := predictionRealtimeTokenIDs(markets)
	if len(tokenIDs) != 2 || tokenIDs[0] != "yes-token" || tokenIDs[1] != "no-token" {
		t.Fatalf("unexpected token IDs: %v", tokenIDs)
	}
	rows := enrichPredictionRealtimeRows([]map[string]any{{"event_type": "best_bid_ask", "asset_id": "yes-token"}}, byToken)
	if len(rows) != 1 || rows[0]["market_id"] != uint(7) || rows[0]["outcome"] != "Yes" || rows[0]["question"] == "" {
		t.Fatalf("unexpected enriched row: %#v", rows)
	}
}

type fakeMeetingPredictionProvider struct {
	search     appprediction.SearchResult
	searchErr  error
	orderbook  map[string]any
	history    []map[string]any
	orderErr   error
	historyErr error
}

func (f fakeMeetingPredictionProvider) Search(context.Context, string, int) (appprediction.SearchResult, error) {
	return f.search, f.searchErr
}

func (fakeMeetingPredictionProvider) SyncActive(context.Context, int) (appprediction.SearchResult, error) {
	return appprediction.SearchResult{}, nil
}

func (f fakeMeetingPredictionProvider) OrderBook(context.Context, string) (map[string]any, error) {
	return f.orderbook, f.orderErr
}

func (f fakeMeetingPredictionProvider) PriceHistory(context.Context, string) ([]map[string]any, error) {
	return f.history, f.historyErr
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
