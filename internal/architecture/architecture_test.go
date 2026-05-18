package architecture

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type goPackage struct {
	ImportPath string
	Imports    []string
	Dir        string
}

func TestLayerImportBoundaries(t *testing.T) {
	packages := listPackages(t)
	for _, pkg := range packages {
		switch {
		case strings.Contains(pkg.ImportPath, "/internal/domain"):
			rejectImports(t, pkg, "gorm.io/", "net/http", "/internal/infra/")
		case strings.Contains(pkg.ImportPath, "/internal/app/"):
			rejectImports(t, pkg, "gorm.io/", "net/http", "/internal/infra/")
		case strings.HasSuffix(pkg.ImportPath, "/internal/transport/http"):
			rejectImports(t, pkg, "gorm.io/", "/internal/infra/", "/internal/infra/queue")
		}
	}
}

func TestLegacyAndRootInfrastructurePackagesStayRemoved(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		"internal/config",
		"internal/security",
		"internal/database",
		"internal/ai",
		"internal/runtimeproxy",
		"internal/ashare",
		"internal/jobs",
		"internal/httpapi",
		"internal/service",
		"internal/market",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err == nil {
			t.Fatalf("legacy/root infrastructure package still exists: %s", rel)
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", rel, err)
		}
	}
}

func TestPersistenceRootOnlyContainsGORMBoundary(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "internal", "infra", "persistence"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if entry.Name() != "gorm" {
			t.Fatalf("internal/infra/persistence contains non-GORM boundary package %s; DB-backed code must live under internal/infra/persistence/gorm", entry.Name())
		}
	}
}

func TestGORMPersistenceContainsRequiredCorePackages(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "internal", "infra", "persistence", "gorm"))
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		"connect": true,
		"migrate": true,
		"model":   true,
		"repo":    true,
		"uow":     true,
	}
	_ = entries
	for name := range allowed {
		if _, err := os.Stat(filepath.Join(root, "internal", "infra", "persistence", "gorm", name)); err != nil {
			t.Fatalf("expected core gorm persistence package %s: %v", name, err)
		}
	}
}

func TestOnlyGORMPersistenceBoundaryUsesGORM(t *testing.T) {
	root := repoRoot(t)
	forbidden := regexp.MustCompile(`gorm\.io/gorm|\*gorm\.DB|\bdb\.(Create|Save|Updates|Update|Delete|Transaction|Where|First|Find|Model|Exec|Raw)\(`)
	for _, rootRel := range []string{"internal/app", "internal/infra", "internal/transport/http", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, rootRel), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			slash := filepath.ToSlash(rel)
			if strings.HasPrefix(slash, "internal/infra/persistence/gorm/") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if match := forbidden.Find(raw); len(match) > 0 {
				t.Fatalf("%s uses direct GORM/DB access via %q; route persistence through internal/infra/persistence/gorm", rel, string(match))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestDomainModelsLiveInDomainSubpackages(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		"internal/domain/auth/models.go",
		"internal/domain/settings/models.go",
		"internal/domain/ai/models.go",
		"internal/domain/market/models.go",
		"internal/domain/telegram/models.go",
		"internal/domain/meeting/models.go",
		"internal/domain/wake/models.go",
		"internal/domain/paper/models.go",
		"internal/domain/kernel/types.go",
		"internal/domain/kernel/json.go",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("expected split domain file %s: %v", rel, err)
		}
	}
	matches, err := filepath.Glob(filepath.Join(root, "internal", "domain", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) > 0 {
		rel, _ := filepath.Rel(root, matches[0])
		t.Fatalf("%s keeps the root domain compatibility package alive; import concrete domain subpackages or domain/kernel directly", rel)
	}
}

func TestDomainModelsDoNotExposePersistenceOrTransportMetadata(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range mustGlob(t, filepath.Join(root, "internal", "domain", "**", "*.go")) {
		if strings.HasSuffix(rel, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(rel)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if strings.Contains(text, "TableName()") {
			display, _ := filepath.Rel(root, rel)
			t.Fatalf("%s defines TableName; table names belong in infra/persistence/gorm/model", display)
		}
		if strings.Contains(text, "`json:") {
			display, _ := filepath.Rel(root, rel)
			t.Fatalf("%s has json tags; transport serialization belongs in DTO/resource mapping", display)
		}
	}
}

func TestPersistenceModelsDoNotExposeTransportMetadata(t *testing.T) {
	root := repoRoot(t)
	for _, path := range mustGlob(t, filepath.Join(root, "internal", "infra", "persistence", "gorm", "model", "*.go")) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "`json:") {
			display, _ := filepath.Rel(root, path)
			t.Fatalf("%s has json tags; persistence models must not own transport serialization", display)
		}
	}
}

func TestTransportRoutesThroughGeneratedOpenAPIWrapper(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "internal", "transport", "http", "telegram.go")); err == nil {
		t.Fatalf("internal/transport/http/telegram.go remains; legacy Telegram HTTP handlers must not survive outside the OpenAPI wrapper")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "internal", "transport", "http", "server.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "openapi.HandlerWithOptions") {
		t.Fatalf("internal/transport/http/server.go must register /api routes through the generated OpenAPI wrapper")
	}
	if strings.Contains(text, `.Route("/api/`) || strings.Contains(text, `.Get("/api/`) || strings.Contains(text, `.Post("/api/`) || strings.Contains(text, `.Put("/api/`) || strings.Contains(text, `.Delete("/api/`) {
		t.Fatalf("internal/transport/http/server.go still contains hand-registered /api routes")
	}
}

func TestTransportRequestDecodersStayJSONAPIOnly(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "internal", "transport", "http", "auth.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"decodeJSONAPIOrLegacy", "decodeJSONAPIAttributesOrLegacyMap"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("transport still exposes legacy request decoder %s", forbidden)
		}
	}
	if !strings.Contains(text, "invalid-jsonapi-request") {
		t.Fatalf("transport request decoder must reject non-JSON:API request bodies")
	}
	if strings.Contains(text, `contentType == ""`) {
		t.Fatalf("transport request decoder must not accept request bodies without JSON:API Content-Type")
	}
	if !strings.Contains(text, "unsupported-media-type") {
		t.Fatalf("transport request decoder must reject non-JSON:API media types")
	}
}

func TestOpenAPIRequestBodiesStayJSONAPIOnly(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"JsonApiRequest", "application/json"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("api/openapi.yaml still exposes legacy request body contract %q", forbidden)
		}
	}
}

