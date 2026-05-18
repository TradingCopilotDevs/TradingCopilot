package httptransport

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/ai"
	appauth "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/auth"
	appdashboard "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/dashboard"
	appmarket "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/market"
	appmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/meeting"
	appmessaging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/messaging"
	apppaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/paper"
	appresearch "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/research"
	appsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/settings"
	appwake "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/wake"
	infraai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/ai"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	inframarketdata "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/marketdata"
	inframessaging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/messaging"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/connect"
	infradashboard "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/dashboard"
	gormmarketdata "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/marketdata"
	inframeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/meeting"
	infrapaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/paper"
	infraproxy "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/proxy"
	runtimeproxy "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	gormuow "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/uow"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

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

	unauthorized := requestJSONWithStatus(t, server, http.MethodPost, "/api/auth/login", `{"username":"admin","password":"bad"}`, http.StatusUnauthorized)
	assertErrorCode(t, unauthorized, "invalid-credentials")
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

	updatedSub := requestJSONWithToken(t, server, http.MethodPut, "/api/message-subscriptions/"+subID, `{"title":"Fixture Updated","enabled":false,"backfillLimit":0}`, token)
	assertResource(t, updatedSub, "message-subscriptions", subID)

	messageDoc := requestJSONWithToken(t, server, http.MethodPost, "/api/ingested-messages", `{"subscriptionId":`+subID+`,"sourceMessageId":101,"text":"message body","filterDecision":"observe","filterReason":"manual","relatedSymbols":["600000"]}`, token)
	assertResourceType(t, messageDoc, "ingested-messages")
	messageID := resourceID(t, messageDoc)

	messagesDoc := requestJSONWithToken(t, server, http.MethodGet, "/api/ingested-messages?subscriptionId="+subID, "", token)
	assertCollection(t, messagesDoc, "ingested-messages")

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
	}
	sec := security.New(settings)
	return New(Dependencies{
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
		AppName:      settings.AppName,
		AppEnv:       settings.AppEnv,
		CORSOrigins:  settings.CORSOrigins,
		FrontendDist: settings.FrontendDist,
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
