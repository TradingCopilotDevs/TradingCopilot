package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	appai "github.com/TradingCopilotDevs/TradingCopilot/internal/app/ai"
	appauth "github.com/TradingCopilotDevs/TradingCopilot/internal/app/auth"
	appdashboard "github.com/TradingCopilotDevs/TradingCopilot/internal/app/dashboard"
	appmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/app/market"
	appmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/app/meeting"
	appmessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/messaging"
	apppaper "github.com/TradingCopilotDevs/TradingCopilot/internal/app/paper"
	appresearch "github.com/TradingCopilotDevs/TradingCopilot/internal/app/research"
	appsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/app/settings"
	appwake "github.com/TradingCopilotDevs/TradingCopilot/internal/app/wake"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	infraai "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/ai"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	inframarketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/marketdata"
	inframessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/messaging"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/connect"
	infradashboard "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/dashboard"
	gormmarketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/marketdata"
	inframeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/meeting"
	infrapaper "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/paper"
	infraproxy "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy"
	runtimeproxy "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	gormuow "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/uow"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestBuildMeetingTrustReportKeepsStructuredClaimsAndConclusionDiffs(t *testing.T) {
	now := time.Now()
	roleKey := "analyst"
	summary := "current summary"
	conclusion := "current conclusion. Risk must be reviewed. Add watchlist."
	row := domainmeeting.Meeting{
		ID:         7,
		Topic:      "600519 research",
		Summary:    &summary,
		Conclusion: &conclusion,
		CreatedAt:  now,
	}
	events := []domainmeeting.Event{
		{
			ID:        11,
			MeetingID: row.ID,
			Sequence:  1,
			Type:      domainkernel.EventToolResult,
			RoleKey:   &roleKey,
			Content:   "quote",
			Payload: domainkernel.NewJSON(map[string]any{
				"status":         "tool_result",
				"tool":           "market.realtime_quote",
				"result_preview": "600519 last=1688",
			}),
			CreatedAt: now,
		},
		{
			ID:        12,
			MeetingID: row.ID,
			Sequence:  2,
			Type:      domainkernel.EventRoleMessage,
			RoleKey:   &roleKey,
			Content:   "analysis",
			Payload: domainkernel.NewJSON(map[string]any{
				"status":         "role_completed",
				"confidence":     "high",
				"provider_id":    3,
				"provider_name":  "fixture-ai",
				"model":          "fixture-model",
				"prompt_version": "managed-runtime-v1",
				"prompt_snapshot": map[string]any{
					"promptHash":         "sha256:fixture",
					"systemPromptHash":   "sha256:system",
					"userPromptHash":     "sha256:user",
					"promptTemplateHash": "sha256:template",
					"messageCount":       2,
				},
				"citations":     []string{"market.realtime_quote"},
				"facts":         []string{"600519 quote came from realtime provider"},
				"assumptions":   []string{"liquidity remains normal"},
				"inferences":    []string{"watchlist tracking is justified"},
				"evidence_gaps": []string{"confirm next trading day volume"},
			}),
			CreatedAt: now.Add(time.Minute),
		},
		{
			ID:        13,
			MeetingID: row.ID,
			Sequence:  3,
			Type:      domainkernel.EventConclusion,
			Content:   "initial conclusion. Risk must be reviewed.",
			Payload:   domainkernel.NewJSON(map[string]any{"status": "completed"}),
			CreatedAt: now.Add(2 * time.Minute),
		},
		{
			ID:        14,
			MeetingID: row.ID,
			Sequence:  4,
			Type:      domainkernel.EventSystem,
			Content:   "Trust review recorded.",
			Payload: domainkernel.NewJSON(map[string]any{
				"status":             "trust_review",
				"sentence_id":        "sentence:risk",
				"sentence":           "Risk must be reviewed.",
				"verdict":            "confirmed",
				"citation_ids":       []string{"12:1"},
				"evidence_event_ids": []uint{11},
				"comment":            "verified",
				"reviewer":           "admin",
				"reviewed_at":        now.Add(3 * time.Minute),
			}),
			CreatedAt: now.Add(3 * time.Minute),
		},
		{
			ID:        15,
			MeetingID: row.ID,
			Sequence:  5,
			Type:      domainkernel.EventSystem,
			RoleKey:   &roleKey,
			Content:   "Executable recap actions require manual review.",
			Payload: domainkernel.NewJSON(map[string]any{
				"status":                 "recap_actions_review_required",
				"policy":                 "weak_reference_review",
				"disposition":            "manual_review_required",
				"reason":                 "weak role-only citation",
				"watchlist_action_count": 1,
				"suggestion_count":       1,
				"suggested_actions": []map[string]any{
					{
						"action_type": "watchlist",
						"index":       0,
						"disposition": "manual_review_required",
						"reason":      "weak role-only citation",
						"spec":        map[string]any{"code": "600519", "active": true},
					},
				},
				"evidence_summary": map[string]any{
					"facts":     []string{"600519 was discussed by the analyst"},
					"citations": []string{"@analyst"},
				},
			}),
			CreatedAt: now.Add(4 * time.Minute),
		},
	}

	report := buildMeetingTrustReport(row, events)
	claims := report["claimBreakdown"].(map[string]any)
	if facts := claims["facts"].([]string); len(facts) != 1 || facts[0] != "600519 quote came from realtime provider" {
		t.Fatalf("facts not preserved: %+v", claims)
	}
	if gaps := report["evidenceGaps"].([]string); len(gaps) != 1 || gaps[0] != "confirm next trading day volume" {
		t.Fatalf("evidence gaps not preserved: %+v", report)
	}
	promptSnapshots := report["promptSnapshots"].([]map[string]any)
	if len(promptSnapshots) != 1 || promptSnapshots[0]["promptHash"] != "sha256:fixture" || promptSnapshots[0]["promptVersion"] != "managed-runtime-v1" {
		t.Fatalf("prompt snapshots not preserved: %+v", report)
	}
	bindings := report["claimEvidenceBindings"].([]map[string]any)
	if len(bindings) == 0 {
		t.Fatalf("claim evidence bindings missing: %+v", report)
	}
	firstBinding := bindings[0]
	if firstBinding["claimType"] != "fact" || firstBinding["claim"] != "600519 quote came from realtime provider" {
		t.Fatalf("unexpected first claim binding: %+v", firstBinding)
	}
	evidenceIDs := firstBinding["evidenceEventIds"].([]uint)
	citationIDs := firstBinding["citationIds"].([]string)
	if len(evidenceIDs) != 1 || evidenceIDs[0] != 11 || len(citationIDs) != 1 || citationIDs[0] != "12:1" {
		t.Fatalf("claim binding should point to tool evidence and citation: %+v", firstBinding)
	}
	evidenceGate := report["evidenceGate"].(map[string]any)
	if evidenceGate["status"] != "pass" || report["evidenceGateStatus"] != "pass" || evidenceGate["unsupportedClaimCount"] != 0 {
		t.Fatalf("evidence gate should pass when facts and inferences are bound: %+v", evidenceGate)
	}
	diffs := report["conclusionDiffs"].([]map[string]any)
	if len(diffs) == 0 || diffs[len(diffs)-1]["changed"] != true {
		t.Fatalf("conclusion diffs missing changed transition: %+v", report)
	}
	lastDiff := diffs[len(diffs)-1]
	if lastDiff["addedSentenceCount"] != 2 || lastDiff["removedSentenceCount"] != 1 || lastDiff["unchangedSentenceCount"] != 1 {
		t.Fatalf("sentence diff counts mismatch: %+v", lastDiff)
	}
	addedSentences := lastDiff["addedSentences"].([]string)
	removedSentences := lastDiff["removedSentences"].([]string)
	unchangedSentences := lastDiff["unchangedSentences"].([]string)
	if len(addedSentences) != 2 || addedSentences[0] != "current conclusion." || addedSentences[1] != "Add watchlist." {
		t.Fatalf("added sentence diff mismatch: %+v", lastDiff)
	}
	if len(removedSentences) != 1 || removedSentences[0] != "initial conclusion." {
		t.Fatalf("removed sentence diff mismatch: %+v", lastDiff)
	}
	if len(unchangedSentences) != 1 || unchangedSentences[0] != "Risk must be reviewed." {
		t.Fatalf("unchanged sentence diff mismatch: %+v", lastDiff)
	}
	reviews := report["sentenceReviews"].([]map[string]any)
	if len(reviews) != 1 || reviews[0]["sentenceId"] != "sentence:risk" || reviews[0]["verdict"] != "confirmed" {
		t.Fatalf("sentence reviews missing structured review: %+v", report)
	}
	reviewEvidenceIDs := reviews[0]["evidenceEventIds"].([]uint)
	reviewCitationIDs := reviews[0]["citationIds"].([]string)
	if len(reviewEvidenceIDs) != 1 || reviewEvidenceIDs[0] != 11 || len(reviewCitationIDs) != 1 || reviewCitationIDs[0] != "12:1" {
		t.Fatalf("sentence review references mismatch: %+v", reviews[0])
	}
	if report["recapActionReviewStatus"] != "needs_review" || report["recapActionSuggestionCount"] != 1 {
		t.Fatalf("recap action review status mismatch: %+v", report)
	}
	recapSuggestions := report["recapActionSuggestions"].([]map[string]any)
	if len(recapSuggestions) != 1 {
		t.Fatalf("recap action suggestions missing: %+v", report)
	}
	recapSuggestion := recapSuggestions[0]
	if recapSuggestion["eventId"] != uint(15) || recapSuggestion["actionType"] != "watchlist" || recapSuggestion["disposition"] != "manual_review_required" {
		t.Fatalf("recap action suggestion mismatch: %+v", recapSuggestion)
	}
	spec := recapSuggestion["spec"].(map[string]any)
	if spec["code"] != "600519" || spec["active"] != true {
		t.Fatalf("recap action spec mismatch: %+v", recapSuggestion)
	}
}

