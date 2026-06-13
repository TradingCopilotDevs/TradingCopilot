package meeting

import (
	"fmt"
	"strings"
	"testing"
	"time"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
)

func TestRunStartPlanAppliesMeetingRunState(t *testing.T) {
	now := time.Date(2026, 6, 9, 10, 30, 0, 123, time.UTC)
	plan := NewRunStartPlan(42, "", now)
	if plan.Runner != DefaultRunnerName || plan.RunID != fmt.Sprintf("go-42-%d", now.UnixNano()) {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	meeting := domainmeeting.Meeting{ID: 42, Status: domainkernel.MeetingQueued, RunAttempt: 2}
	ApplyRunStart(&meeting, plan)
	if meeting.Status != domainkernel.MeetingRunning || meeting.RunID == nil || *meeting.RunID != plan.RunID {
		t.Fatalf("meeting run state mismatch: %+v", meeting)
	}
	if meeting.StartedAt == nil || !meeting.StartedAt.Equal(now) || meeting.HeartbeatAt == nil || !meeting.HeartbeatAt.Equal(now) {
		t.Fatalf("meeting times mismatch: %+v", meeting)
	}
	if meeting.RunAttempt != 3 {
		t.Fatalf("run attempt = %d, want 3", meeting.RunAttempt)
	}
	payload := RunStartedEventPayload(plan)
	if payload["status"] != "running" || payload["runner"] != "go" || payload["run_id"] != plan.RunID {
		t.Fatalf("payload mismatch: %+v", payload)
	}
}

func TestMeetingRunPoliciesExposeTerminalAndLegacyDefaults(t *testing.T) {
	for _, status := range []domainkernel.MeetingStatus{domainkernel.MeetingCompleted, domainkernel.MeetingFailed, domainkernel.MeetingCancelled} {
		if !IsTerminalStatus(status) {
			t.Fatalf("status %s should be terminal", status)
		}
	}
	if IsTerminalStatus(domainkernel.MeetingRunning) || IsTerminalStatus(domainkernel.MeetingQueued) {
		t.Fatal("running and queued statuses should not be terminal")
	}
	defaults := LegacyCompletionDefaults()
	if defaults.Summary == "" || defaults.Conclusion == "" {
		t.Fatalf("legacy defaults should be populated: %+v", defaults)
	}
}

func TestBuildRoleTurnResultSanitizesStructuredClaims(t *testing.T) {
	validRoles := map[string]struct{}{"analyst": {}, "risk": {}}
	result := BuildRoleTurnResult("Analyst", `{"content":"body"}`, map[string]any{
		"content":       " body ",
		"confidence":    "unknown",
		"questions":     []any{map[string]any{"target": "@risk", "question": "confirm liquidity"}, map[string]any{"target": "ghost", "question": "ignored target falls back"}},
		"mentions":      []any{"@risk", "ghost", "@risk"},
		"citations":     []any{"market.realtime_quote", "market.realtime_quote"},
		"facts":         []any{"quote came from market.realtime_quote", map[string]any{"claim": "volume was present"}},
		"assumptions":   []any{"normal liquidity"},
		"inferences":    []any{"pilot order may be executable"},
		"evidence_gaps": []any{"next session volume confirmation"},
	}, validRoles)

	if result.Content != "body" || result.Confidence != "medium" || result.RawJSON == nil {
		t.Fatalf("basic result fields mismatch: %+v", result)
	}
	if len(result.Questions) != 2 || result.Questions[0]["target"] != "risk" || result.Questions[1]["target"] != "all" {
		t.Fatalf("questions not sanitized: %+v", result.Questions)
	}
	if len(result.Mentions) != 1 || result.Mentions[0] != "risk" {
		t.Fatalf("mentions not sanitized: %+v", result.Mentions)
	}
	if len(result.Citations) != 1 || len(result.Facts) != 2 || len(result.Assumptions) != 1 || len(result.Inferences) != 1 || len(result.EvidenceGaps) != 1 {
		t.Fatalf("structured claims mismatch: %+v", result)
	}
}

func TestFallbackModeratorPlanFromTextBuildsKickoffQuestions(t *testing.T) {
	raw := "# Kickoff\n\n- Review the latest quote and liquidity.\n- Challenge the thesis with explicit risk checks.\n- Produce evidence-backed next steps for the team."
	plan, ok := FallbackModeratorPlanFromText(raw, []string{"analyst", "risk"}, true)
	if !ok || !plan.ContinueDiscussion || plan.Content == "" {
		t.Fatalf("expected fallback plan: ok=%v plan=%+v", ok, plan)
	}
	if len(plan.Questions) != 2 || plan.Questions[0]["target"] != "analyst" || plan.Questions[1]["target"] != "risk" {
		t.Fatalf("fallback questions mismatch: %+v", plan.Questions)
	}
	if _, ok := FallbackModeratorPlanFromText("too short", []string{"analyst"}, true); ok {
		t.Fatal("short text should not become a fallback moderator plan")
	}
}

func TestValidateModeratorRecapActionsRequiresEvidenceForExecutableActions(t *testing.T) {
	if err := ValidateModeratorRecapActions(map[string]any{"summary": "observe only"}); err != nil {
		t.Fatalf("recap without actions should not require evidence: %v", err)
	}

	missingClaims := map[string]any{
		"watchlist_actions": []any{map[string]any{"code": "600519", "active": true}},
		"citations":         []any{"market.realtime_quote"},
	}
	if err := ValidateModeratorRecapActions(missingClaims); err == nil || !strings.Contains(err.Error(), "structured fact or inference") {
		t.Fatalf("expected missing claim error, got %v", err)
	}

	missingCitations := map[string]any{
		"orders":     []any{map[string]any{"code": "600519", "side": "buy", "position_pct": 0.05}},
		"inferences": []any{"pilot order is reasonable"},
	}
	if err := ValidateModeratorRecapActions(missingCitations); err == nil || !strings.Contains(err.Error(), "citation") {
		t.Fatalf("expected missing citation error, got %v", err)
	}

	valid := map[string]any{
		"facts":             []any{"600519 quote came from market.realtime_quote"},
		"inferences":        []any{"watchlist tracking is justified"},
		"citations":         []any{"@analyst", "market.realtime_quote"},
		"watchlist_actions": []any{map[string]any{"code": "600519", "active": true}},
		"wake_plans":        []any{map[string]any{"trigger_type": "indicator", "trigger_config": map[string]any{"code": "600519", "threshold": "100"}}},
	}
	if err := ValidateModeratorRecapActions(valid); err != nil {
		t.Fatalf("valid evidenced actions rejected: %v", err)
	}

	predictionMissingCitations := map[string]any{
		"prediction_watchlist_actions": []any{map[string]any{"market_id": 42, "active": true}},
		"inferences":                   []any{"prediction market should remain visible"},
	}
	if err := ValidateModeratorRecapActions(predictionMissingCitations); err == nil || !strings.Contains(err.Error(), "citation") {
		t.Fatalf("expected missing citation error for prediction watchlist action, got %v", err)
	}

	predictionValid := map[string]any{
		"facts":                        []any{"prediction.market_snapshot provided market state"},
		"citations":                    []any{"prediction.market_snapshot"},
		"prediction_watchlist_actions": []any{map[string]any{"market_id": 42, "active": true}},
	}
	if err := ValidateModeratorRecapActions(predictionValid); err != nil {
		t.Fatalf("valid prediction watchlist action rejected: %v", err)
	}
}

func TestValidateModeratorRecapActionsKeepsWakePlanSemanticGuard(t *testing.T) {
	recap := map[string]any{
		"facts":      []any{"510300 was discussed"},
		"citations":  []any{"@analyst"},
		"wake_plans": []any{map[string]any{"trigger_type": "indicator", "trigger_config": map[string]any{"threshold": "4.25"}}},
	}
	err := ValidateModeratorRecapActions(recap)
	if err == nil || !strings.Contains(err.Error(), "wake_plans[0]") || !strings.Contains(err.Error(), "requires code") {
		t.Fatalf("expected wake plan semantic error, got %v", err)
	}
	retry := ModeratorRecapValidationRetryInstruction(err)
	if !strings.Contains(retry, "include facts or inferences plus citations") || !strings.Contains(retry, "indicator wake_plans") {
		t.Fatalf("retry instruction missing guidance: %s", retry)
	}
}