func TestOpenAPIParameterNamesStayCamelCase(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	pathParam := regexp.MustCompile(`\{([^}]+)\}`)
	for _, match := range pathParam.FindAllStringSubmatch(text, -1) {
		if strings.Contains(match[1], "_") {
			t.Fatalf("api/openapi.yaml path parameter %q is snake_case; use camelCase URL parameter names", match[1])
		}
	}
	namedParam := regexp.MustCompile(`(?m)^\s+name:\s*([A-Za-z0-9_\[\]]+)\s*$`)
	for _, match := range namedParam.FindAllStringSubmatch(text, -1) {
		if strings.Contains(match[1], "_") {
			t.Fatalf("api/openapi.yaml parameter name %q is snake_case; use camelCase parameters except page[...] pagination", match[1])
		}
	}
}

func TestMessagingOpenAPIUsesProviderNeutralResources(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{
		"message-subscription-app-configs",
		"message-subscription-login-sessions",
		"message-subscriptions",
		"message-subscription-tests",
		"message-subscription-collect-results",
		"platform-adapters",
		"platform-adapter-test-results",
		"ingested-messages",
		"rss_feed",
		"pollIntervalSeconds",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("api/openapi.yaml does not declare provider-neutral messaging resource %q", required)
		}
	}
	for _, forbidden := range []string{
		"telegram-app-configs",
		"telegram-bot-configs",
		"telegram-channels",
		"telegram-channel-tests",
		"telegram-channel-collect-results",
		"telegram-bot-test-results",
		"telegram-login-sessions",
		"telegram-messages",
		"telegram-message-refilters",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("api/openapi.yaml still declares legacy Telegram messaging resource %q", forbidden)
		}
	}
}

func TestGeneratedFrontendTypesUseProviderNeutralMessagingResources(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "frontend", "src", "generated", "api-types.ts"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "message-subscription-login-sessions") {
		t.Fatalf("frontend generated API types do not include provider-neutral message subscription login sessions")
	}
	if strings.Contains(text, "telegram-login-sessions") {
		t.Fatalf("frontend generated API types still expose legacy telegram-login-sessions")
	}
}