func TestBuildMeetingTrustReportBlocksUnsupportedFactAndInferenceClaims(t *testing.T) {
	now := time.Now()
	roleKey := "risk"
	row := domainmeeting.Meeting{
		ID:        8,
		Topic:     "unsupported claims",
		CreatedAt: now,
	}
	events := []domainmeeting.Event{
		{
			ID:        21,
			MeetingID: row.ID,
			Sequence:  1,
			Type:      domainkernel.EventRoleMessage,
			RoleKey:   &roleKey,
			Content:   "risk analysis",
			Payload: domainkernel.NewJSON(map[string]any{
				"status":      "role_completed",
				"facts":       []string{"600000 has a positive catalyst"},
				"assumptions": []string{"liquidity remains normal"},
				"inferences":  []string{"increase position size"},
			}),
			CreatedAt: now,
		},
	}

	report := buildMeetingTrustReport(row, events)
	gate := report["evidenceGate"].(map[string]any)
	if gate["status"] != "blocked" || report["evidenceGateStatus"] != "blocked" {
		t.Fatalf("evidence gate should block unsupported fact/inference claims: %+v", gate)
	}
	if gate["checkedClaimCount"] != 2 || gate["supportedClaimCount"] != 0 || gate["unsupportedClaimCount"] != 2 {
		t.Fatalf("evidence gate counts mismatch: %+v", gate)
	}
	unsupported := gate["unsupportedClaims"].([]map[string]any)
	if len(unsupported) != 2 {
		t.Fatalf("unsupported claims mismatch: %+v", gate)
	}
	if unsupported[0]["claimType"] != "fact" || unsupported[1]["claimType"] != "inference" {
		t.Fatalf("unsupported claims should only include fact and inference: %+v", unsupported)
	}
	topLevelUnsupported := report["unsupportedClaims"].([]map[string]any)
	if len(topLevelUnsupported) != 2 || report["unsupportedClaimCount"] != 2 {
		t.Fatalf("top-level unsupported claim summary mismatch: %+v", report)
	}
	gaps := report["gaps"].([]string)
	if !containsString(gaps, "unsupported fact or inference claims without evidence") {
		t.Fatalf("evidence gate gap missing: %+v", gaps)
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func TestRouterServesHealthFromTransport(t *testing.T) {
	server := newTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)

	server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "application/vnd.api+json" {
		t.Fatalf("expected JSON:API content type, got %q", contentType)
	}
	var doc struct {
		Data struct {
			Type       string         `json:"type"`
			ID         string         `json:"id"`
			Attributes map[string]any `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Data.Type != "health-checks" || doc.Data.ID != "api" {
		t.Fatalf("unexpected health resource: %+v", doc.Data)
	}
	if doc.Data.Attributes["app"] != "transport-test" || doc.Data.Attributes["env"] != "test" {
		t.Fatalf("unexpected health attributes: %+v", doc.Data.Attributes)
	}
}

func TestAuthRoutesUseTransportUsecase(t *testing.T) {
	server := newTestServer(t)

	state := requestJSON(t, server, http.MethodGet, "/api/auth/bootstrap-required", "")
	assertResource(t, state, "auth-bootstrap-states", "default")

	plain := requestRawJSONWithStatus(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`, http.StatusUnsupportedMediaType)
	assertErrorCode(t, plain, "unsupported-media-type")

	token := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"data":{"type":"auth-bootstrap-requests","attributes":{"username":"admin","password":"password123"}}}`)
	assertResource(t, token, "auth-tokens", "current")

	conflict := requestJSONWithStatus(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin2","password":"password123"}`, http.StatusConflict)
	assertErrorCode(t, conflict, "admin-exists")

	login := requestJSON(t, server, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"password123"}`)
	assertResource(t, login, "auth-tokens", "current")
	loginToken := resourceAttribute(t, login, "accessToken").(string)

	logout := requestJSONWithToken(t, server, http.MethodPost, "/api/auth/logout", "", loginToken)
	assertResourceType(t, logout, "auth-sessions")
	if got := resourceAttribute(t, logout, "status"); got != "revoked" {
		t.Fatalf("logout session status = %#v, want revoked", got)
	}
	reused := requestJSONWithStatusAndToken(t, server, http.MethodGet, "/api/admin/users", "", http.StatusUnauthorized, loginToken)
	assertErrorCode(t, reused, "invalid-access-token")

	unauthorized := requestJSONWithStatus(t, server, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"bad"}`, http.StatusUnauthorized)
	assertErrorCode(t, unauthorized, "invalid-credentials")
}

func TestSetupReadinessActionsAreExplicitAndIdempotent(t *testing.T) {
	server := newTestServer(t)
	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)

	initial := requestJSONWithToken(t, server, http.MethodGet, "/api/setup/readiness", "", token)
	assertResource(t, initial, "setup-readinesses", "current")
	if step := setupStepByKey(t, initial, "admin"); step["ready"] != true || step["status"] != "ready" {
		t.Fatalf("admin step should be ready after bootstrap: %+v", step)
	}
	for _, key := range []string{"paperRisk", "researchTeam", "messaging"} {
		step := setupStepByKey(t, initial, key)
		if step["ready"] == true || step["status"] == "ready" {
			t.Fatalf("initial readiness for %s should report missing config without creating defaults: %+v", key, step)
		}
	}

	aiSync := requestJSONWithToken(t, server, http.MethodPost, "/api/setup/actions/sync-ai-models", "", token)
	if attrs := resourceAttributes(t, aiSync); attrs["status"] != "warning" {
		t.Fatalf("sync-ai-models without providers should be a warning action result: %+v", attrs)
	}
	if output := setupActionOutput(t, aiSync); output["attemptedProviders"] != float64(0) || output["syncedModels"] != float64(0) {
		t.Fatalf("sync-ai-models output mismatch: %+v", output)
	}
	requestJSONWithToken(t, server, http.MethodPost, "/api/setup/actions/sync-ai-models", "", token)

	proxyTest := requestJSONWithToken(t, server, http.MethodPost, "/api/setup/actions/test-proxy", "", token)
	if attrs := resourceAttributes(t, proxyTest); attrs["status"] != "ok" {
		t.Fatalf("test-proxy should succeed with skipped module checks when proxy is disabled: %+v", attrs)
	}
	requestJSONWithToken(t, server, http.MethodPost, "/api/setup/actions/test-proxy", "", token)

	paperFirst := requestJSONWithToken(t, server, http.MethodPost, "/api/setup/actions/ensure-paper-defaults", "", token)
	paperOut := setupActionOutput(t, paperFirst)
	if paperOut["accounts"] != float64(1) || paperOut["riskConfigs"] != float64(1) {
		t.Fatalf("ensure-paper-defaults should create one default account and risk config: %+v", paperOut)
	}
	paperSecond := requestJSONWithToken(t, server, http.MethodPost, "/api/setup/actions/ensure-paper-defaults", "", token)
	paperAgain := setupActionOutput(t, paperSecond)
	if paperAgain["accounts"] != float64(1) || paperAgain["riskConfigs"] != float64(1) {
		t.Fatalf("ensure-paper-defaults should be idempotent: %+v", paperAgain)
	}
	afterPaper := requestJSONWithToken(t, server, http.MethodGet, "/api/setup/readiness", "", token)
	if step := setupStepByKey(t, afterPaper, "paperRisk"); step["ready"] != true || step["status"] != "ready" {
		t.Fatalf("paper step should become ready after defaults: %+v", step)
	}
	if step := setupStepByKey(t, afterPaper, "researchTeam"); step["ready"] == true {
		t.Fatalf("research team should still wait for explicit default-team action: %+v", step)
	}

	teamFirst := requestJSONWithToken(t, server, http.MethodPost, "/api/setup/actions/ensure-default-team", "", token)
	teamOut := setupActionOutput(t, teamFirst)
	if teamOut["created"] != true || teamOut["roleCount"] != float64(len(appai.DefaultRoles)) {
		t.Fatalf("ensure-default-team should create default team and roles: %+v", teamOut)
	}
	teamSecond := requestJSONWithToken(t, server, http.MethodPost, "/api/setup/actions/ensure-default-team", "", token)
	teamAgain := setupActionOutput(t, teamSecond)
	if teamAgain["created"] != false || teamAgain["roleCount"] != float64(len(appai.DefaultRoles)) || teamAgain["teamId"] != teamOut["teamId"] {
		t.Fatalf("ensure-default-team should reuse the same team and roles: first=%+v second=%+v", teamOut, teamAgain)
	}

	filterFirst := requestJSONWithToken(t, server, http.MethodPost, "/api/setup/actions/ensure-message-filter", "", token)
	filterOut := setupActionOutput(t, filterFirst)
	if fmt.Sprint(filterOut["filterId"]) == "" || fmt.Sprint(filterOut["filterName"]) == "" {
		t.Fatalf("ensure-message-filter should create or return a default filter: %+v", filterOut)
	}
	filterSecond := requestJSONWithToken(t, server, http.MethodPost, "/api/setup/actions/ensure-message-filter", "", token)
	filterAgain := setupActionOutput(t, filterSecond)
	if filterAgain["filterId"] != filterOut["filterId"] || filterAgain["filterName"] != filterOut["filterName"] {
		t.Fatalf("ensure-message-filter should be idempotent: first=%+v second=%+v", filterOut, filterAgain)
	}

	if got := collectionLen(t, requestJSONWithToken(t, server, http.MethodGet, "/api/paper/accounts", "", token)); got != 1 {
		t.Fatalf("paper account count after repeated setup actions = %d, want 1", got)
	}
	if got := collectionLen(t, requestJSONWithToken(t, server, http.MethodGet, "/api/paper/risk-configs", "", token)); got != 1 {
		t.Fatalf("paper risk config count after repeated setup actions = %d, want 1", got)
	}
	teamsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/research-teams", "", token)
	if got := collectionLen(t, teamsDoc); got != 1 {
		t.Fatalf("research team count after repeated setup actions = %d, want 1", got)
	}
	teamID := teamsDoc["data"].([]any)[0].(map[string]any)["id"].(string)
	if got := collectionLen(t, requestJSONWithToken(t, server, http.MethodGet, "/api/research-teams/"+teamID+"/roles", "", token)); got != len(appai.DefaultRoles) {
		t.Fatalf("research team role count after repeated setup actions = %d, want %d", got, len(appai.DefaultRoles))
	}
	if got := collectionLen(t, requestJSONWithToken(t, server, http.MethodGet, "/api/message-subscription-filters", "", token)); got != 1 {
		t.Fatalf("message filter count after repeated setup actions = %d, want 1", got)
	}
}

func TestSetupWizardSmokeE2ECreatesMeetingActions(t *testing.T) {
	fixture := newTestServerFixtureWithMeetingEnqueuer(t, "", func(uint) error { return nil })
	server := fixture.Server
	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)

	providerDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/ai/providers", `{"name":"Fixture AI","baseUrl":"https://ai.example/v1","defaultModel":"fixture-model","apiKey":"fixture-key","enabled":true}`, token)
	providerID := parseResourceUintID(t, providerDoc)

	requestJSONWithToken(t, server, http.MethodPost, "/api/setup/actions/ensure-paper-defaults", "", token)
	requestJSONWithToken(t, server, http.MethodPost, "/api/setup/actions/ensure-default-team", "", token)
	requestJSONWithToken(t, server, http.MethodPost, "/api/setup/actions/ensure-message-filter", "", token)

	teamsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/research-teams", "", token)
	teamID := firstCollectionResourceID(t, teamsDoc)
	teamIDUint := parseUintString(t, teamID)
	filtersDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/message-subscription-filters", "", token)
	filterID := firstCollectionResourceID(t, filtersDoc)
	filterIDUint := parseUintString(t, filterID)
	requestJSONWithToken(t, server, http.MethodPut, "/api/message-subscription-filters/"+filterID, fmt.Sprintf(`{"providerId":%d,"model":"fixture-model","enabled":true,"isDefault":true}`, providerID), token)

	subDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/message-subscriptions", fmt.Sprintf(`{"provider":"rss_feed","title":"Fixture RSS","sourceRef":"https://example.test/feed.xml","enabled":true,"filterId":%d,"teamIds":[%d],"backfillLimit":0}`, filterIDUint, teamIDUint), token)
	assertResourceType(t, subDoc, "message-subscriptions")
	requestJSONWithToken(t, server, http.MethodPost, "/api/market/symbols", `{"code":"000001","name":"Ping An Bank","exchange":"SZ","active":true}`, token)

	meetingDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/meetings", fmt.Sprintf(`{"researchTeamId":%d,"topic":"000001 setup smoke","triggerSource":"manual","context":{"symbols":["000001"]}}`, teamIDUint), token)
	assertResourceType(t, meetingDoc, "meetings")
	meetingID := resourceID(t, meetingDoc)
	meetingIDUint := parseUintString(t, meetingID)
	meetingRow, found, err := gormrepo.NewMeetingRepository(fixture.DB).Find(context.Background(), meetingIDUint)
	if err != nil || !found {
		t.Fatalf("meeting should be persisted: found=%v err=%v", found, err)
	}
	recap := map[string]any{
		"summary":    "setup smoke summary",
		"conclusion": "track 000001 and create a small paper proposal",
		"facts":      []string{"000001 was configured as a market symbol in setup smoke."},
		"inferences": []string{"A small simulated order and follow-up wake plan are justified for smoke validation."},
		"citations":  []string{"@moderator", "market.symbols"},
		"watchlist_actions": []map[string]any{{
			"code":   "000001",
			"name":   "Ping An Bank",
			"note":   "setup smoke watch",
			"active": true,
		}},
		"wake_plans": []map[string]any{{
			"trigger_type":  "time",
			"next_check_at": "2026-06-10 09:30:00",
			"reason":        "setup smoke follow-up",
			"trigger_config": map[string]any{
				"topic": "setup smoke follow-up",
			},
		}},
		"orders": []map[string]any{{
			"code":            "000001",
			"side":            "buy",
			"quantity":        100,
			"suggested_price": 10,
			"reason":          "setup smoke order",
		}},
	}
	rawRecap, err := json.Marshal(recap)
	if err != nil {
		t.Fatal(err)
	}
	roleKey := "moderator"
	recapEvent, err := inframeeting.AppendEvent(fixture.DB, meetingRow.ID, domainkernel.EventRoleMessage, &roleKey, "```json\n"+string(rawRecap)+"\n```", map[string]any{
		"status":     "role_completed",
		"facts":      recap["facts"],
		"inferences": recap["inferences"],
		"citations":  recap["citations"],
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := inframeeting.ApplyMeetingRecapActions(fixture.DB, meetingRow, recapEvent, roleKey, recap); err != nil {
		t.Fatal(err)
	}

	watchlistDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/market/watchlist?researchTeamId="+teamID, "", token)
	watchAttrs := collectionResourceByAttribute(t, watchlistDoc, "code", "000001")
	if watchAttrs["active"] != true || fmt.Sprint(watchAttrs["note"]) != "setup smoke watch" {
		t.Fatalf("watchlist action was not persisted as expected: %+v", watchAttrs)
	}
	wakeDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/wake-plans?meetingId="+meetingID, "", token)
	wakeAttrs := collectionResourceByAttribute(t, wakeDoc, "triggerType", "time")
	if wakeAttrs["status"] != "active" || fmt.Sprint(wakeAttrs["reason"]) != "setup smoke follow-up" {
		t.Fatalf("wake action was not persisted as expected: %+v", wakeAttrs)
	}
	ordersDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/paper/orders", "", token)
	orderAttrs := collectionResourceByAttribute(t, ordersDoc, "code", "000001")
	if orderAttrs["status"] != "suggested" || fmt.Sprint(orderAttrs["meetingId"]) != meetingID || fmt.Sprint(orderAttrs["sourceMeetingEventId"]) != fmt.Sprint(recapEvent.ID) {
		t.Fatalf("paper order action was not persisted as expected: %+v", orderAttrs)
	}

	readiness := requestJSONWithToken(t, server, http.MethodGet, "/api/setup/readiness", "", token)
	for _, key := range []string{"aiProviders", "researchTeam", "paperRisk", "market", "messaging"} {
		step := setupStepByKey(t, readiness, key)
		if step["ready"] != true || step["status"] != "ready" {
			t.Fatalf("setup step %s should be ready after smoke path: %+v", key, step)
		}
	}
}

func TestWakePlanListCanFocusOverdueActivePlans(t *testing.T) {
	server := newTestServer(t)
	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)
	teamID := createReadyResearchTeam(t, server, token)

	dueAt := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339)
	futureAt := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	pausedDueAt := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)
	requestJSONWithToken(t, server, http.MethodPost, "/api/wake-plans", fmt.Sprintf(`{"data":{"type":"wake-plans","attributes":{"researchTeamId":%s,"triggerType":"time","triggerConfig":{"topic":"overdue follow-up"},"reason":"overdue plan","status":"active","nextCheckAt":"%s"}}}`, teamID, dueAt), token)
	requestJSONWithToken(t, server, http.MethodPost, "/api/wake-plans", fmt.Sprintf(`{"data":{"type":"wake-plans","attributes":{"researchTeamId":%s,"triggerType":"time","triggerConfig":{"topic":"future follow-up"},"reason":"future plan","status":"active","nextCheckAt":"%s"}}}`, teamID, futureAt), token)
	requestJSONWithToken(t, server, http.MethodPost, "/api/wake-plans", fmt.Sprintf(`{"data":{"type":"wake-plans","attributes":{"researchTeamId":%s,"triggerType":"time","triggerConfig":{"topic":"paused follow-up"},"reason":"paused overdue plan","status":"paused","nextCheckAt":"%s"}}}`, teamID, pausedDueAt), token)

	doc := requestJSONWithToken(t, server, http.MethodGet, "/api/wake-plans?overdue=true", "", token)
	data, ok := doc["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("overdue wake plan collection mismatch: %+v", doc)
	}
	attrs := collectionResourceAttributes(t, doc, 0)
	if attrs["reason"] != "overdue plan" || attrs["status"] != "active" {
		t.Fatalf("unexpected overdue wake plan: %+v", attrs)
	}
}