func TestMessageFiltersAreNotResearchTeamRoles(t *testing.T) {
	root := repoRoot(t)
	roleDefaults, err := os.ReadFile(filepath.Join(root, "internal", "app", "ai", "role_defaults.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(roleDefaults), `Key:  "news_filter"`) || strings.Contains(string(roleDefaults), `Name: "消息过滤器"`) {
		t.Fatalf("message filter must not be seeded as a research-team AI role")
	}
	messagingDefaults, err := os.ReadFile(filepath.Join(root, "internal", "app", "messaging", "usecase.go"))
	if err != nil {
		t.Fatal(err)
	}
	messagingText := string(messagingDefaults)
	for _, required := range []string{"DefaultFilterSeed", "默认消息过滤器", "related_symbols"} {
		if !strings.Contains(messagingText, required) {
			t.Fatalf("messaging defaults must contain %q", required)
		}
	}

	router, err := os.ReadFile(filepath.Join(root, "frontend", "src", "router.ts"))
	if err != nil {
		t.Fatal(err)
	}
	routerText := string(router)
	for _, required := range []string{"/model-providers", "/research-team"} {
		if !strings.Contains(routerText, required) {
			t.Fatalf("frontend router does not expose split model configuration route %q", required)
		}
	}
	for _, forbidden := range []string{"AiView", "path: '/ai'"} {
		if strings.Contains(routerText, forbidden) {
			t.Fatalf("frontend router still exposes old combined AI page via %q", forbidden)
		}
	}

	openapiRaw, err := os.ReadFile(filepath.Join(root, "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(openapiRaw), "message-subscription-filters") {
		t.Fatalf("OpenAPI must expose message-subscription-filters as the message-domain filter resource")
	}
}

func TestResearchTeamContractIsExplicit(t *testing.T) {
	root := repoRoot(t)
	openapiRaw, err := os.ReadFile(filepath.Join(root, "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	openapiText := string(openapiRaw)
	for _, required := range []string{"research-teams", "research-team-roles", "researchTeamId", "teamIds", "paperAccountId"} {
		if !strings.Contains(openapiText, required) {
			t.Fatalf("OpenAPI must declare research team contract field %q", required)
		}
	}

	modelRaw, err := os.ReadFile(filepath.Join(root, "internal", "infra", "persistence", "gorm", "model", "research.go"))
	if err != nil {
		t.Fatal(err)
	}
	modelText := string(modelRaw)
	for _, required := range []string{"uniqueIndex", "message_subscription_research_teams", "research_team_roles"} {
		if !strings.Contains(modelText, required) {
			t.Fatalf("research persistence model must declare %q", required)
		}
	}

	runnerRaw, err := os.ReadFile(filepath.Join(root, "internal", "infra", "persistence", "gorm", "meeting", "meeting.go"))
	if err != nil {
		t.Fatal(err)
	}
	runnerText := string(runnerRaw)
	if !strings.Contains(runnerText, "persistmodel.ResearchTeamRole") || strings.Contains(runnerText, "persistmodel.AgentRole") {
		t.Fatalf("meeting runner must load team roles and must not read global agent roles")
	}

	researchUsecase, err := os.ReadFile(filepath.Join(root, "internal", "app", "research", "usecase.go"))
	if err != nil {
		t.Fatal(err)
	}
	researchText := string(researchUsecase)
	for _, required := range []string{"DefaultTeamName", "EnsureDefaultTeam", "createDefaultRole", "DeleteRoles(ctx, teamID)"} {
		if !strings.Contains(researchText, required) {
			t.Fatalf("research usecase must declare default team bootstrap behavior %q", required)
		}
	}
}

func TestPaperRiskConfigContractIncludesAccountBinding(t *testing.T) {
	root := repoRoot(t)
	openapiRaw, err := os.ReadFile(filepath.Join(root, "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	openapiText := string(openapiRaw)
	for _, required := range []string{"accountIds:", "accountCount:", "unboundAccountIds:", "unboundAccountCount:"} {
		if !strings.Contains(openapiText, required) {
			t.Fatalf("api/openapi.yaml does not declare paper risk config binding field %q", required)
		}
	}
	frontendRaw, err := os.ReadFile(filepath.Join(root, "frontend", "src", "generated", "api-types.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(frontendRaw), "accountIds?") {
		t.Fatalf("frontend generated API types do not include paper risk config accountIds")
	}
}

func TestTransportQueryParamsAreDeclaredInOpenAPI(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	declared := map[string]bool{}
	namePattern := regexp.MustCompile(`(?m)^\s+name:\s*([A-Za-z0-9_\[\]]+)\s*$`)
	for _, match := range namePattern.FindAllStringSubmatch(string(raw), -1) {
		declared[match[1]] = true
	}
	declared["token"] = true

	queryGet := regexp.MustCompile(`URL\.Query\(\)\.Get\("([^"]+)"\)`)
	err = filepath.WalkDir(filepath.Join(root, "internal", "transport", "http"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(filepath.ToSlash(path), "/internal/transport/http/openapi/") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for lineNumber, line := range strings.Split(string(raw), "\n") {
			for _, match := range queryGet.FindAllStringSubmatch(line, -1) {
				if !declared[match[1]] {
					rel, _ := filepath.Rel(root, path)
					t.Fatalf("%s:%d reads undocumented query parameter %q", rel, lineNumber+1, match[1])
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTransportRequestAttributeDecodersDoNotAcceptSnakeCaseAliases(t *testing.T) {
	root := repoRoot(t)
	snakeAttributeDecoder := regexp.MustCompile(`\b(stringAttr|attrValue|numberAttrValue|uintPtrAttr|timePtrAttr|boolAttr|boolAttrWithDefault)\s*\(\s*attrs\s*,[^\n]*"[^"]*_[^"]*"`)
	err := filepath.WalkDir(filepath.Join(root, "internal", "transport", "http"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(filepath.ToSlash(path), "/internal/transport/http/openapi/") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for lineNumber, line := range strings.Split(string(raw), "\n") {
			if match := snakeAttributeDecoder.FindString(line); match != "" {
				rel, _ := filepath.Rel(root, path)
				t.Fatalf("%s:%d accepts a snake_case request attribute alias via %q", rel, lineNumber+1, match)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestTransportDoesNotTranslateRequestAttributesToSnakeCase(t *testing.T) {
	root := repoRoot(t)
	for _, path := range mustGlob(t, filepath.Join(root, "internal", "transport", "http", "*.go")) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, forbidden := range []string{"snakeizeMapKeys", "paperPayloadSnakeKey", "lowerCamelToSnake"} {
			if strings.Contains(text, forbidden) {
				display, _ := filepath.Rel(root, path)
				t.Fatalf("%s still translates request attributes to snake_case via %q; keep JSON:API attributes camelCase at transport boundary", display, forbidden)
			}
		}
	}
}

func TestFrontendDoesNotUseSnakeCaseJSONAPITransforms(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		filepath.Join("frontend", "src", "api.ts"),
		filepath.Join("frontend", "src", "views"),
	} {
		path := filepath.Join(root, rel)
		err := filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || (!strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".vue")) {
				return nil
			}
			if strings.Contains(filepath.ToSlash(path), "/frontend/src/generated/") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			text := string(raw)
			for _, forbidden := range []string{"keyCase: 'snake'", "keyCase: \"snake\"", "snakeizeRecord"} {
				if strings.Contains(text, forbidden) {
					display, _ := filepath.Rel(root, path)
					t.Fatalf("%s still depends on frontend snake-case JSON:API transform %q", display, forbidden)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestFrontendInteractionGuidelinesAreDocumented(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "doc", "frontend-interaction-guidelines.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{"loading state", "mobile card view", "fullscreen on mobile", "apiErrorText", "Dangerous actions", "Volatile Data", "useAutoRefresh", "cursor pagination", "history mode"} {
		if !strings.Contains(text, required) {
			t.Fatalf("frontend interaction guidelines must document %q", required)
		}
	}
	for _, required := range []string{"Logs", "detail drawer", "restart-required", "event", "group", "method", "status", "durationMs", "not_applicable", "noise"} {
		if !strings.Contains(text, required) {
			t.Fatalf("frontend interaction guidelines must document logging rule %q", required)
		}
	}
}

func TestFrontendViewsDoNotOwnVolatileRefreshTimers(t *testing.T) {
	root := repoRoot(t)
	for _, path := range mustGlob(t, filepath.Join(root, "frontend", "src", "views", "*.vue")) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, forbidden := range []string{"setInterval(", "window.setInterval(", "new EventSource("} {
			if strings.Contains(text, forbidden) {
				display, _ := filepath.Rel(root, path)
				t.Fatalf("%s owns volatile refresh plumbing via %q; use frontend/src/composables/useAutoRefresh.ts", display, forbidden)
			}
		}
	}
}

func TestHighGrowthCollectionsUseCursorPagination(t *testing.T) {
	root := repoRoot(t)
	apiContractRaw, err := os.ReadFile(filepath.Join(root, "doc", "api-contract.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(apiContractRaw), "Cursors are opaque") {
		t.Fatalf("doc/api-contract.md must require opaque cursor pass-through semantics")
	}

	for _, target := range []struct {
		rel  string
		name string
	}{
		{filepath.Join("internal", "transport", "http", "logs.go"), "listLogs"},
		{filepath.Join("internal", "transport", "http", "messaging.go"), "listIngestedMessages"},
		{filepath.Join("internal", "transport", "http", "meeting.go"), "listMeetings"},
		{filepath.Join("internal", "transport", "http", "meeting.go"), "listEvents"},
		{filepath.Join("internal", "transport", "http", "wake.go"), "listWakePlans"},
		{filepath.Join("internal", "transport", "http", "market.go"), "listSymbols"},
		{filepath.Join("internal", "transport", "http", "market.go"), "listWatchlist"},
		{filepath.Join("internal", "transport", "http", "paper.go"), "listAccountOrders"},
		{filepath.Join("internal", "transport", "http", "paper.go"), "listFills"},
		{filepath.Join("internal", "transport", "http", "paper.go"), "listOrders"},
	} {
		raw, err := os.ReadFile(filepath.Join(root, target.rel))
		if err != nil {
			t.Fatal(err)
		}
		body := goFuncBody(t, string(raw), target.name)
		if strings.Contains(body, "writeResourceCollection") {
			t.Fatalf("%s %s must not use in-memory resource collection pagination", target.rel, target.name)
		}
		if !strings.Contains(body, "PageMeta") {
			t.Fatalf("%s %s must write cursor pagination metadata", target.rel, target.name)
		}
	}

	for _, rel := range []string{
		filepath.Join("frontend", "src", "views", "LogsView.vue"),
		filepath.Join("frontend", "src", "views", "IngestedMessagesView.vue"),
		filepath.Join("frontend", "src", "views", "MeetingsView.vue"),
		filepath.Join("frontend", "src", "views", "WakeView.vue"),
		filepath.Join("frontend", "src", "views", "MarketView.vue"),
		filepath.Join("frontend", "src", "views", "PaperView.vue"),
		filepath.Join("frontend", "src", "views", "MeetingDetailView.vue"),
	} {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if !strings.Contains(text, "nextCursor") && !strings.Contains(text, "eventNextCursor") {
			t.Fatalf("%s must expose cursor-pagination state for high-growth data", rel)
		}
	}
}

func TestFrontendViewsUseSharedAPIErrorText(t *testing.T) {
	root := repoRoot(t)
	for _, path := range mustGlob(t, filepath.Join(root, "frontend", "src", "views", "*.vue")) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, forbidden := range []string{"function errorText", "function apiErrorMessage", "function jsonApiErrorText", "response?.data?.errors", "response?.data?.detail"} {
			if strings.Contains(text, forbidden) {
				display, _ := filepath.Rel(root, path)
				t.Fatalf("%s owns local API error parsing via %q; use frontend/src/api.ts apiErrorText", display, forbidden)
			}
		}
	}
}

func TestFrontendBusinessDialogsUseResponsivePattern(t *testing.T) {
	root := repoRoot(t)
	for _, path := range mustGlob(t, filepath.Join(root, "frontend", "src", "views", "*.vue")) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if !strings.Contains(text, "<el-dialog") {
			continue
		}
		display, _ := filepath.Rel(root, path)
		if !strings.Contains(text, `:fullscreen="isMobile"`) {
			t.Fatalf("%s declares a business dialog without mobile fullscreen handling", display)
		}
		if strings.Contains(text, "<el-form") && !strings.Contains(text, `:label-position="isMobile ? 'top' : 'right'"`) {
			t.Fatalf("%s declares a dialog form without responsive label positioning", display)
		}
	}
}

func TestFrontendOperationalListsHaveMobileCardFallbacks(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		filepath.Join("frontend", "src", "views", "IngestedMessagesView.vue"),
		filepath.Join("frontend", "src", "views", "MeetingsView.vue"),
		filepath.Join("frontend", "src", "views", "MessageSubscriptionsView.vue"),
		filepath.Join("frontend", "src", "views", "PlatformAdaptersView.vue"),
		filepath.Join("frontend", "src", "views", "ResearchTeamView.vue"),
		filepath.Join("frontend", "src", "views", "LogsView.vue"),
	} {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "mobile-card-list") {
			t.Fatalf("%s must expose a mobile card fallback for operational lists", rel)
		}
	}
}

func TestRSSCredentialFreeContractIsExplicit(t *testing.T) {
	root := repoRoot(t)
	viewRaw, err := os.ReadFile(filepath.Join(root, "frontend", "src", "views", "MessageSubscriptionsView.vue"))
	if err != nil {
		t.Fatal(err)
	}
	viewText := string(viewRaw)
	for _, forbidden := range []string{`<el-tab-pane label="RSS/Atom" name="rss"`, "RSS/Atom 无全局凭证"} {
		if strings.Contains(viewText, forbidden) {
			t.Fatalf("MessageSubscriptionsView.vue renders RSS/Atom credential placeholder %q; credential-free providers must not get placeholder credential tabs", forbidden)
		}
	}

	apiContractRaw, err := os.ReadFile(filepath.Join(root, "doc", "api-contract.md"))
	if err != nil {
		t.Fatal(err)
	}
	apiContractText := string(apiContractRaw)
	for _, required := range []string{"RSS/Atom subscriptions require public", "reject URL-embedded credentials", "do not support feed authentication in v1"} {
		if !strings.Contains(apiContractText, required) {
			t.Fatalf("doc/api-contract.md must document RSS/Atom credential-free contract via %q", required)
		}
	}

	guidelinesRaw, err := os.ReadFile(filepath.Join(root, "doc", "frontend-interaction-guidelines.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(guidelinesRaw), "Do not render placeholder credential tabs") {
		t.Fatal("doc/frontend-interaction-guidelines.md must forbid placeholder credential tabs")
	}
}

func TestFrontendDefaultRoleResetRequiresConfirmation(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "frontend", "src", "views", "ResearchTeamView.vue"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "roles/apply-defaults") && !strings.Contains(text, "ElMessageBox.confirm") {
		t.Fatalf("ResearchTeamView.vue applies default roles without confirmation")
	}
}

func TestLoggingContractIsExplicit(t *testing.T) {
	root := repoRoot(t)
	openapiRaw, err := os.ReadFile(filepath.Join(root, "api", "openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	openapiText := string(openapiRaw)
	for _, required := range []string{"/logs", "/logs/files", "log-entries", "log-files", "LogFrom", "LogRole", "LogSource", "LogFile", "LogEvent", "LogGroup", "LogMethod", "LogPath", "LogStatus", "LogSlowOnly", "LogIncludeNoise"} {
		if !strings.Contains(openapiText, required) {
			t.Fatalf("api/openapi.yaml must declare logging contract %q", required)
		}
	}
	for _, forbidden := range []string{"LogEventKind", "LogHttpGroup", "LogStatusMin", "LogStatusMax", "eventKind", "httpGroup", "statusMin", "statusMax"} {
		if strings.Contains(openapiText, forbidden) {
			t.Fatalf("api/openapi.yaml still exposes old logging contract %q", forbidden)
		}
	}

	testMatrixRaw, err := os.ReadFile(filepath.Join(root, "doc", "test-matrix.md"))
	if err != nil {
		t.Fatal(err)
	}
	testMatrixText := string(testMatrixRaw)
	for _, required := range []string{"Expected no-op, guard, or duplicate-suppression paths", "level=info", "status=skipped", "reserved for failures"} {
		if !strings.Contains(testMatrixText, required) {
			t.Fatalf("doc/test-matrix.md must document skipped logging semantics via %q", required)
		}
	}

	httpLoggerRaw, err := os.ReadFile(filepath.Join(root, "internal", "transport", "http", "logging_middleware.go"))
	if err != nil {
		t.Fatal(err)
	}
	httpLoggerText := string(httpLoggerRaw)
	for _, required := range []string{"event", "group", "method", "status", "durationMs", "noise", "logGroupForPath", "logNoise"} {
		if !strings.Contains(httpLoggerText, required) {
			t.Fatalf("HTTP logging middleware must expose structured field %q", required)
		}
	}
	for _, forbidden := range []string{"eventKind", "httpGroup", "httpLogGroup", "httpLogNoise"} {
		if strings.Contains(httpLoggerText, forbidden) {
			t.Fatalf("HTTP logging middleware still exposes old logging field %q", forbidden)
		}
	}
	if strings.Contains(httpLoggerText, `"http request completed"`) {
		t.Fatalf("HTTP logging middleware must not use a fixed completion message")
	}

	logsViewRaw, err := os.ReadFile(filepath.Join(root, "frontend", "src", "views", "LogsView.vue"))
	if err != nil {
		t.Fatal(err)
	}
	logsViewText := string(logsViewRaw)
	for _, required := range []string{"event", "group", "method", "status", "slowOnly", "includeNoise"} {
		if !strings.Contains(logsViewText, required) {
			t.Fatalf("LogsView.vue must expose logging filter %q", required)
		}
	}
	for _, forbidden := range []string{"eventKind", "httpGroup", "statusMin", "statusMax"} {
		if strings.Contains(logsViewText, forbidden) {
			t.Fatalf("LogsView.vue still exposes old logging filter %q", forbidden)
		}
	}

	settingsRaw, err := os.ReadFile(filepath.Join(root, "internal", "app", "settings", "usecase.go"))
	if err != nil {
		t.Fatal(err)
	}
	settingsText := string(settingsRaw)
	for _, required := range []string{"LOG_DIR", "LOG_LEVEL", "LOG_ROTATION_MODE", "LOG_ROTATION_SIZE_MB", "LOG_ROTATION_TOTAL_SIZE_MB", "LOG_ROTATION_MAX_AGE_DAYS"} {
		if !strings.Contains(settingsText, required) {
			t.Fatalf("settings usecase must expose editable logging env %q", required)
		}
	}

	routerRaw, err := os.ReadFile(filepath.Join(root, "frontend", "src", "router.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(routerRaw), "/logs") {
		t.Fatalf("frontend router must expose /logs")
	}
}

func TestPaperFrontendUsesDeclaredJSONAPIResourceTypes(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "frontend", "src", "views", "PaperView.vue"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "paper-requests") {
		t.Fatalf("PaperView.vue still posts generic paper-requests; use declared paper JSON:API resource types")
	}
}

func TestMeetingDailyTokenBudgetUnlimitedContractIsExplicit(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{".env.example", ".env.local.example"} {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "MEETING_DAILY_TOKEN_BUDGET=-1") {
			t.Fatalf("%s must default MEETING_DAILY_TOKEN_BUDGET to -1 for unlimited usage", rel)
		}
	}

	apiContractRaw, err := os.ReadFile(filepath.Join(root, "doc", "api-contract.md"))
	if err != nil {
		t.Fatal(err)
	}
	apiContractText := string(apiContractRaw)
	for _, required := range []string{"MEETING_DAILY_TOKEN_BUDGET", "`-1` for unlimited", "`0` or values below `-1` are invalid"} {
		if !strings.Contains(apiContractText, required) {
			t.Fatalf("doc/api-contract.md must document meeting daily token budget semantics via %q", required)
		}
	}

	settingsRaw, err := os.ReadFile(filepath.Join(root, "internal", "app", "settings", "usecase.go"))
	if err != nil {
		t.Fatal(err)
	}
	settingsText := string(settingsRaw)
	for _, required := range []string{"validateEditableRuntimeSetting", "MEETING_DAILY_TOKEN_BUDGET must be -1 for unlimited or a positive integer"} {
		if !strings.Contains(settingsText, required) {
			t.Fatalf("settings usecase must guard meeting daily token budget semantics via %q", required)
		}
	}

	frontendRaw, err := os.ReadFile(filepath.Join(root, "frontend", "src", "views", "SettingsView.vue"))
	if err != nil {
		t.Fatal(err)
	}
	frontendText := string(frontendRaw)
	for _, required := range []string{`:min="-1"`, "-1 means unlimited"} {
		if !strings.Contains(frontendText, required) {
			t.Fatalf("SettingsView.vue must allow the unlimited meeting daily token budget sentinel via %q", required)
		}
	}
}

func TestFrontendQueryParamsStayCamelCase(t *testing.T) {
	root := repoRoot(t)
	forbidden := []string{"meeting_id", "channel_id", "filter_decision", "only_unfiltered", "trigger_source"}
	err := filepath.WalkDir(filepath.Join(root, "frontend", "src"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || (!strings.HasSuffix(path, ".ts") && !strings.HasSuffix(path, ".vue")) {
			return nil
		}
		if strings.Contains(filepath.ToSlash(path), "/frontend/src/generated/") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(raw)
		for _, value := range forbidden {
			if strings.Contains(text, value) {
				display, _ := filepath.Rel(root, path)
				t.Fatalf("%s still uses snake_case frontend query parameter %q", display, value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDocsDoNotClaimSnakeCaseAPIResponses(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "doc", "test-matrix.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "responses preserve `snake_case`") {
		t.Fatalf("doc/test-matrix.md still documents snake_case API responses; JSON:API attributes should be camelCase")
	}
}

func TestInfraRuntimeDoesNotDependOnTransport(t *testing.T) {
	packages := listPackages(t)
	for _, pkg := range packages {
		if strings.HasSuffix(pkg.ImportPath, "/internal/infra/runtime") {
			rejectImports(t, pkg, "/internal/transport/")
		}
	}
}

func TestNonPersistenceInfraDoesNotUseGORM(t *testing.T) {
	root := repoRoot(t)
	forbidden := regexp.MustCompile(`gorm\.io/gorm|\*gorm\.DB|\bdb\.`)
	err := filepath.WalkDir(filepath.Join(root, "internal", "infra"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(filepath.ToSlash(rel), "internal/infra/persistence/") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if match := forbidden.Find(raw); len(match) > 0 {
			t.Fatalf("%s uses GORM/direct DB access via %q; route persistence through internal/infra/persistence", rel, string(match))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestLocalReferenceAndLogPathsAreIgnored(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	ignore := string(raw)
	for _, pattern := range []string{"AIWarrenBuffett/", "logs/", "frontend/dist/", "node_modules/", "data/"} {
		if !strings.Contains(ignore, pattern) {
			t.Fatalf(".gitignore does not contain %s", pattern)
		}
	}
}

func TestInternalModuleDirectoriesAreNotEmpty(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			return err
		}
		realEntries := 0
		for _, entry := range entries {
			if entry.Name() != ".gitkeep" {
				realEntries++
			}
		}
		if realEntries == 0 {
			rel, _ := filepath.Rel(root, path)
			t.Fatalf("empty internal module directory remains: %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCmdDoesNotOwnInteractiveRuntimeWorkflows(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "cmd", "tradingcopilot", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{
		"bufio",
		"ReadString(",
		"SaveTelegramMTProtoAppConfig(",
		"StartTelegramMTProtoLogin(",
		"CompleteTelegramMTProtoLogin(",
		"GetLatestTelegramMTProtoMessage(",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("cmd/tradingcopilot/main.go still owns runtime workflow detail %q", forbidden)
		}
	}
}

func TestCmdOnlyParsesAndDelegates(t *testing.T) {
	packages := listPackages(t)
	for _, pkg := range packages {
		if !strings.Contains(pkg.ImportPath, "/cmd/tradingcopilot") {
			continue
		}
		rejectImports(t, pkg,
			"gorm.io/",
			"/internal/infra/persistence/",
			"/internal/transport/",
		)
	}
}

func TestMarketSyncUsesTransactionScopedRepository(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "internal", "app", "market", "usecase.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"u.repo.UpsertSymbol(ctx", "u.repo.UpsertDailyBar(ctx"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("market write synchronization bypasses transaction-scoped repository via %s", forbidden)
		}
	}
}

func TestInfraAdaptersDoNotOwnGORMWrites(t *testing.T) {
	root := repoRoot(t)
	writePattern := regexp.MustCompile(`\b(db|tx)\.(Create|Save|Updates|Update|Delete|Transaction)\(`)
	allowedDirs := []string{
		filepath.Join(root, "internal", "infra", "persistence", "gorm", "repo") + string(os.PathSeparator),
		filepath.Join(root, "internal", "infra", "persistence", "gorm", "uow") + string(os.PathSeparator),
		filepath.Join(root, "internal", "infra", "persistence", "gorm", "migrate") + string(os.PathSeparator),
	}
	err := filepath.WalkDir(filepath.Join(root, "internal", "infra"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		for _, allowed := range allowedDirs {
			if strings.HasPrefix(path, allowed) {
				return nil
			}
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if match := writePattern.Find(raw); len(match) > 0 {
			rel, _ := filepath.Rel(root, path)
			t.Fatalf("%s contains direct GORM write/transaction call %q; route writes through gorm repo/uow", rel, string(match))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAppServicePortsDoNotOwnTransactions(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(filepath.Join(root, "internal", "app"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if serviceInterfaceContainsWithTx(string(raw)) {
			rel, _ := filepath.Rel(root, path)
			t.Fatalf("%s declares a Service-owned transaction method; app write transactions must use repository/transactor ports", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAppRepositoryPortsDoNotOwnTransactions(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(filepath.Join(root, "internal", "app"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if repositoryInterfaceContainsWithTx(string(raw)) {
			rel, _ := filepath.Rel(root, path)
			t.Fatalf("%s declares Repository-owned WithTx; app write transactions must use explicit Transactor/UnitOfWork ports", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMarketDataInfraDoesNotPersist(t *testing.T) {
	root := repoRoot(t)
	forbidden := regexp.MustCompile(`gormrepo\.New|\.(Create|Save|Updates|Update|Delete|Transaction)\(`)
	err := filepath.WalkDir(filepath.Join(root, "internal", "infra", "marketdata"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if match := forbidden.Find(raw); len(match) > 0 {
			rel, _ := filepath.Rel(root, path)
			t.Fatalf("%s persists from marketdata infra via %q; route writes through app/market repositories", rel, string(match))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPaperAppDoesNotOwnLegacyPayloadTranslation(t *testing.T) {
	root := repoRoot(t)
	err := filepath.WalkDir(filepath.Join(root, "internal", "app", "paper"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(raw)
		for _, forbidden := range []string{"legacyPaperPayload", "legacyPaperKey", "lowerCamelToSnake"} {
			if strings.Contains(text, forbidden) {
				rel, _ := filepath.Rel(root, path)
				t.Fatalf("%s owns legacy paper payload translation via %q; keep transport inputs typed at the app boundary and legacy persistence keys inside the GORM paper adapter", rel, forbidden)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMarketDataRuntimeHelpersAreOutsideGORMPersistence(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		filepath.Join("internal", "infra", "marketdata", "symbols.go"),
		filepath.Join("internal", "infra", "marketdata", "realtime.go"),
		filepath.Join("internal", "infra", "marketdata", "history.go"),
		filepath.Join("internal", "infra", "marketdata", "tencent.go"),
		filepath.Join("internal", "infra", "marketdata", "runtime_config.go"),
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("expected marketdata runtime helper %s: %v", rel, err)
		}
	}

	if _, err := os.Stat(filepath.Join(root, "internal", "infra", "persistence", "gorm", "marketdata", "adapter.go")); err == nil {
		t.Fatalf("persistence marketdata adapter service still exists; app wiring must use internal/infra/marketdata.Service with a DB-backed store")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "internal", "infra", "persistence", "gorm", "marketdata", "market.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{
		"func (p *HTTPHistoryProvider)",
		"`json:\"fields\"`",
		"http.NewRequest(http.MethodGet",
		"func parseTencentKLineRow",
		"func parseTencentSeriesRow",
		"case ProviderTushare:",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("persistence marketdata still owns external provider/runtime detail %q", forbidden)
		}
	}

	raw, err = os.ReadFile(filepath.Join(root, "internal", "app", "market", "usecase.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "LatestRealtimeQuote(ctx") {
		t.Fatalf("app/market must own realtime quote cache fallback through the repository port")
	}
}

func TestRealtimeFallbackDataSourceContractStaysRemoved(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		".env.example",
		".env.local.example",
		filepath.Join("doc", "api-contract.md"),
		filepath.Join("frontend", "src"),
		filepath.Join("internal"),
	} {
		path := filepath.Join(root, rel)
		if info, err := os.Stat(path); err != nil {
			t.Fatal(err)
		} else if info.IsDir() {
			err := filepath.WalkDir(path, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() || strings.Contains(filepath.ToSlash(path), "/frontend/src/generated/") || strings.HasSuffix(filepath.ToSlash(path), "/internal/architecture/architecture_test.go") {
					return nil
				}
				assertNoRealtimeFallbackDataSourceTokens(t, root, path)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		} else {
			assertNoRealtimeFallbackDataSourceTokens(t, root, path)
		}
	}
	for _, rel := range []string{
		".env.example",
		".env.local.example",
		filepath.Join("doc", "api-contract.md"),
		filepath.Join("internal", "infra", "marketdata", "realtime_compat.go"),
		filepath.Join("frontend", "src", "views", "SettingsView.vue"),
	} {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "MARKET_REALTIME_COMPAT_PROVIDER") && !strings.Contains(string(raw), "ProviderTencentCompat") {
			t.Fatalf("%s must document or implement the narrow realtime compatibility provider contract", rel)
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, "doc", "api-contract.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "ETF/LOF") || !strings.Contains(string(raw), "not a generic fallback provider list") {
		t.Fatalf("api-contract must describe MARKET_REALTIME_COMPAT_PROVIDER as a narrow ETF/LOF compatibility source")
	}
}

func assertNoRealtimeFallbackDataSourceTokens(t *testing.T, root string, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{
		"MARKET_REALTIME_FALLBACKS",
		"MarketRealtimeFallbacks",
		"RealtimeFallbacks",
		"RealtimeProviderOrder",
		"FetchRealtimeQuotesWithFallbacks",
		"FetchRealtimeQuoteWithFallbacks",
		"QuoteForCodeWithProviders",
		"RefreshQuotesWithProviders",
		"第 3 类：实时回退数据源",
	} {
		if strings.Contains(text, forbidden) {
			display, _ := filepath.Rel(root, path)
			t.Fatalf("%s still exposes removed realtime fallback data-source contract via %q", display, forbidden)
		}
	}
}

func TestProxyHTTPRuntimeIsOutsideGORMPersistence(t *testing.T) {
	root := repoRoot(t)
	pureRuntime := filepath.Join(root, "internal", "infra", "proxy", "runtime", "proxy.go")
	raw, err := os.ReadFile(pureRuntime)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{"HTTPClientForConfig", "ValidateProxyURL", "ShouldBypass"} {
		if !strings.Contains(text, required) {
			t.Fatalf("internal/infra/proxy/runtime/proxy.go must own proxy runtime helper %s", required)
		}
	}
	for _, forbidden := range []string{"gorm.io/gorm", "/internal/infra/persistence/gorm/"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("internal/infra/proxy/runtime/proxy.go imports persistence dependency %q", forbidden)
		}
	}

	persistenceRuntime := filepath.Join(root, "internal", "infra", "persistence", "gorm", "proxy", "runtime", "proxy.go")
	raw, err = os.ReadFile(persistenceRuntime)
	if err != nil {
		t.Fatal(err)
	}
	text = string(raw)
	for _, forbidden := range []string{"golang.org/x/net/proxy", "func socks5Dialer"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("persistence proxy runtime still owns outbound proxy HTTP runtime detail %q", forbidden)
		}
	}
}

func TestQueueRuntimeMuxIsOutsideGORMPersistence(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "internal", "infra", "queue", "queue.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "func NewServeMux(handlers Handlers) *asynq.ServeMux") {
		t.Fatalf("internal/infra/queue must own generic asynq ServeMux construction")
	}

	raw, err = os.ReadFile(filepath.Join(root, "internal", "infra", "persistence", "gorm", "queue", "jobs.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"json.Unmarshal(task.Payload()", "asynq.NewServeMux()"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("persistence queue package still owns generic queue runtime detail %q", forbidden)
		}
	}
	if !strings.Contains(text, "infraqueue.NewServeMux") {
		t.Fatalf("persistence queue package must bind DB-backed handlers through internal/infra/queue.NewServeMux")
	}
}

func TestPeriodicQueueJobsCoalesceByTaskType(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "internal", "infra", "queue", "queue.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{
		"Expires time.Duration",
		"Deadline(",
		"MaxRetry(0)",
		"FormatInt(slot.Unix()",
		`"periodic:" + job.Type + ":"`,
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("periodic queue jobs must coalesce by task type and avoid expired backlog noise; found %q", forbidden)
		}
	}
	if !strings.Contains(text, `return "periodic:" + job.Type`) {
		t.Fatalf("periodic task IDs must be stable per task type")
	}
}

func TestUnitOfWorkDoesNotConstructMixedRuntimeServices(t *testing.T) {
	root := repoRoot(t)
	uowDir := filepath.Join(root, "internal", "infra", "persistence", "gorm", "uow")
	for _, path := range mustGlob(t, filepath.Join(uowDir, "*.go")) {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, forbidden := range []string{
			"/internal/infra/persistence/gorm/marketdata",
			"/internal/infra/persistence/gorm/telegram",
			"/internal/infra/persistence/gorm/meeting",
			"marketdata.NewService(",
			"infratelegram.NewService(",
			"inframeeting.NewService(",
		} {
			if strings.Contains(text, forbidden) {
				rel, _ := filepath.Rel(root, path)
				t.Fatalf("%s constructs mixed persistence/runtime service via %q; UoW must only bind transaction-scoped repositories", rel, forbidden)
			}
		}
	}
}

func TestMigratedGORMWorkflowsPreserveCallerContext(t *testing.T) {
	root := repoRoot(t)
	for _, relDir := range []string{
		filepath.Join("internal", "infra", "persistence", "gorm", "meeting"),
		filepath.Join("internal", "infra", "persistence", "gorm", "paper"),
		filepath.Join("internal", "infra", "persistence", "gorm", "telegram"),
	} {
		err := filepath.WalkDir(filepath.Join(root, relDir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for lineNumber, line := range strings.Split(string(raw), "\n") {
				if !strings.Contains(line, "context.Background()") {
					continue
				}
				if strings.Contains(line, "return context.Background()") {
					continue
				}
				display, _ := filepath.Rel(root, path)
				t.Fatalf("%s:%d uses context.Background(); preserve app/UoW caller context through dbContext(db)", display, lineNumber+1)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestTelegramRuntimeHelpersAreOutsideGORMPersistence(t *testing.T) {
	root := repoRoot(t)
	for _, rel := range []string{
		filepath.Join("internal", "infra", "telegram", "bot.go"),
		filepath.Join("internal", "infra", "telegram", "public.go"),
		filepath.Join("internal", "infra", "telegram", "filter.go"),
		filepath.Join("internal", "infra", "telegram", "proxy.go"),
		filepath.Join("internal", "infra", "telegram", "mtproto_session.go"),
		filepath.Join("internal", "infra", "telegram", "mtproto_client.go"),
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("expected Telegram runtime helper %s: %v", rel, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, "internal", "infra", "persistence", "gorm", "telegram", "telegram_mtproto.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"golang.org/x/net/proxy", "func dialSOCKS4", "http.ReadResponse", "http.Request{Method: http.MethodConnect"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("persistence telegram MTProto still owns proxy runtime detail %q", forbidden)
		}
	}
	for _, forbidden := range []string{"type telegramDBSessionStorage", "LoadSession(ctx context.Context)", "StoreSession(ctx context.Context"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("persistence telegram MTProto still owns gotd session storage detail %q", forbidden)
		}
	}
	for _, forbidden := range []string{"github.com/gotd", "telegram.NewClient", "peers.Options", "MessagesGetHistory", "dialogs.NewQueryBuilder", "tg.NewUpdateDispatcher"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("persistence telegram MTProto still owns gotd runtime detail %q", forbidden)
		}
	}

	raw, err = os.ReadFile(filepath.Join(root, "internal", "infra", "persistence", "gorm", "telegram", "telegram_listener.go"))
	if err != nil {
		t.Fatal(err)
	}
	text = string(raw)
	for _, forbidden := range []string{"github.com/gotd", "telegram.NewClient", "peers.Options", "tg.NewUpdateDispatcher", "func telegramPeerKeys", "func handleGotdLiveUpdateMessage"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("persistence telegram listener still owns gotd runtime detail %q", forbidden)
		}
	}

	raw, err = os.ReadFile(filepath.Join(root, "internal", "infra", "persistence", "gorm", "telegram", "telegram_bot.go"))
	if err != nil {
		t.Fatal(err)
	}
	text = string(raw)
	for _, forbidden := range []string{"func replyTelegramBotMeetings", "func createTelegramBotMeeting", "func telegramBotChatAllowed"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("persistence telegram bot still owns app orchestration via %q", forbidden)
		}
	}
}

func TestMeetingRuntimeHelpersAreOutsideGORMPersistence(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "internal", "infra", "meeting", "web_search.go")); err != nil {
		t.Fatalf("expected meeting web search runtime helper: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "internal", "infra", "persistence", "gorm", "meeting", "meeting_tools.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"encoding/xml", "net/url", "xml.NewDecoder", "http.DefaultClient", "https://news.google.com/rss/search"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("persistence meeting tools still own web search runtime detail %q", forbidden)
		}
	}
}

func TestMessagingRuntimeAdapterIsOutsideGORMPersistence(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "internal", "infra", "messaging", "service.go")); err != nil {
		t.Fatalf("expected provider-neutral messaging runtime adapter outside GORM persistence: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "internal", "infra", "persistence", "gorm", "messaging")); err == nil {
		t.Fatalf("internal/infra/persistence/gorm/messaging still exists; messaging protocol runtime belongs in internal/infra/messaging")
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}

	for _, rel := range []string{
		filepath.Join("internal", "infra", "persistence", "gorm", "repo", "messaging.go"),
		filepath.Join("internal", "infra", "persistence", "gorm", "uow", "messaging.go"),
	} {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, forbidden := range []string{"/internal/infra/telegram", "github.com/gotd", "FilterMessageWithRole", "MTProtoClient", "SendBotMessage"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s owns messaging runtime detail via %q; use internal/infra/messaging and keep GORM packages persistence-only", rel, forbidden)
			}
		}
	}

	raw, err := os.ReadFile(filepath.Join(root, "internal", "infra", "messaging", "service.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{"github.com/mmcdole/gofeed", "ProviderRSSFeed", "ParseURLWithContext"} {
		if !strings.Contains(text, required) {
			t.Fatalf("internal/infra/messaging/service.go must own RSS/Atom runtime via %q", required)
		}
	}
	for _, forbidden := range []string{
		".CreateMessage(",
		".SaveMessage(",
		".DeleteMessagesByIDsWithRefs(",
		".CreateMeeting(",
		".AppendMeetingEvent(",
		".CreateMeetingReference(",
		"EnsureMeetingForMessage(",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("internal/infra/messaging/service.go owns messaging business write via %q; route persistence and meeting creation through app/messaging transactions", forbidden)
		}
	}

	for _, rel := range []string{
		filepath.Join("internal", "infra", "persistence", "gorm", "repo", "messaging.go"),
		filepath.Join("internal", "infra", "persistence", "gorm", "model", "messaging.go"),
	} {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		for _, forbidden := range []string{"github.com/mmcdole/gofeed", "encoding/xml"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s owns RSS parsing runtime via %q; keep RSS runtime in internal/infra/messaging", rel, forbidden)
			}
		}
	}
}

func TestMessagingIngestionDoesNotSynchronouslyRunAIFilter(t *testing.T) {
	root := repoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "internal", "app", "messaging", "usecase.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	body := functionBody(text, "func (u Usecase) ingestFetchedMessage")
	if body == "" {
		t.Fatal("messaging usecase must define ingestFetchedMessage")
	}
	for _, forbidden := range []string{"applyFilter(", "ensureMeetingsForMessage("} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("ingestFetchedMessage must only persist/mark messages and enqueue filtering; found synchronous %q", forbidden)
		}
	}
	for _, required := range []string{"TaskQueue interface", "QueueCollectSubscriptions", "ProcessFilterTask", "FilterStatusFiltering"} {
		if !strings.Contains(text, required) {
			t.Fatalf("async message filtering contract is missing %q", required)
		}
	}
}

func functionBody(text string, signature string) string {
	start := strings.Index(text, signature)
	if start < 0 {
		return ""
	}
	open := strings.Index(text[start:], "{")
	if open < 0 {
		return ""
	}
	i := start + open
	depth := 0
	for ; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return ""
}

func serviceInterfaceContainsWithTx(raw string) bool {
	inService := false
	depth := 0
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "type Service interface") {
			inService = true
		}
		if !inService {
			continue
		}
		depth += strings.Count(line, "{")
		depth -= strings.Count(line, "}")
		if strings.HasPrefix(trimmed, "WithTx(") {
			return true
		}
		if depth <= 0 {
			inService = false
		}
	}
	return false
}

func repositoryInterfaceContainsWithTx(raw string) bool {
	inRepository := false
	depth := 0
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "type ") && strings.Contains(trimmed, "Repository interface") {
			inRepository = true
		}
		if !inRepository {
			continue
		}
		depth += strings.Count(line, "{")
		depth -= strings.Count(line, "}")
		if strings.HasPrefix(trimmed, "WithTx(") {
			return true
		}
		if depth <= 0 {
			inRepository = false
		}
	}
	return false
}

func goFuncBody(t *testing.T, raw string, name string) string {
	t.Helper()
	marker := "func (s *Server) " + name + "("
	start := strings.Index(raw, marker)
	if start < 0 {
		t.Fatalf("server handler %s not found", name)
	}
	next := strings.Index(raw[start+len(marker):], "\nfunc ")
	if next < 0 {
		return raw[start:]
	}
	return raw[start : start+len(marker)+next]
}

func rejectImports(t *testing.T, pkg goPackage, forbidden ...string) {
	t.Helper()
	for _, imported := range pkg.Imports {
		for _, pattern := range forbidden {
			if strings.Contains(imported, pattern) || imported == pattern {
				t.Fatalf("%s imports forbidden dependency %s", pkg.ImportPath, imported)
			}
		}
	}
}

func listPackages(t *testing.T) []goPackage {
	t.Helper()
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = repoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(out)))
	var packages []goPackage
	for {
		var pkg goPackage
		if err := decoder.Decode(&pkg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("decode go list output: %v", err)
		}
		packages = append(packages, pkg)
	}
	return packages
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatalf("glob matched no files: %s", pattern)
	}
	return matches
}