func TestAdminSecurityRoutesManageUsersSessionsAndAudit(t *testing.T) {
	server := newTestServer(t)
	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)

	usersDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/admin/users", "", token)
	assertCollection(t, usersDoc, "admin-users")

	userDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/admin/users", `{"data":{"type":"admin-users","attributes":{"username":"analyst","displayName":"Analyst","role":"viewer","password":"password123","active":true}}}`, token)
	assertResourceType(t, userDoc, "admin-users")
	userID := resourceID(t, userDoc)

	updatedDoc := requestJSONWithToken(t, server, http.MethodPut, "/api/admin/users/"+userID, `{"data":{"type":"admin-users","attributes":{"displayName":"Analyst Updated","role":"operator","active":true}}}`, token)
	assertResource(t, updatedDoc, "admin-users", userID)

	resetDenied := requestJSONWithStatusAndToken(t, server, http.MethodPost, "/api/admin/users/"+userID+"/password-reset", `{"data":{"type":"admin-user-password-resets","attributes":{"password":"new-password","confirm":false}}}`, http.StatusBadRequest, token)
	assertErrorCode(t, resetDenied, "confirmation-required")

	resetDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/admin/users/"+userID+"/password-reset", `{"data":{"type":"admin-user-password-resets","attributes":{"password":"new-password","confirm":true}}}`, token)
	assertResource(t, resetDoc, "admin-users", userID)

	sessionsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/admin/sessions?includeRevoked=true", "", token)
	assertCollection(t, sessionsDoc, "auth-sessions")
	sessionID := resourceIDFromCollection(t, sessionsDoc, "admin")

	revokeDenied := requestJSONWithStatusAndToken(t, server, http.MethodPost, "/api/admin/sessions/"+sessionID+"/revoke", `{"data":{"type":"auth-session-revokes","attributes":{"reason":"test","confirm":false}}}`, http.StatusBadRequest, token)
	assertErrorCode(t, revokeDenied, "confirmation-required")

	revokeDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/admin/sessions/"+sessionID+"/revoke", `{"data":{"type":"auth-session-revokes","attributes":{"reason":"test","confirm":true}}}`, token)
	assertResource(t, revokeDoc, "auth-sessions", sessionID)

	revoked := requestJSONWithStatusAndToken(t, server, http.MethodGet, "/api/admin/users", "", http.StatusUnauthorized, token)
	assertErrorCode(t, revoked, "invalid-access-token")

	newTokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"password123"}`)
	newToken := resourceAttribute(t, newTokenDoc, "accessToken").(string)
	auditDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/audit-events", "", newToken)
	assertCollection(t, auditDoc, "audit-events")
}

func TestRolePermissionsConstrainViewerAndOperator(t *testing.T) {
	server := newTestServer(t)
	ownerDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	ownerToken := resourceAttribute(t, ownerDoc, "accessToken").(string)

	requestJSONWithToken(t, server, http.MethodPost, "/api/admin/users", `{"data":{"type":"admin-users","attributes":{"username":"viewer","displayName":"Viewer","role":"viewer","password":"password123","active":true}}}`, ownerToken)
	requestJSONWithToken(t, server, http.MethodPost, "/api/admin/users", `{"data":{"type":"admin-users","attributes":{"username":"operator","displayName":"Operator","role":"operator","password":"password123","active":true}}}`, ownerToken)

	viewerDoc := requestJSON(t, server, http.MethodPost, "/api/auth/login", `{"username":"viewer","password":"password123"}`)
	viewerToken := resourceAttribute(t, viewerDoc, "accessToken").(string)
	assertResource(t, requestJSONWithToken(t, server, http.MethodGet, "/api/dashboard", "", viewerToken), "dashboards", "current")
	assertErrorCode(t, requestJSONWithStatusAndToken(t, server, http.MethodGet, "/api/admin/users", "", http.StatusForbidden, viewerToken), "permission-denied")
	assertErrorCode(t, requestJSONWithStatusAndToken(t, server, http.MethodGet, "/api/audit-events", "", http.StatusForbidden, viewerToken), "permission-denied")
	assertErrorCode(t, requestJSONWithStatusAndToken(t, server, http.MethodPost, "/api/market/watchlist", `{"code":"600000","name":"浦发银行"}`, http.StatusForbidden, viewerToken), "permission-denied")
	assertResourceType(t, requestJSONWithToken(t, server, http.MethodPost, "/api/auth/logout", "", viewerToken), "auth-sessions")

	operatorDoc := requestJSON(t, server, http.MethodPost, "/api/auth/login", `{"username":"operator","password":"password123"}`)
	operatorToken := resourceAttribute(t, operatorDoc, "accessToken").(string)
	assertErrorCode(t, requestJSONWithStatusAndToken(t, server, http.MethodGet, "/api/admin/users", "", http.StatusForbidden, operatorToken), "permission-denied")
	assertErrorCode(t, requestJSONWithStatusAndToken(t, server, http.MethodPut, "/api/settings/app-settings/MEETING_DAILY_TOKEN_BUDGET", `{"value":{"value":-1},"description":"operator should not change settings"}`, http.StatusForbidden, operatorToken), "permission-denied")
	assertErrorCode(t, requestJSONWithStatusAndToken(t, server, http.MethodPost, "/api/ai/providers", `{"name":"fixture","baseUrl":"https://ai.example/v1","defaultModel":"fixture-model","enabled":true}`, http.StatusForbidden, operatorToken), "permission-denied")
	assertErrorCode(t, requestJSONWithStatusAndToken(t, server, http.MethodPost, "/api/platform-adapters", `{"displayName":"Fixture Bot","enabled":true,"botToken":"token","chatId":"chat"}`, http.StatusForbidden, operatorToken), "permission-denied")

	accountDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/paper/accounts", `{"name":"Operator Account","initialCash":100000,"active":true}`, operatorToken)
	assertResourceType(t, accountDoc, "paper-accounts")
	retryDoc := requestJSONWithStatusAndToken(t, server, http.MethodPost, "/api/ops/jobs/retry-failed", "", http.StatusConflict, operatorToken)
	assertErrorCode(t, retryDoc, "ops-queue-retry-failed")
}

func TestMarketTaskEndpointRequiresConfiguredQueue(t *testing.T) {
	server := newTestServer(t)
	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)

	doc := requestJSONWithStatusAndToken(t, server, http.MethodPost, "/api/market/tasks", `{"data":{"type":"market-tasks","attributes":{"action":"refresh_quote","code":"600519"}}}`, http.StatusConflict, token)
	assertErrorCode(t, doc, "market-task-enqueue-failed")
}

func TestMeetingTrustReviewRouteAppendsReviewAndReportsIt(t *testing.T) {
	server := newTestServer(t)
	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)
	teamID := createReadyResearchTeam(t, server, token)

	meetingDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/meetings", `{"data":{"type":"meetings","attributes":{"researchTeamId":`+teamID+`,"topic":"Trust review fixture"}}}`, token)
	meetingID := resourceID(t, meetingDoc)

	reviewDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/meetings/"+meetingID+"/trust-reviews", `{"data":{"type":"meeting-trust-reviews","attributes":{"sentence":"Risk must be reviewed.","verdict":"needs_evidence","citationIds":["12:1"],"evidenceEventIds":[11],"comment":"needs source owner check"}}}`, token)
	assertResourceType(t, reviewDoc, "meeting-trust-reviews")
	reviewAttrs := resourceAttributes(t, reviewDoc)
	if reviewAttrs["verdict"] != "needs_evidence" || reviewAttrs["sentence"] != "Risk must be reviewed." {
		t.Fatalf("unexpected review response: %+v", reviewAttrs)
	}

	loadedDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/meetings/"+meetingID, "", token)
	trustReport, ok := resourceAttribute(t, loadedDoc, "trustReport").(map[string]any)
	if !ok {
		t.Fatalf("meeting trust report missing: %+v", loadedDoc)
	}
	reviews, ok := trustReport["sentenceReviews"].([]any)
	if !ok || len(reviews) != 1 {
		t.Fatalf("meeting trust report should include sentence review: %+v", trustReport)
	}
	review := reviews[0].(map[string]any)
	if review["verdict"] != "needs_evidence" || review["reviewer"] != "admin" || review["sentenceId"] == "" {
		t.Fatalf("meeting trust review not aggregated: %+v", review)
	}
}

func TestMeetingRecapActionReviewRouteAppendsReviewAndReportsIt(t *testing.T) {
	fixture := newTestServerFixtureWithMeetingEnqueuer(t, "", func(uint) error { return nil })
	server := fixture.Server
	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)
	teamID := createReadyResearchTeam(t, server, token)

	meetingDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/meetings", `{"data":{"type":"meetings","attributes":{"researchTeamId":`+teamID+`,"topic":"Recap action review fixture"}}}`, token)
	meetingID := resourceID(t, meetingDoc)
	meetingIDUint := parseUintString(t, meetingID)
	roleKey := "moderator"
	sourceEvent, err := inframeeting.AppendEvent(fixture.DB, meetingIDUint, domainkernel.EventSystem, &roleKey, "Executable recap actions require manual review.", map[string]any{
		"status":      "recap_actions_review_required",
		"policy":      "weak_reference_review",
		"disposition": "manual_review_required",
		"reason":      "weak role-only citation",
		"suggested_actions": []map[string]any{
			{
				"action_type": "watchlist",
				"index":       0,
				"disposition": "manual_review_required",
				"reason":      "weak role-only citation",
				"spec":        map[string]any{"code": "600519", "active": true},
			},
		},
		"evidence_summary": map[string]any{
			"facts":     []string{"600519 was discussed"},
			"citations": []string{"@analyst"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	suggestionID := fmt.Sprintf("%d:recap-action:1", sourceEvent.ID)

	denied := requestJSONWithStatusAndToken(t, server, http.MethodPost, "/api/meetings/"+meetingID+"/recap-action-reviews", `{"data":{"type":"meeting-recap-action-reviews","attributes":{"suggestionId":"`+suggestionID+`","decision":"approved"}}}`, http.StatusBadRequest, token)
	assertErrorCode(t, denied, "confirmation-required")

	reviewDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/meetings/"+meetingID+"/recap-action-reviews", `{"data":{"type":"meeting-recap-action-reviews","attributes":{"suggestionId":"`+suggestionID+`","actionType":"watchlist","decision":"approved","citationIds":["market.symbols"],"evidenceEventIds":[11],"comment":"source checked","confirm":true}}}`, token)
	assertResourceType(t, reviewDoc, "meeting-recap-action-reviews")
	reviewAttrs := resourceAttributes(t, reviewDoc)
	if reviewAttrs["decision"] != "approved" || reviewAttrs["executionDisposition"] != "manual_execution_required" || reviewAttrs["suggestionId"] != suggestionID {
		t.Fatalf("unexpected recap action review response: %+v", reviewAttrs)
	}

	loadedDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/meetings/"+meetingID, "", token)
	trustReport, ok := resourceAttribute(t, loadedDoc, "trustReport").(map[string]any)
	if !ok {
		t.Fatalf("meeting trust report missing: %+v", loadedDoc)
	}
	if trustReport["recapActionReviewStatus"] != "clear" || trustReport["recapActionReviewCount"] != float64(1) {
		t.Fatalf("recap action review aggregate mismatch: %+v", trustReport)
	}
	suggestions, ok := trustReport["recapActionSuggestions"].([]any)
	if !ok || len(suggestions) != 1 {
		t.Fatalf("recap action suggestion missing: %+v", trustReport)
	}
	suggestion := suggestions[0].(map[string]any)
	latestReview, ok := suggestion["latestReview"].(map[string]any)
	if !ok || latestReview["decision"] != "approved" || latestReview["reviewer"] != "admin" {
		t.Fatalf("recap action latest review not attached: %+v", suggestion)
	}
}

func TestProtectedMutationsAreAuditedWithoutRequestBody(t *testing.T) {
	server := newTestServer(t)
	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)

	requestJSONWithToken(t, server, http.MethodPost, "/api/settings/secrets", `{"kind":"market_data","name":"audit_fixture","value":"do-not-log-me"}`, token)
	auditDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/audit-events", "", token)
	event := findAuditEvent(t, auditDoc, "http.post", "settings.secrets")
	if event["outcome"] != "ok" {
		t.Fatalf("audit outcome = %#v, want ok", event["outcome"])
	}
	detail := fmt.Sprint(event["detail"])
	if strings.Contains(detail, "do-not-log-me") {
		t.Fatalf("audit detail leaked request body: %s", detail)
	}
}

func TestOpsRoutesUseDashboardDiagnostics(t *testing.T) {
	server := newTestServer(t)
	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)

	assertResource(t, requestJSONWithToken(t, server, http.MethodGet, "/api/ops/provider-health", "", token), "provider-health-reports", "current")
	jobsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/ops/jobs?queue=critical&type=paper:execute&state=archived&failedLimit=99", "", token)
	assertResource(t, jobsDoc, "ops-job-reports", "current")
	queue := resourceAttribute(t, jobsDoc, "queue").(map[string]any)
	if _, ok := queue["failedTasks"].([]any); !ok {
		t.Fatalf("ops queue diagnostics should expose failed task samples: %+v", queue)
	}
	filters, ok := queue["filters"].(map[string]any)
	if !ok || filters["queue"] != "critical" || filters["type"] != "paper:execute" || filters["state"] != "archived" || filters["failedLimit"] != float64(50) {
		t.Fatalf("ops queue diagnostics should expose normalized filters: %+v", queue)
	}
	if _, ok := queue["backlogRisk"].(map[string]any); !ok {
		t.Fatalf("ops queue diagnostics should expose backlog risk: %+v", queue)
	}
	recentErrors, ok := resourceAttribute(t, jobsDoc, "recentErrors").(map[string]any)
	if !ok {
		t.Fatalf("ops jobs should expose recent error diagnostics: %+v", jobsDoc)
	}
	if recentErrors["count"] == nil || recentErrors["entries"] == nil {
		t.Fatalf("recent error diagnostics missing count or entries: %+v", recentErrors)
	}
	retryDoc := requestJSONWithStatusAndToken(t, server, http.MethodPost, "/api/ops/jobs/retry-failed", "", http.StatusConflict, token)
	assertErrorCode(t, retryDoc, "ops-queue-retry-failed")
	runTaskDoc := requestJSONWithStatusAndToken(t, server, http.MethodPost, "/api/ops/jobs/default/tasks/task-1/run", "", http.StatusConflict, token)
	assertErrorCode(t, runTaskDoc, "ops-queue-task-run-failed")
	metricsRec := httptest.NewRecorder()
	metricsReq := httptest.NewRequest(http.MethodGet, "/api/ops/metrics", nil)
	metricsReq.Header.Set("Authorization", "Bearer "+token)
	server.Router().ServeHTTP(metricsRec, metricsReq)
	if metricsRec.Code != http.StatusOK {
		t.Fatalf("ops metrics status = %d body=%s", metricsRec.Code, metricsRec.Body.String())
	}
	if contentType := metricsRec.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/plain") {
		t.Fatalf("ops metrics content-type = %q", contentType)
	}
	if body := metricsRec.Body.String(); !strings.Contains(body, "tradingcopilot_info") || !strings.Contains(body, "tradingcopilot_queue_pending") || !strings.Contains(body, "tradingcopilot_meeting_run_avg_seconds_24h") || !strings.Contains(body, "tradingcopilot_ai_latency_avg_ms_24h") || !strings.Contains(body, "tradingcopilot_ai_cost_rate_missing_models") {
		t.Fatalf("ops metrics missing expected series: %s", body)
	}
	assertResource(t, requestJSONWithToken(t, server, http.MethodGet, "/api/ops/backups", "", token), "ops-backup-reports", "current")
	backupDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/ops/backups", "", token)
	assertResourceType(t, backupDoc, "ops-backup-runs")
	if resourceAttribute(t, backupDoc, "status") != "created" {
		t.Fatalf("backup status = %#v, want created", resourceAttribute(t, backupDoc, "status"))
	}
	backupName := fmt.Sprint(resourceAttribute(t, backupDoc, "backupName"))
	dryRunDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/ops/backups/"+backupName+"/restore-dry-run", "", token)
	assertResourceType(t, dryRunDoc, "ops-backup-restore-dry-runs")
	dryRunStatus := resourceAttribute(t, dryRunDoc, "status")
	if dryRunStatus != "valid" && dryRunStatus != "warning" {
		t.Fatalf("restore dry-run status = %#v", dryRunStatus)
	}
	if resourceAttribute(t, dryRunDoc, "sandboxStatus") != "extracted" {
		t.Fatalf("restore dry-run sandbox status = %#v, want extracted", resourceAttribute(t, dryRunDoc, "sandboxStatus"))
	}
	if resourceAttribute(t, dryRunDoc, "destructive") != false {
		t.Fatalf("restore dry-run must remain non-destructive: %+v", resourceAttributes(t, dryRunDoc))
	}
	sandboxDir := fmt.Sprint(resourceAttribute(t, dryRunDoc, "sandboxDir"))
	if sandboxDir == "" || filepath.Base(filepath.Dir(sandboxDir)) != "restore-drills" {
		t.Fatalf("restore dry-run sandbox dir = %q, want restore-drills child", sandboxDir)
	}
	fileCount, ok := resourceAttribute(t, dryRunDoc, "sandboxFileCount").(float64)
	if !ok || fileCount <= 0 {
		t.Fatalf("restore dry-run sandbox file count = %#v", resourceAttribute(t, dryRunDoc, "sandboxFileCount"))
	}
	if checks, ok := resourceAttribute(t, dryRunDoc, "sandboxChecks").([]any); !ok || len(checks) == 0 {
		t.Fatalf("restore dry-run sandbox checks missing: %+v", resourceAttributes(t, dryRunDoc))
	}
	backupsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/ops/backups", "", token)
	if resourceAttribute(t, backupsDoc, "status") != "ready" {
		t.Fatalf("backup report status = %#v, want ready", resourceAttribute(t, backupsDoc, "status"))
	}
	wantDrill := "drilled"
	if dryRunStatus == "warning" {
		wantDrill = "drill_warning"
	}
	if resourceAttribute(t, backupsDoc, "restoreDrill") != wantDrill {
		t.Fatalf("backup restore drill = %#v, want %s", resourceAttribute(t, backupsDoc, "restoreDrill"), wantDrill)
	}
	drillDetail, ok := resourceAttribute(t, backupsDoc, "restoreDrillDetail").(map[string]any)
	if !ok || drillDetail["lastDryRun"] == nil {
		t.Fatalf("backup restore drill detail missing last dry-run: %+v", drillDetail)
	}
}

func TestSafeZipExtractPathRejectsUnsafeEntries(t *testing.T) {
	root := t.TempDir()
	for _, entry := range []string{
		"../evil.txt",
		"/absolute.txt",
		`C:\evil.txt`,
		"database/../evil.txt",
		"database/./snapshot.db",
	} {
		if target, ok := safeZipExtractPath(root, entry); ok {
			t.Fatalf("safeZipExtractPath(%q) = %q, want rejected", entry, target)
		}
	}
	target, ok := safeZipExtractPath(root, "database/tradingcopilot.db")
	if !ok {
		t.Fatalf("safeZipExtractPath rejected valid database entry")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(rootAbs, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		t.Fatalf("safeZipExtractPath escaped root: root=%q target=%q rel=%q err=%v", rootAbs, target, rel, err)
	}
}

func TestBackupRetentionPrunesOldArchivesAndSandboxDirs(t *testing.T) {
	backupDir := t.TempDir()
	now := time.Now().UTC()
	for index := 0; index < 9; index++ {
		name := fmt.Sprintf("tradingcopilot-backup-202601%02d-000000.zip", index+1)
		path := filepath.Join(backupDir, name)
		if err := os.WriteFile(path, []byte("backup"), 0o600); err != nil {
			t.Fatal(err)
		}
		modTime := now.AddDate(0, 0, -index)
		if err := os.Chtimes(path, modTime, modTime); err != nil {
			t.Fatal(err)
		}
	}
	drillRoot := filepath.Join(backupDir, "restore-drills")
	oldDrill := filepath.Join(drillRoot, "old")
	recentDrill := filepath.Join(drillRoot, "recent")
	for _, dir := range []string{oldDrill, recentDrill} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := now.AddDate(0, 0, -8)
	recentTime := now.AddDate(0, 0, -1)
	if err := os.Chtimes(oldDrill, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(recentDrill, recentTime, recentTime); err != nil {
		t.Fatal(err)
	}

	backups, _, err := listBackupArchives(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	run := pruneBackupArtifacts(backupDir, backups, 3, 5, now)
	if run["status"] != "applied" {
		t.Fatalf("retention status = %#v", run["status"])
	}
	if got := run["prunedBackupCount"]; got != 3 {
		t.Fatalf("pruned backup count = %#v, want 3; run=%+v", got, run)
	}
	if got := run["prunedSandboxDirCount"]; got != 1 {
		t.Fatalf("pruned sandbox dir count = %#v, want 1; run=%+v", got, run)
	}
	remaining, _, err := listBackupArchives(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 6 {
		t.Fatalf("remaining backups = %d, want 6", len(remaining))
	}
	if _, err := os.Stat(oldDrill); !os.IsNotExist(err) {
		t.Fatalf("old restore drill should be pruned, stat err=%v", err)
	}
	if _, err := os.Stat(recentDrill); err != nil {
		t.Fatalf("recent restore drill should remain: %v", err)
	}
}

func TestSettingsReadRoutesUseTransportUsecaseAndRequireAdmin(t *testing.T) {
	server := newTestServer(t)

	unauthorized := requestJSONWithStatus(t, server, http.MethodGet, "/api/settings/runtime-env", "", http.StatusUnauthorized)
	assertErrorCode(t, unauthorized, "invalid-access-token")

	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)

	envDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/settings/runtime-env", "", token)
	assertCollection(t, envDoc, "runtime-env-vars")

	defsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/settings/secret-definitions", "", token)
	assertCollection(t, defsDoc, "secret-definitions")

	secretDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/settings/secrets", `{"kind":"market_data","name":"fixture","value":"secret"}`, token)
	assertResourceType(t, secretDoc, "secrets")
	secretsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/settings/secrets", "", token)
	assertCollection(t, secretsDoc, "secrets")

	settingDoc := requestJSONWithToken(t, server, http.MethodPut, "/api/settings/app-settings/CUSTOM_SETTING", `{"value":{"enabled":true},"description":"fixture"}`, token)
	assertResource(t, settingDoc, "app-settings", "CUSTOM_SETTING")
	budgetDoc := requestJSONWithToken(t, server, http.MethodPut, "/api/settings/app-settings/MEETING_DAILY_TOKEN_BUDGET", `{"value":{"value":-1},"description":"fixture"}`, token)
	assertResource(t, budgetDoc, "app-settings", "MEETING_DAILY_TOKEN_BUDGET")
	invalidBudget := requestJSONWithStatusAndToken(t, server, http.MethodPut, "/api/settings/app-settings/MEETING_DAILY_TOKEN_BUDGET", `{"value":{"value":0},"description":"fixture"}`, http.StatusBadRequest, token)
	assertErrorCode(t, invalidBudget, "app-setting-save-failed")
	settingsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/settings/app-settings", "", token)
	assertCollection(t, settingsDoc, "app-settings")

	proxyTestDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/settings/proxy/test", "", token)
	assertResource(t, proxyTestDoc, "proxy-tests", "current")

	proxyDoc := requestJSONWithToken(t, server, http.MethodPut, "/api/settings/proxy", `{"data":{"type":"proxy-settings","id":"current","attributes":{"proxyUrl":"http://proxy.example:8080","enabledAi":true,"enabledTelegram":false,"enabledMarket":true,"enabledWeb":false,"noProxy":["10.0.0.0/8",".internal"]}}}`, token)
	assertResource(t, proxyDoc, "proxy-settings", "current")
	proxyAttrs := resourceAttributes(t, proxyDoc)
	if proxyAttrs["proxyUrl"] != "http://proxy.example:8080" || proxyAttrs["enabledAi"] != true {
		t.Fatalf("unexpected proxy attributes: %+v", proxyAttrs)
	}

	loadedProxyDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/settings/proxy", "", token)
	assertResource(t, loadedProxyDoc, "proxy-settings", "current")

	legacyProxy := requestJSONWithStatusAndToken(t, server, http.MethodPut, "/api/settings/proxy", `{"proxy_url":"ftp://proxy.example:21","enabled_ai":true}`, http.StatusBadRequest, token)
	assertErrorCode(t, legacyProxy, "invalid-jsonapi-request")

	invalidProxy := requestJSONWithStatusAndToken(t, server, http.MethodPut, "/api/settings/proxy", `{"proxyUrl":"ftp://proxy.example:21","enabledAi":true}`, http.StatusBadRequest, token)
	assertErrorCode(t, invalidProxy, "proxy-save-failed")
}

func TestAIRoutesUseTransportUsecaseAndRequireAdmin(t *testing.T) {
	server := newTestServer(t)

	unauthorized := requestJSONWithStatus(t, server, http.MethodGet, "/api/ai/providers", "", http.StatusUnauthorized)
	assertErrorCode(t, unauthorized, "invalid-access-token")

	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)

	providersDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/ai/providers", "", token)
	assertEmptyOrCollection(t, providersDoc, "ai-providers")

	providerDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/ai/providers", `{"name":"fixture","baseUrl":"https://ai.example/v1","defaultModel":"fixture-model","enabled":true}`, token)
	assertResourceType(t, providerDoc, "ai-providers")
	providerID := resourceID(t, providerDoc)

	modelsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/ai/providers/"+providerID+"/models", "", token)
	assertEmptyOrCollection(t, modelsDoc, "ai-provider-models")

	toolsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/ai/tool-definitions", "", token)
	assertCollection(t, toolsDoc, "ai-tool-definitions")

	skillsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/ai/skill-definitions", "", token)
	assertCollection(t, skillsDoc, "ai-skill-definitions")

	rolesDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/ai/roles", "", token)
	assertCollection(t, rolesDoc, "ai-roles")

	updateDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/ai/roles/apply-default-capabilities", "", token)
	assertResource(t, updateDoc, "ai-role-capability-updates", "default")

	roleDoc := requestJSONWithToken(t, server, http.MethodPut, "/api/ai/roles/fixture_role", `{"name":"Fixture","responsibility":"fixture","promptTemplate":"return JSON","providerId":null,"model":null,"toolNames":["market.realtime_quote"],"skillNames":[],"enabled":true,"sortOrder":99}`, token)
	assertResource(t, roleDoc, "ai-roles", "fixture_role")
}

func TestResearchTeamApplyDefaultsDeletesAndRebuildsRoles(t *testing.T) {
	server := newTestServer(t)
	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)
	accountDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/paper/accounts", `{"name":"Team Account","initialCash":1000000,"active":true}`, token)
	accountID := resourceID(t, accountDoc)
	teamDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/research-teams", `{"name":"Team","description":"","paperAccountId":`+accountID+`,"active":true}`, token)
	teamID := resourceID(t, teamDoc)

	requestJSONWithToken(t, server, http.MethodPost, "/api/research-teams/"+teamID+"/roles/apply-defaults", "", token)
	requestJSONWithToken(t, server, http.MethodPut, "/api/research-teams/"+teamID+"/roles/moderator", `{"key":"moderator","name":"Custom Moderator","responsibility":"custom","promptTemplate":"custom prompt","providerId":null,"model":"custom-model","toolNames":[],"skillNames":[],"enabled":true,"sortOrder":99}`, token)
	requestJSONWithToken(t, server, http.MethodPut, "/api/research-teams/"+teamID+"/roles/custom", `{"key":"custom","name":"Custom Role","responsibility":"custom","promptTemplate":"custom prompt","providerId":null,"model":null,"toolNames":[],"skillNames":[],"enabled":true,"sortOrder":100}`, token)

	resetDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/research-teams/"+teamID+"/roles/apply-defaults", "", token)
	if got := resourceAttribute(t, resetDoc, "updated"); got != float64(len(appai.DefaultRoles)) {
		t.Fatalf("updated = %v, want %d", got, len(appai.DefaultRoles))
	}
	rolesDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/research-teams/"+teamID+"/roles", "", token)
	rows, ok := rolesDoc["data"].([]any)
	if !ok || len(rows) != len(appai.DefaultRoles) {
		t.Fatalf("roles after reset = %+v, want %d items", rolesDoc["data"], len(appai.DefaultRoles))
	}
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("unexpected role row: %+v", raw)
		}
		attrs, ok := row["attributes"].(map[string]any)
		if !ok {
			t.Fatalf("missing role attrs: %+v", row)
		}
		switch attrs["key"] {
		case "custom":
			t.Fatal("custom role should be removed by apply-defaults reset")
		case "moderator":
			if attrs["name"] == "Custom Moderator" || attrs["promptTemplate"] == "custom prompt" || attrs["model"] == "custom-model" {
				t.Fatalf("moderator role was not reset: %+v", attrs)
			}
		}
	}
}

func TestMessagingRoutesUseTransportUsecaseAndRequireAdmin(t *testing.T) {
	server := newTestServer(t)

	unauthorized := requestJSONWithStatus(t, server, http.MethodGet, "/api/message-subscriptions", "", http.StatusUnauthorized)
	assertErrorCode(t, unauthorized, "invalid-access-token")

	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)

	configDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/message-subscriptions/app-config", "", token)
	assertResource(t, configDoc, "message-subscription-app-configs", "telegram")

	teamID := createReadyResearchTeam(t, server, token)
	privateFeed := requestJSONWithStatusAndToken(t, server, http.MethodPost, "/api/message-subscriptions", `{"provider":"rss_feed","title":"Private RSS","sourceRef":"https://user:pass@example.test/feed.xml","enabled":false,"backfillLimit":0}`, http.StatusBadRequest, token)
	assertErrorCode(t, privateFeed, "message-subscription-create-failed")

	rssDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/message-subscriptions", `{"provider":"rss_feed","title":"Private RSS","sourceRef":"https://example.test/feed.xml","enabled":false,"backfillLimit":0,"rssAuthType":"basic","rssUsername":"feed-user","rssPassword":"feed-pass"}`, token)
	assertResourceType(t, rssDoc, "message-subscriptions")
	rssID := resourceID(t, rssDoc)
	if resourceAttribute(t, rssDoc, "rssAuthType") != "basic" || resourceAttribute(t, rssDoc, "rssUsername") != "feed-user" || resourceAttribute(t, rssDoc, "hasRssPassword") != true {
		t.Fatalf("rss auth response mismatch: %+v", rssDoc)
	}
	config, _ := resourceAttribute(t, rssDoc, "config").(map[string]any)
	if strings.Contains(fmt.Sprint(config), "feed-pass") || strings.Contains(fmt.Sprint(config), "passwordSecretName") {
		t.Fatalf("rss auth response leaked secret material: %+v", config)
	}
	maintenanceDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/message-subscriptions/maintenance", `{"action":"rotate_rss_auth","subscriptionIds":[`+rssID+`],"rssAuthType":"bearer","rssPassword":"rotated-token"}`, token)
	assertResourceType(t, maintenanceDoc, "message-subscription-maintenance-results")
	if resourceAttribute(t, maintenanceDoc, "updated") != float64(1) {
		t.Fatalf("maintenance update count mismatch: %+v", maintenanceDoc)
	}

	subDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/message-subscriptions", `{"title":"Fixture","sourceRef":"@fixture","enabled":true,"teamIds":[`+teamID+`],"backfillLimit":0}`, token)
	assertResourceType(t, subDoc, "message-subscriptions")
	if got := resourceAttribute(t, subDoc, "backfillLimit"); got != float64(0) {
		t.Fatalf("expected explicit zero backfill limit, got %v", got)
	}
	subID := resourceID(t, subDoc)

	duplicateSub := requestJSONWithToken(t, server, http.MethodPost, "/api/message-subscriptions", `{"title":"Fixture Again","sourceRef":"@fixture","enabled":true,"teamIds":[`+teamID+`],"backfillLimit":0}`, token)
	assertResource(t, duplicateSub, "message-subscriptions", subID)

	subsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/message-subscriptions", "", token)
	assertCollection(t, subsDoc, "message-subscriptions")

	diagnosticsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/message-subscriptions/diagnostics", "", token)
	assertCollection(t, diagnosticsDoc, "message-subscription-diagnostics")

	updatedSub := requestJSONWithToken(t, server, http.MethodPut, "/api/message-subscriptions/"+subID, `{"title":"Fixture Updated","enabled":false,"backfillLimit":0}`, token)
	assertResource(t, updatedSub, "message-subscriptions", subID)

	messageDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/ingested-messages", `{"subscriptionId":`+subID+`,"sourceMessageId":101,"text":"message body","filterDecision":"observe","filterReason":"manual","relatedSymbols":["600000"]}`, token)
	assertResourceType(t, messageDoc, "ingested-messages")
	messageID := resourceID(t, messageDoc)

	messagesDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/ingested-messages?subscriptionId="+subID, "", token)
	assertCollection(t, messagesDoc, "ingested-messages")

	feedbackDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/ingested-messages/"+messageID+"/feedback", `{"label":"helpful","comment":"useful signal"}`, token)
	assertResource(t, feedbackDoc, "ingested-messages", messageID)
	if resourceAttribute(t, feedbackDoc, "feedbackLabel") != "helpful" || resourceAttribute(t, feedbackDoc, "feedbackComment") != "useful signal" {
		t.Fatalf("feedback attributes mismatch: %+v", feedbackDoc)
	}

	trainingDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/message-feedback/training-samples?subscriptionId="+subID+"&feedbackLabel=helpful", "", token)
	assertCollection(t, trainingDoc, "message-feedback-training-samples")
	sourceTrustDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/message-feedback/source-trust?subscriptionId="+subID, "", token)
	assertCollection(t, sourceTrustDoc, "message-source-trust-sources")
	recomputeDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/message-feedback/source-trust/recompute?subscriptionId="+subID, "", token)
	assertResource(t, recomputeDoc, "message-source-trust-reports", "current")
	if resourceAttribute(t, recomputeDoc, "sampleCount") != float64(1) {
		t.Fatalf("source trust recompute sampleCount mismatch: %+v", recomputeDoc)
	}
	evaluationDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/message-feedback/evaluation?subscriptionId="+subID, "", token)
	assertResource(t, evaluationDoc, "message-feedback-evaluations", "current")
	if resourceAttribute(t, evaluationDoc, "sampleCount") != float64(1) {
		t.Fatalf("feedback evaluation sampleCount mismatch: %+v", evaluationDoc)
	}
	snapshotDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/message-feedback/training-snapshots?subscriptionId="+subID, "", token)
	assertResourceType(t, snapshotDoc, "message-feedback-training-snapshots")
	if resourceAttribute(t, snapshotDoc, "sampleCount") != float64(1) {
		t.Fatalf("feedback snapshot sampleCount mismatch: %+v", snapshotDoc)
	}
	snapshotsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/message-feedback/training-snapshots?subscriptionId="+subID, "", token)
	assertCollection(t, snapshotsDoc, "message-feedback-training-snapshots")

	exportDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/message-feedback/training-exports?subscriptionId="+subID, "", token)
	assertResourceType(t, exportDoc, "message-feedback-training-exports")
	exportVersion := resourceID(t, exportDoc)
	if resourceAttribute(t, exportDoc, "sampleCount") != float64(1) || !strings.Contains(fmt.Sprint(resourceAttribute(t, exportDoc, "content")), `"feedbackLabel":"helpful"`) {
		t.Fatalf("feedback export payload mismatch: %+v", exportDoc)
	}
	exportsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/message-feedback/training-exports?subscriptionId="+subID, "", token)
	assertCollection(t, exportsDoc, "message-feedback-training-exports")
	if attrs := collectionResourceAttributes(t, exportsDoc, 0); attrs["content"] != nil {
		t.Fatalf("feedback export list should omit content: %+v", attrs)
	}
	loadedExportDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/message-feedback/training-exports/"+exportVersion, "", token)
	assertResource(t, loadedExportDoc, "message-feedback-training-exports", exportVersion)
	if resourceAttribute(t, loadedExportDoc, "contentSha256") == "" || resourceAttribute(t, loadedExportDoc, "content") == "" {
		t.Fatalf("feedback export read attributes mismatch: %+v", loadedExportDoc)
	}

	secondMessageDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/ingested-messages", `{"subscriptionId":`+subID+`,"sourceMessageId":102,"text":"second message","filterDecision":"ignore","filterReason":"manual","relatedSymbols":[]}`, token)
	secondMessageID := resourceID(t, secondMessageDoc)
	batchDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/ingested-messages/feedback/batch", `{"messageIds":[`+messageID+`,`+secondMessageID+`,999999],"label":"neutral","comment":"batch review"}`, token)
	assertResource(t, batchDoc, "ingested-message-feedback-batches", "current")
	if resourceAttribute(t, batchDoc, "requestedCount") != float64(3) || resourceAttribute(t, batchDoc, "updatedCount") != float64(2) {
		t.Fatalf("feedback batch counts mismatch: %+v", batchDoc)
	}

	filterMissing := requestJSONWithStatusAndToken(t, server, http.MethodPost, "/api/ingested-messages/999/filter", "", http.StatusNotFound, token)
	assertErrorCode(t, filterMissing, "ingested-message-not-found")

	deleteMessage := requestJSONWithToken(t, server, http.MethodDelete, "/api/ingested-messages/"+messageID, "", token)
	assertResourceType(t, deleteMessage, "deletions")

	adapterDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/platform-adapters", `{"displayName":"Fixture Bot","enabled":true,"botToken":"token","chatId":"chat"}`, token)
	assertResourceType(t, adapterDoc, "platform-adapters")
	adapterID := resourceID(t, adapterDoc)

	adaptersDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/platform-adapters", "", token)
	assertCollection(t, adaptersDoc, "platform-adapters")

	deleteAdapter := requestJSONWithToken(t, server, http.MethodDelete, "/api/platform-adapters/"+adapterID, "", token)
	assertResourceType(t, deleteAdapter, "deletions")
}

func TestIngestedMessageMeetingDecisionDispatchesCreatedMeeting(t *testing.T) {
	dispatched := 0
	server := newTestServerWithMeetingEnqueuer(t, "", func(uint) error {
		dispatched++
		return nil
	})
	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)
	teamID := createReadyResearchTeam(t, server, token)
	subDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/message-subscriptions", `{"title":"Fixture","sourceRef":"@fixture","enabled":true,"teamIds":[`+teamID+`],"backfillLimit":0}`, token)
	subID := resourceID(t, subDoc)

	requestJSONWithToken(t, server, http.MethodPost, "/api/ingested-messages", `{"subscriptionId":`+subID+`,"sourceMessageId":202,"text":"600000 triggered","filterDecision":"meeting","filterReason":"manual","relatedSymbols":["600000"]}`, token)

	if dispatched != 1 {
		t.Fatalf("expected one created meeting to be dispatched, got %d", dispatched)
	}
}

func TestIngestedMessageMeetingDecisionReportsDispatchFailure(t *testing.T) {
	server := newTestServerWithMeetingEnqueuer(t, "redis", func(uint) error {
		return errors.New("redis unavailable")
	})
	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)
	teamID := createReadyResearchTeam(t, server, token)
	subDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/message-subscriptions", `{"title":"Fixture","sourceRef":"@fixture","enabled":true,"teamIds":[`+teamID+`],"backfillLimit":0}`, token)
	subID := resourceID(t, subDoc)

	doc := requestJSONWithStatusAndToken(t, server, http.MethodPost, "/api/ingested-messages", `{"subscriptionId":`+subID+`,"sourceMessageId":202,"text":"600000 triggered","filterDecision":"meeting","filterReason":"manual","relatedSymbols":["600000"]}`, http.StatusBadGateway, token)

	assertErrorCode(t, doc, "meeting-dispatch-failed")
}

func TestPaperRiskConfigRoutesPersistBindAndUnbindAccounts(t *testing.T) {
	server := newTestServer(t)
	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)

	accountDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/paper/accounts", `{"data":{"type":"paper-accounts","attributes":{"name":"Fixture Account","cash":100000,"riskConfigId":null,"active":true}}}`, token)
	accountID := resourceID(t, accountDoc)

	inputCheck := riskConfigInput(map[string]any{"allowShMain": false, "allowEtfLof": false})
	if inputCheck.AllowSHMain == nil || *inputCheck.AllowSHMain || inputCheck.AllowETFLOF == nil || *inputCheck.AllowETFLOF {
		t.Fatalf("riskConfigInput did not parse explicit false booleans: %+v", inputCheck)
	}

	configDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/paper/risk-configs", `{"data":{"type":"paper-risk-configs","attributes":{"name":"Fixture Risk","initialCash":100000,"maxPositionPct":0.25,"maxOrderPct":0.1,"allowShort":false,"allowMargin":false,"allowShMain":false,"allowSzMain":true,"allowBj":false,"allowStar":false,"allowChinext":false,"allowEtfLof":false,"commissionRate":0,"minCommission":0,"stampDutyRate":0,"transferFeeRate":0,"enabled":false,"accountIds":[`+accountID+`]}}}`, token)
	assertResourceType(t, configDoc, "paper-risk-configs")
	configID := resourceID(t, configDoc)
	configAttrs := resourceAttributes(t, configDoc)
	if configAttrs["allowShMain"] != false || configAttrs["allowEtfLof"] != false || configAttrs["enabled"] != false {
		t.Fatalf("explicit false risk config fields were not persisted: %+v", configAttrs)
	}
	assertJSONAttrText(t, configAttrs, "initialCash", "100000", "100000.00")
	assertJSONAttrText(t, configAttrs, "maxPositionPct", "0.25", "0.2500")
	assertJSONAttrText(t, configAttrs, "maxOrderPct", "0.1", "0.10", "0.1000")
	assertJSONAttrText(t, configAttrs, "commissionRate", "0", "0.000000")
	if configAttrs["accountCount"] != float64(1) {
		t.Fatalf("accountCount = %#v, want 1", configAttrs["accountCount"])
	}

	configsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/paper/risk-configs", "", token)
	configs := configsDoc["data"].([]any)
	var listed map[string]any
	for _, item := range configs {
		resource := item.(map[string]any)
		if resource["id"] == configID {
			listed = resource
			break
		}
	}
	if listed == nil {
		t.Fatalf("created risk config %s not found in list: %+v", configID, configsDoc)
	}
	listedAttrs := listed["attributes"].(map[string]any)
	if listedAttrs["name"] != "Fixture Risk" || listedAttrs["allowShMain"] != false || listedAttrs["allowEtfLof"] != false {
		t.Fatalf("listed risk config attributes are not the configured camelCase values: %+v", listedAttrs)
	}
	assertJSONAttrText(t, listedAttrs, "initialCash", "100000", "100000.00")
	assertJSONAttrText(t, listedAttrs, "maxPositionPct", "0.25", "0.2500")
	assertJSONAttrText(t, listedAttrs, "maxOrderPct", "0.1", "0.10", "0.1000")
	assertJSONAttrText(t, listedAttrs, "minCommission", "0", "0.00")
	if accountIDs, ok := listedAttrs["accountIds"].([]any); !ok || len(accountIDs) != 1 || fmt.Sprint(accountIDs[0]) != accountID {
		t.Fatalf("listed accountIds = %#v, want [%s]", listedAttrs["accountIds"], accountID)
	}
	for _, leaked := range []string{"Name", "InitialCash", "MaxPositionPct", "AllowSHMain", "AllowETFLOF"} {
		if _, ok := listedAttrs[leaked]; ok {
			t.Fatalf("listed risk config leaked Go struct field %s: %+v", leaked, listedAttrs)
		}
	}
	if _, ok := listedAttrs["initial_cash"]; ok {
		t.Fatalf("listed risk config leaked Go struct field names: %+v", listedAttrs)
	}

	deleteDoc := requestJSONWithToken(t, server, http.MethodDelete, "/api/paper/risk-configs/"+configID, "", token)
	deleteAttrs := resourceAttributes(t, deleteDoc)
	if deleteAttrs["unboundAccountCount"] != float64(1) {
		t.Fatalf("unboundAccountCount = %#v, want 1", deleteAttrs["unboundAccountCount"])
	}
	accountsDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/paper/accounts", "", token)
	for _, item := range accountsDoc["data"].([]any) {
		resource := item.(map[string]any)
		if resource["id"] != accountID {
			continue
		}
		attrs := resource["attributes"].(map[string]any)
		if attrs["riskConfigId"] != nil {
			t.Fatalf("riskConfigId after delete = %#v, want nil", attrs["riskConfigId"])
		}
		return
	}
	t.Fatalf("account %s not found after delete: %+v", accountID, accountsDoc)
}

func TestPaperBacktestRouteReturnsJSONAPIErrorWhenDailyBarsAreMissing(t *testing.T) {
	server := newTestServer(t)
	tokenDoc := requestJSON(t, server, http.MethodPost, "/api/auth/bootstrap", `{"username":"admin","password":"password123"}`)
	token := resourceAttribute(t, tokenDoc, "accessToken").(string)
	accountDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/paper/accounts", `{"data":{"type":"paper-accounts","attributes":{"name":"Backtest Account","cash":100000,"riskConfigId":null,"active":true}}}`, token)
	accountID := resourceID(t, accountDoc)

	doc := requestJSONWithStatusAndToken(t, server, http.MethodPost, "/api/paper/accounts/"+accountID+"/backtests", `{"data":{"type":"paper-backtests","attributes":{"code":"600519","startDate":"2026-06-01T00:00:00Z","endDate":"2026-06-30T00:00:00Z","initialCash":100000,"buyThresholdPct":-3,"sellThresholdPct":3,"orderPct":0.1,"slippageBps":5}}}`, http.StatusBadRequest, token)

	assertErrorCode(t, doc, "paper-backtest-run-failed")
}

func TestPlatformAdapterTestResourceMapsTelegramMessageID(t *testing.T) {
	resource := platformAdapterTestResource(map[string]any{"status": "sent", "chat_id": "chat", "message_id": float64(42)})
	if resource.Type != "platform-adapter-test-results" {
		t.Fatalf("type = %q", resource.Type)
	}
	attrs, ok := resource.Attributes.(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v", resource.Attributes)
	}
	if attrs["messageId"] != float64(42) {
		t.Fatalf("messageId = %#v", attrs["messageId"])
	}
}

func TestMessageSubscriptionLoginSessionUsesProviderNeutralResource(t *testing.T) {
	resource := messageSubscriptionLoginSessionResource(map[string]any{"status": "stored"})
	if resource.Type != "message-subscription-login-sessions" || resource.ID != "telegram" {
		t.Fatalf("unexpected login session resource: %+v", resource)
	}
}

func newTestServer(t *testing.T) *Server {
	return newTestServerWithMeetingEnqueuer(t, "", func(uint) error { return nil })
}

func newTestServerWithMeetingEnqueuer(t *testing.T, meetingDispatchMode string, enqueueMeeting appmeeting.EnqueueMeeting) *Server {
	t.Helper()
	return newTestServerFixtureWithMeetingEnqueuer(t, meetingDispatchMode, enqueueMeeting).Server
}

type testServerFixture struct {
	Server *Server
	DB     *gorm.DB
}

func newTestServerFixtureWithMeetingEnqueuer(t *testing.T, meetingDispatchMode string, enqueueMeeting appmeeting.EnqueueMeeting) testServerFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	settings := config.Settings{
		AppName:                  "transport-test",
		AppEnv:                   "test",
		AppSecretKey:             "transport-secret",
		JWTSecretKey:             "jwt-secret",
		AccessTokenExpireMinutes: 60,
		CORSOrigins:              []string{"*"},
		FrontendDist:             "frontend/dist",
		MeetingDispatchMode:      meetingDispatchMode,
		LogDir:                   t.TempDir(),
		RuntimeEnvFile:           filepath.Join(t.TempDir(), ".env"),
	}
	sec := security.New(settings)
	server := New(Dependencies{
		Settings:  transportSettings(settings),
		Auth:      appauth.NewUsecase(gormrepo.NewAuthRepository(db), sec, gormuow.NewAuthUnitOfWork(db)),
		AI:        appai.NewUsecase(gormrepo.NewAIRepository(db), sec, infraai.NewModelSyncer(settings, sec, runtimeproxy.NewHTTPClient(db, settings, runtimeproxy.ModuleAI, 0)), gormuow.NewAIUnitOfWork(db)),
		Dashboard: appdashboard.NewUsecase(infradashboard.NewLoader(db, settings), appdashboard.Settings{AppName: settings.AppName, AppEnv: settings.AppEnv}),
		Market:    appmarket.NewUsecase(gormrepo.NewMarketRepository(db), inframarketdata.NewService(settings, gormmarketdata.NewStore(db, settings, sec)), gormuow.NewMarketUnitOfWork(db, settings)),
		Meeting: appmeeting.NewUsecase(gormrepo.NewMeetingRepository(db), inframeeting.NewService(db), appmeeting.Settings{
			MeetingDispatchMode: settings.MeetingDispatchMode,
		}, gormuow.NewMeetingUnitOfWork(db)).WithMeetingEnqueuer(enqueueMeeting),
		Messaging: appmessaging.NewUsecase(gormrepo.NewMessagingRepository(db), inframessaging.NewService(settings), sec, gormuow.NewMessagingUnitOfWork(db, settings)),
		Paper:     apppaper.NewUsecase(gormrepo.NewPaperRepository(db), infrapaper.NewService(db), gormuow.NewPaperUnitOfWork(db)),
		Research:  appresearch.NewUsecase(gormrepo.NewResearchRepository(db), gormuow.NewResearchUnitOfWork(db)),
		AppConfig: appsettings.NewUsecase(gormrepo.NewSettingsRepository(db), sec, appRuntimeSettings(settings), gormuow.NewSettingsUnitOfWork(db), config.WriteEnvOverrides).WithProxyTester(infraproxy.NewTester(db, settings, sec)),
		Wake:      appwake.NewUsecase(gormrepo.NewWakeRepository(db), inframarketdata.NewService(settings, gormmarketdata.NewStore(db, settings, sec)), gormuow.NewWakeUnitOfWork(db, settings)),
	})
	return testServerFixture{Server: server, DB: db}
}

func createReadyResearchTeam(t *testing.T, server *Server, token string) string {
	t.Helper()
	accountDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/paper/accounts", `{"name":"Team Account `+t.Name()+`","initialCash":1000000,"active":true}`, token)
	accountID := resourceID(t, accountDoc)
	teamDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/research-teams", `{"name":"Team `+t.Name()+`","description":"","paperAccountId":`+accountID+`,"active":true}`, token)
	teamID := resourceID(t, teamDoc)
	requestJSONWithToken(t, server, http.MethodPost, "/api/research-teams/"+teamID+"/roles/apply-defaults", "", token)
	return teamID
}

func transportSettings(settings config.Settings) Settings {
	return Settings{
		AppName:                    settings.AppName,
		AppEnv:                     settings.AppEnv,
		CORSOrigins:                settings.CORSOrigins,
		FrontendDist:               settings.FrontendDist,
		DatabaseURL:                settings.DatabaseURL,
		RedisURL:                   settings.RedisURL,
		MeetingMode:                settings.MeetingDispatchMode,
		LogDir:                     settings.LogDir,
		RuntimeEnvFile:             settings.RuntimeEnvFile,
		BackupCopies:               settings.BackupRetentionCopies,
		BackupDays:                 settings.BackupRetentionDays,
		BackupRestoreDrillInterval: settings.BackupRestoreDrillInterval,
	}
}

func appRuntimeSettings(settings config.Settings) appsettings.RuntimeSettings {
	return appsettings.RuntimeSettings{
		DatabaseURL:                  settings.DatabaseURL,
		RedisURL:                     settings.RedisURL,
		PublicBaseURL:                settings.PublicBaseURL,
		DefaultMarketProvider:        settings.DefaultMarketProvider,
		MarketRealtimeProvider:       settings.MarketRealtimeProvider,
		MarketRealtimeCompatProvider: settings.MarketRealtimeCompatProvider,
		MarketRealtimeCacheTTL:       settings.MarketRealtimeCacheTTL,
		MeetingMaxRounds:             settings.MeetingMaxRounds,
		MeetingDailyTokenBudget:      settings.MeetingDailyTokenBudget,
		ToolResultLimit:              settings.ToolResultLimit,
		SQLStatementTimeoutMillis:    settings.SQLStatementTimeoutMillis,
		RuntimeEnvFile:               settings.RuntimeEnvFile,
	}
}

func requestJSON(t *testing.T, server *Server, method string, path string, body string) map[string]any {
	t.Helper()
	return requestJSONWithStatus(t, server, method, path, body, http.StatusOK)
}

func requestJSONWithToken(t *testing.T, server *Server, method string, path string, body string, token string) map[string]any {
	t.Helper()
	return requestJSONWithStatusAndToken(t, server, method, path, body, http.StatusOK, token)
}

func requestJSONWithStatus(t *testing.T, server *Server, method string, path string, body string, status int) map[string]any {
	t.Helper()
	return requestJSONWithStatusAndToken(t, server, method, path, body, status, "")
}

func requestRawJSONWithStatus(t *testing.T, server *Server, method string, path string, body string, status int) map[string]any {
	t.Helper()
	return requestRawJSONWithStatusAndToken(t, server, method, path, body, status, "")
}

func requestRawJSONWithStatusAndToken(t *testing.T, server *Server, method string, path string, body string, status int, token string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if strings.TrimSpace(body) != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	server.Router().ServeHTTP(rec, req)

	if rec.Code != status {
		t.Fatalf("%s %s status = %d, want %d body=%s", method, path, rec.Code, status, rec.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func requestJSONWithStatusAndToken(t *testing.T, server *Server, method string, path string, body string, status int, token string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	body = jsonapiTestBody(body)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if strings.TrimSpace(body) != "" {
		req.Header.Set("Content-Type", "application/vnd.api+json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	server.Router().ServeHTTP(rec, req)

	if rec.Code != status {
		t.Fatalf("%s %s status = %d, want %d body=%s", method, path, rec.Code, status, rec.Body.String())
	}
	var doc map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func jsonapiTestBody(body string) string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" || strings.Contains(trimmed, `"data"`) {
		return body
	}
	return `{"data":{"type":"test-requests","attributes":` + body + `}}`
}

func assertResource(t *testing.T, doc map[string]any, resourceType string, id string) {
	t.Helper()
	data, ok := doc["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing data resource: %+v", doc)
	}
	if data["type"] != resourceType || data["id"] != id {
		t.Fatalf("unexpected resource: %+v", data)
	}
}

func assertResourceType(t *testing.T, doc map[string]any, resourceType string) {
	t.Helper()
	data, ok := doc["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing data resource: %+v", doc)
	}
	if data["type"] != resourceType {
		t.Fatalf("unexpected resource: %+v", data)
	}
}

func resourceID(t *testing.T, doc map[string]any) string {
	t.Helper()
	data, ok := doc["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing data resource: %+v", doc)
	}
	id, ok := data["id"].(string)
	if !ok || id == "" {
		t.Fatalf("missing resource id: %+v", data)
	}
	return id
}

func resourceIDFromCollection(t *testing.T, doc map[string]any, username string) string {
	t.Helper()
	rows, ok := doc["data"].([]any)
	if !ok {
		t.Fatalf("missing collection data: %+v", doc)
	}
	for _, item := range rows {
		resource, ok := item.(map[string]any)
		if !ok {
			continue
		}
		attrs, _ := resource["attributes"].(map[string]any)
		if attrs["username"] == username {
			id, _ := resource["id"].(string)
			if id != "" {
				return id
			}
		}
	}
	t.Fatalf("resource for username %q not found: %+v", username, doc)
	return ""
}

func findAuditEvent(t *testing.T, doc map[string]any, action string, resourceType string) map[string]any {
	t.Helper()
	rows, ok := doc["data"].([]any)
	if !ok {
		t.Fatalf("missing collection data: %+v", doc)
	}
	for _, item := range rows {
		resource, ok := item.(map[string]any)
		if !ok {
			continue
		}
		attrs, _ := resource["attributes"].(map[string]any)
		if attrs["action"] == action && attrs["resourceType"] == resourceType {
			return attrs
		}
	}
	t.Fatalf("audit event %s/%s not found: %+v", action, resourceType, doc)
	return nil
}

func assertCollection(t *testing.T, doc map[string]any, resourceType string) {
	t.Helper()
	data, ok := doc["data"].([]any)
	if !ok || len(data) == 0 {
		t.Fatalf("missing collection data: %+v", doc)
	}
	first, ok := data[0].(map[string]any)
	if !ok || first["type"] != resourceType {
		t.Fatalf("unexpected collection item: %+v", data[0])
	}
}

func assertEmptyOrCollection(t *testing.T, doc map[string]any, resourceType string) {
	t.Helper()
	data, ok := doc["data"].([]any)
	if !ok {
		t.Fatalf("missing collection data: %+v", doc)
	}
	if len(data) == 0 {
		return
	}
	first, ok := data[0].(map[string]any)
	if !ok || first["type"] != resourceType {
		t.Fatalf("unexpected collection item: %+v", data[0])
	}
}

func collectionResourceAttributes(t *testing.T, doc map[string]any, index int) map[string]any {
	t.Helper()
	data, ok := doc["data"].([]any)
	if !ok || index < 0 || index >= len(data) {
		t.Fatalf("missing collection item %d: %+v", index, doc)
	}
	resource, ok := data[index].(map[string]any)
	if !ok {
		t.Fatalf("unexpected collection item %d: %+v", index, data[index])
	}
	attrs, ok := resource["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("missing collection item attributes %d: %+v", index, resource)
	}
	return attrs
}

func collectionResourceByAttribute(t *testing.T, doc map[string]any, key string, value string) map[string]any {
	t.Helper()
	data, ok := doc["data"].([]any)
	if !ok {
		t.Fatalf("missing collection data: %+v", doc)
	}
	for _, item := range data {
		resource, ok := item.(map[string]any)
		if !ok {
			continue
		}
		attrs, _ := resource["attributes"].(map[string]any)
		if fmt.Sprint(attrs[key]) == value {
			return attrs
		}
	}
	t.Fatalf("collection resource with %s=%q not found: %+v", key, value, doc)
	return nil
}

func firstCollectionResourceID(t *testing.T, doc map[string]any) string {
	t.Helper()
	data, ok := doc["data"].([]any)
	if !ok || len(data) == 0 {
		t.Fatalf("missing collection data: %+v", doc)
	}
	resource, ok := data[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected first collection item: %+v", data[0])
	}
	id, ok := resource["id"].(string)
	if !ok || id == "" {
		t.Fatalf("missing first collection resource id: %+v", resource)
	}
	return id
}

func resourceAttribute(t *testing.T, doc map[string]any, name string) any {
	t.Helper()
	return resourceAttributes(t, doc)[name]
}

func resourceAttributes(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	data, ok := doc["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing data resource: %+v", doc)
	}
	attrs, ok := data["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("missing attributes: %+v", data)
	}
	return attrs
}

func parseResourceUintID(t *testing.T, doc map[string]any) uint {
	t.Helper()
	return parseUintString(t, resourceID(t, doc))
}

func parseUintString(t *testing.T, value string) uint {
	t.Helper()
	parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	if err != nil {
		t.Fatalf("invalid uint id %q: %v", value, err)
	}
	return uint(parsed)
}

func setupStepByKey(t *testing.T, doc map[string]any, key string) map[string]any {
	t.Helper()
	attrs := resourceAttributes(t, doc)
	rows, ok := attrs["steps"].([]any)
	if !ok {
		t.Fatalf("setup readiness missing steps: %+v", attrs)
	}
	for _, item := range rows {
		step, ok := item.(map[string]any)
		if ok && step["key"] == key {
			return step
		}
	}
	t.Fatalf("setup step %q not found: %+v", key, rows)
	return nil
}

func setupActionOutput(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	attrs := resourceAttributes(t, doc)
	output, ok := attrs["output"].(map[string]any)
	if !ok {
		t.Fatalf("setup action missing output: %+v", attrs)
	}
	return output
}

func collectionLen(t *testing.T, doc map[string]any) int {
	t.Helper()
	data, ok := doc["data"].([]any)
	if !ok {
		t.Fatalf("missing collection data: %+v", doc)
	}
	return len(data)
}

func assertJSONAttrText(t *testing.T, attrs map[string]any, key string, wants ...string) {
	t.Helper()
	got := fmt.Sprint(attrs[key])
	for _, want := range wants {
		if got == want {
			return
		}
	}
	t.Fatalf("%s = %#v, want one of %v", key, attrs[key], wants)
}

func assertErrorCode(t *testing.T, doc map[string]any, code string) {
	t.Helper()
	errorsValue, ok := doc["errors"].([]any)
	if !ok || len(errorsValue) == 0 {
		t.Fatalf("missing errors: %+v", doc)
	}
	first, ok := errorsValue[0].(map[string]any)
	if !ok || first["code"] != code {
		t.Fatalf("unexpected error: %+v", errorsValue[0])
	}
}
