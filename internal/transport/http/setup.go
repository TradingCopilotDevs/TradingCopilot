package httptransport

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	appprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/app/prediction"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http/jsonapi"
)

type setupStep struct {
	Key       string `json:"key"`
	Title     string `json:"title"`
	Category  string `json:"category"`
	Ready     bool   `json:"ready"`
	Status    string `json:"status"`
	Summary   string `json:"summary"`
	Detail    string `json:"detail"`
	ActionKey string `json:"actionKey,omitempty"`
	Route     string `json:"route,omitempty"`
	Optional  bool   `json:"optional,omitempty"`
}

type setupActionResult struct {
	Key     string         `json:"key"`
	Status  string         `json:"status"`
	Summary string         `json:"summary"`
	Detail  string         `json:"detail,omitempty"`
	Output  map[string]any `json:"output,omitempty"`
	RanAt   time.Time      `json:"ranAt"`
}

func (s *Server) setupReadiness(w http.ResponseWriter, r *http.Request) {
	payload, err := s.buildSetupReadiness(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "setup-readiness-load-failed", "Setup readiness load failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("setup-readinesses", "current", payload))
}

func (s *Server) runSetupAction(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(stringParam(r, "key"))
	result, err := s.executeSetupAction(r.Context(), key)
	if err != nil {
		status := http.StatusBadRequest
		code := "setup-action-failed"
		if strings.Contains(err.Error(), "unsupported setup action") {
			status = http.StatusNotFound
			code = "setup-action-not-found"
		}
		writeJSONAPIError(w, status, code, "Setup action failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("setup-action-results", key, result))
}

func (s *Server) buildSetupReadiness(ctx context.Context) (map[string]any, error) {
	dashboard, err := s.dashboardUsecase.Load(ctx)
	if err != nil {
		return nil, err
	}
	metrics := mapValue(dashboard, "businessMetrics")
	diagnostics := mapValue(dashboard, "dependencyDiagnostics")
	systemStatus := mapValue(dashboard, "systemStatus")
	summary := mapValue(dashboard, "summary")

	adminRequired, adminErr := s.auth.BootstrapRequired(ctx)
	steps := []setupStep{
		{
			Key:      "admin",
			Title:    "管理员账号",
			Category: "access",
			Ready:    adminErr == nil && !adminRequired,
			Status:   statusFromReady(adminErr == nil && !adminRequired, adminErr),
			Summary:  adminSummary(adminRequired, adminErr),
			Detail:   "首次启动时需要创建管理员账号；之后进入工作台继续完成服务配置。",
		},
	}

	aiDiagnostics := mapValue(diagnostics, "ai")
	aiProviders := listValue(aiDiagnostics, "providers")
	aiReady := anyReadyProvider(aiProviders)
	steps = append(steps, setupStep{
		Key:       "aiProviders",
		Title:     "AI 服务商与模型",
		Category:  "research",
		Ready:     aiReady,
		Status:    readinessStatus(aiReady, len(aiProviders) > 0),
		Summary:   fmt.Sprintf("%d 个服务商，%d 个可用于投研", len(aiProviders), countReadyProviders(aiProviders)),
		Detail:    "投研会议和消息过滤需要至少一个启用、带 API Key、带默认模型的 OpenAI 兼容服务商。",
		ActionKey: "sync-ai-models",
		Route:     "/model-providers",
	})

	teams, teamErr := s.researchUsecase.ListTeams(ctx)
	roleCount := 0
	for _, team := range teams {
		roles, err := s.researchUsecase.ListRoles(ctx, team.ID)
		if err == nil {
			roleCount += len(roles)
		}
	}
	teamReady := teamErr == nil && len(teams) > 0 && roleCount > 0
	steps = append(steps, setupStep{
		Key:       "researchTeam",
		Title:     "投研团队与角色",
		Category:  "research",
		Ready:     teamReady,
		Status:    statusFromReady(teamReady, teamErr),
		Summary:   fmt.Sprintf("%d 个团队，%d 个角色", len(teams), roleCount),
		Detail:    "会议运行需要团队、模拟盘账户绑定，以及至少一组启用的投研角色。",
		ActionKey: "ensure-default-team",
		Route:     "/research-team",
	})

	predictionTeamCount := 0
	for _, team := range teams {
		if strings.TrimSpace(team.AssetClass) == "prediction_market" {
			predictionTeamCount++
		}
	}
	filters, filterErr := s.messagingUsecase.ListSubscriptionFilters(ctx)
	predictionFilterReady := false
	for _, filter := range filters {
		if filter.Name == "默认预测市场消息过滤器" {
			predictionFilterReady = true
			break
		}
	}
	var predictionLocalSample appprediction.MarketSearchResult
	var predictionLocalErr error
	var predictionMatches []appprediction.MatchRow
	var predictionMatchErr error
	predictionHealth := map[string]any{}
	if s.predictionUsecase.Configured() {
		predictionLocalSample, predictionLocalErr = s.predictionUsecase.Search(ctx, "", 1)
		predictionMatches, predictionMatchErr = s.predictionUsecase.ListMatches(ctx, appprediction.MatchFilter{Limit: 100})
		predictionHealth = s.predictionProviderHealth(ctx)
	}
	predictionDefaultsReady := teamErr == nil && filterErr == nil && predictionTeamCount > 0 && predictionFilterReady
	predictionReady := predictionDefaultsReady && s.predictionUsecase.Configured()
	predictionStatus := statusFromReady(predictionDefaultsReady, firstError(teamErr, filterErr))
	if !s.predictionUsecase.Configured() {
		predictionStatus = "missing"
	}
	if strings.TrimSpace(fmt.Sprint(predictionHealth["status"])) == "warning" && predictionStatus == "ready" {
		predictionStatus = "warning"
	}
	if (predictionLocalErr != nil || predictionMatchErr != nil) && predictionStatus == "ready" {
		predictionStatus = "warning"
	}
	steps = append(steps, setupStep{
		Key:      "predictionMarket",
		Title:    "预测市场",
		Category: "research",
		Ready:    predictionReady,
		Status:   predictionStatus,
		Summary: fmt.Sprintf(
			"%d 个预测团队，预测过滤器 %s，本地市场样本 %d，连通性 %s",
			predictionTeamCount,
			yesNo(predictionFilterReady),
			len(predictionLocalSample.Rows),
			predictionConnectivitySummary(predictionHealth),
		),
		Detail: fmt.Sprintf(
			"Polymarket 公共行情、市场发现、新闻匹配和会议证据接入使用行情/预测市场代理配置；v1 不启用预测市场模拟盘。连通性：%s；代理模块：%s；最近匹配健康度：%s。",
			predictionConnectivitySummary(predictionHealth),
			firstNonEmptyString(strings.TrimSpace(fmt.Sprint(predictionHealth["proxyModule"])), "market"),
			predictionMatchHealthSummary(predictionMatches, predictionMatchErr),
		),
		ActionKey: "ensure-prediction-market-defaults",
		Route:     "/prediction-markets",
	})

	paperSetup, paperErr := s.paperUsecase.SetupStatus(ctx)
	paperReady := paperErr == nil && paperSetup.AccountCount > 0 && paperSetup.RiskConfigCount > 0
	steps = append(steps, setupStep{
		Key:       "paperRisk",
		Title:     "模拟盘账户与风控",
		Category:  "trading",
		Ready:     paperReady,
		Status:    statusFromReady(paperReady, paperErr),
		Summary:   fmt.Sprintf("%d 个账户，%d 套风控", paperSetup.AccountCount, paperSetup.RiskConfigCount),
		Detail:    "模拟盘建议订单需要账户、风控范围、手续费和启用状态共同闭环。",
		ActionKey: "ensure-paper-defaults",
		Route:     "/paper",
	})

	marketProvider := stringValueFromMap(mapValue(diagnostics, "market"), "provider.defaultProvider")
	marketSymbolCount := intValue(metrics["marketSymbolCount"])
	marketReady := marketSymbolCount > 0
	steps = append(steps, setupStep{
		Key:       "market",
		Title:     "行情源与证券列表",
		Category:  "data",
		Ready:     marketReady,
		Status:    readinessStatus(marketReady, strings.TrimSpace(marketProvider) != ""),
		Summary:   fmt.Sprintf("%d 个证券，默认源 %s", marketSymbolCount, firstNonEmptyString(marketProvider, "-")),
		Detail:    "A 股行情、指标唤醒、自选池和投研工具都依赖已同步的证券列表与可用行情源。",
		ActionKey: "sync-market-symbols",
		Route:     "/market",
	})

	messageSetup, messageSetupErr := s.messagingUsecase.SetupStatus(ctx)
	messageStatus := s.messagingUsecase.SubscriptionStatus(ctx)
	newsFilterReady := boolValue(metrics["newsFilterReady"])
	messagingReady := messageSetupErr == nil && messageSetup.FilterCount > 0 && messageSetup.SubscriptionCount > 0 && newsFilterReady
	steps = append(steps, setupStep{
		Key:       "messaging",
		Title:     "消息源与过滤器",
		Category:  "data",
		Ready:     messagingReady,
		Status:    statusFromReady(messagingReady, messageSetupErr),
		Summary:   fmt.Sprintf("%d 个订阅源，过滤器 AI %s", messageSetup.SubscriptionCount, yesNo(newsFilterReady)),
		Detail:    fmt.Sprintf("Telegram App ID/App Hash/Session: %s/%s/%s；RSS 或 Telegram 订阅源至少需要配置一个。", yesNo(messageStatus["has_app_id"]), yesNo(messageStatus["has_app_hash"]), yesNo(messageStatus["has_session"])),
		ActionKey: "ensure-message-filter",
		Route:     "/message-subscriptions",
	})

	proxyConfig, proxyErr := s.settingsUsecase.LoadProxyConfig(ctx)
	proxyEnabled := proxyConfig.AnyModuleEnabled()
	proxyReady := proxyErr == nil && (!proxyEnabled || strings.TrimSpace(proxyConfig.ProxyURL) != "")
	steps = append(steps, setupStep{
		Key:       "proxy",
		Title:     "代理连通性",
		Category:  "runtime",
		Ready:     proxyReady,
		Status:    statusFromReady(proxyReady, proxyErr),
		Summary:   proxySummary(proxyEnabled, proxyConfig.ProxyURL),
		Detail:    "AI、Telegram、行情和 Web 请求可以分别启用出站代理；启用后需要先保存代理 URL 再测试。",
		ActionKey: "test-proxy",
		Route:     "/settings",
		Optional:  !proxyEnabled,
	})

	logReady, logSummary := s.logReadiness(ctx)
	steps = append(steps, setupStep{
		Key:      "logs",
		Title:    "运行日志",
		Category: "runtime",
		Ready:    logReady,
		Status:   readinessStatus(logReady, true),
		Summary:  logSummary,
		Detail:   "日志目录、级别和轮转策略决定排障可用性；生产部署前应确认日志保留策略。",
		Route:    "/logs",
	})

	queueReady, queueStatus, queueSummary := queueReadiness(systemStatus, summary)
	steps = append(steps, setupStep{
		Key:      "queue",
		Title:    "队列与后台任务",
		Category: "runtime",
		Ready:    queueReady,
		Status:   queueStatus,
		Summary:  queueSummary,
		Detail:   "非本地模式下，会议运行、调度器和消息采集依赖 Redis、Worker、Scheduler 心跳。",
		Route:    "/",
	})

	completed := 0
	for _, step := range steps {
		if step.Ready || step.Optional {
			completed++
		}
	}
	completionPct := 0
	if len(steps) > 0 {
		completionPct = completed * 100 / len(steps)
	}
	return map[string]any{
		"completed":     completed,
		"total":         len(steps),
		"completionPct": completionPct,
		"generatedAt":   time.Now(),
		"steps":         steps,
	}, nil
}

func (s *Server) executeSetupAction(ctx context.Context, key string) (setupActionResult, error) {
	switch key {
	case "sync-ai-models":
		return s.syncAllProviderModels(ctx)
	case "apply-default-ai-capabilities":
		updated, err := s.aiUsecase.ApplyDefaultRoleCapabilities(ctx)
		if err != nil {
			return setupActionResult{}, err
		}
		return actionResult(key, "ok", "已应用默认 AI 角色能力", "", map[string]any{"updated": updated}), nil
	case "ensure-default-team":
		return s.ensureDefaultResearchTeam(ctx)
	case "ensure-paper-defaults":
		overview, err := s.paperUsecase.Overview(ctx)
		if err != nil {
			return setupActionResult{}, err
		}
		accounts, _ := s.paperUsecase.ListAccounts(ctx)
		riskConfigs, _ := s.paperUsecase.ListRiskConfigs(ctx)
		return actionResult(key, "ok", "已检查模拟盘默认账户和风控", "", map[string]any{"overview": overview, "accounts": len(accounts), "riskConfigs": len(riskConfigs)}), nil
	case "ensure-message-filter":
		filter, err := s.messagingUsecase.EnsureDefaultSubscriptionFilter(ctx)
		if err != nil {
			return setupActionResult{}, err
		}
		output := map[string]any{}
		if filter != nil {
			output["filterId"] = filter.ID
			output["filterName"] = filter.Name
		}
		return actionResult(key, "ok", "已检查默认消息过滤器", "", output), nil
	case "ensure-prediction-market-defaults":
		return s.ensurePredictionMarketDefaults(ctx)
	case "sync-market-symbols":
		provider, synced, err := s.marketUsecase.SyncSymbols(ctx)
		if err != nil {
			return setupActionResult{}, err
		}
		return actionResult(key, "ok", "证券列表同步完成", "", map[string]any{"provider": provider, "synced": synced}), nil
	case "test-proxy":
		report, err := s.settingsUsecase.TestProxyConfig(ctx)
		if err != nil {
			return setupActionResult{}, err
		}
		return actionResult(key, "ok", "代理测试完成", "", map[string]any{"checkedAt": report.CheckedAt, "results": report.Results}), nil
	default:
		return setupActionResult{}, fmt.Errorf("unsupported setup action: %s", key)
	}
}

func (s *Server) ensurePredictionMarketDefaults(ctx context.Context) (setupActionResult, error) {
	team, created, err := s.researchUsecase.EnsureDefaultPredictionTeam(ctx)
	if err != nil {
		return setupActionResult{}, err
	}
	roleCount := 0
	if team != nil {
		roles, err := s.researchUsecase.ListRoles(ctx, team.ID)
		if err != nil {
			return setupActionResult{}, err
		}
		roleCount = len(roles)
		if roleCount == 0 {
			roleCount, err = s.researchUsecase.ApplyDefaultRoles(ctx, team.ID)
			if err != nil {
				return setupActionResult{}, err
			}
		}
	}
	filter, err := s.messagingUsecase.EnsureDefaultPredictionSubscriptionFilter(ctx)
	if err != nil {
		return setupActionResult{}, err
	}
	output := map[string]any{"createdTeam": created, "roleCount": roleCount}
	if team != nil {
		output["teamId"] = team.ID
		output["teamName"] = team.Name
	}
	if filter != nil {
		output["filterId"] = filter.ID
		output["filterName"] = filter.Name
	}
	return actionResult("ensure-prediction-market-defaults", "ok", "已检查预测市场默认团队和过滤器", "", output), nil
}

func (s *Server) syncAllProviderModels(ctx context.Context) (setupActionResult, error) {
	providers, err := s.aiUsecase.ListProviders(ctx)
	if err != nil {
		return setupActionResult{}, err
	}
	synced := 0
	attempted := 0
	errorsByProvider := map[string]string{}
	for _, provider := range providers {
		if !provider.Enabled {
			continue
		}
		attempted++
		rows, err := s.aiUsecase.SyncProviderModels(ctx, provider.ID)
		if err != nil {
			errorsByProvider[strconv.FormatUint(uint64(provider.ID), 10)] = err.Error()
			continue
		}
		synced += len(rows)
	}
	status := "ok"
	detail := ""
	if len(errorsByProvider) > 0 {
		status = "warning"
		detail = "部分服务商同步失败，请在模型服务商页面查看 API Key、Base URL 和模型权限。"
	}
	if attempted == 0 {
		status = "warning"
		detail = "没有启用的 AI 服务商。"
	}
	return actionResult("sync-ai-models", status, "模型同步动作已完成", detail, map[string]any{
		"attemptedProviders": attempted,
		"syncedModels":       synced,
		"errors":             errorsByProvider,
	}), nil
}

func (s *Server) ensureDefaultResearchTeam(ctx context.Context) (setupActionResult, error) {
	accounts, err := s.paperUsecase.ListAccounts(ctx)
	if err != nil {
		return setupActionResult{}, err
	}
	if len(accounts) == 0 {
		return setupActionResult{}, fmt.Errorf("paper account is required before creating a default research team")
	}
	team, created, err := s.researchUsecase.EnsureDefaultTeam(ctx, accounts[0].Account.ID)
	if err != nil {
		return setupActionResult{}, err
	}
	roleCount := 0
	if team != nil {
		roles, err := s.researchUsecase.ListRoles(ctx, team.ID)
		if err != nil {
			return setupActionResult{}, err
		}
		roleCount = len(roles)
		if roleCount == 0 {
			roleCount, err = s.researchUsecase.ApplyDefaultRoles(ctx, team.ID)
			if err != nil {
				return setupActionResult{}, err
			}
		}
	}
	output := map[string]any{"created": created, "roleCount": roleCount}
	if team != nil {
		output["teamId"] = team.ID
		output["teamName"] = team.Name
	}
	return actionResult("ensure-default-team", "ok", "已检查默认投研团队", "", output), nil
}

func actionResult(key, status, summary, detail string, output map[string]any) setupActionResult {
	return setupActionResult{Key: key, Status: status, Summary: summary, Detail: detail, Output: output, RanAt: time.Now()}
}

func (s *Server) logReadiness(ctx context.Context) (bool, string) {
	rows, err := s.settingsUsecase.RuntimeEnv(ctx)
	if err != nil {
		return false, err.Error()
	}
	for _, row := range rows {
		if row.Key == "LOG_DIR" {
			text := strings.TrimSpace(fmt.Sprint(row.Value))
			return text != "", fmt.Sprintf("日志目录 %s", firstNonEmptyString(text, "-"))
		}
	}
	return false, "未找到日志目录配置"
}

func queueReadiness(systemStatus map[string]any, summary map[string]any) (bool, string, string) {
	mode := strings.TrimSpace(fmt.Sprint(summary["meetingDispatchMode"]))
	if mode == "" || mode == "local" {
		return true, "ready", "本地队列模式"
	}
	keys := []string{"redis", "worker", "scheduler"}
	unhealthy := []string{}
	for _, key := range keys {
		item := mapValue(systemStatus, key)
		status := strings.TrimSpace(fmt.Sprint(item["status"]))
		if status != "ok" && status != "disabled" {
			unhealthy = append(unhealthy, key+":"+status)
		}
	}
	if len(unhealthy) == 0 {
		return true, "ready", "Redis、Worker、Scheduler 正常"
	}
	return false, "error", "异常：" + strings.Join(unhealthy, ", ")
}

func readinessStatus(ready bool, configured bool) string {
	if ready {
		return "ready"
	}
	if configured {
		return "warning"
	}
	return "missing"
}

func statusFromReady(ready bool, err error) string {
	if err != nil {
		return "error"
	}
	if ready {
		return "ready"
	}
	return "missing"
}

func predictionMatchHealthSummary(rows []appprediction.MatchRow, err error) string {
	if err != nil {
		return "匹配样本不可用：" + err.Error()
	}
	if len(rows) == 0 {
		return "暂无最近匹配样本"
	}
	counts := map[string]int{}
	for _, row := range rows {
		status := strings.TrimSpace(row.Match.Status)
		if status == "" {
			status = "unknown"
		}
		counts[status]++
	}
	parts := []string{}
	for _, status := range []string{"linked", "confirmed", "review_required", "candidate", "rejected", "unknown"} {
		if count := counts[status]; count > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", status, count))
		}
	}
	return fmt.Sprintf("近 %d 条：%s", len(rows), strings.Join(parts, ", "))
}

func predictionConnectivitySummary(health map[string]any) string {
	if len(health) == 0 {
		return "未配置"
	}
	parts := []string{}
	for _, key := range []string{"gammaConnectivity", "clobConnectivity", "wsConnectivity"} {
		value := strings.TrimSpace(fmt.Sprint(health[key]))
		if value == "" || value == "<nil>" {
			value = "not_checked"
		}
		parts = append(parts, fmt.Sprintf("%s=%s", strings.TrimSuffix(key, "Connectivity"), value))
	}
	if status := strings.TrimSpace(fmt.Sprint(health["status"])); status != "" && status != "<nil>" {
		parts = append(parts, "status="+status)
	}
	return strings.Join(parts, ", ")
}

func adminSummary(required bool, err error) string {
	if err != nil {
		return err.Error()
	}
	if required {
		return "尚未初始化管理员"
	}
	return "管理员已初始化"
}

func proxySummary(enabled bool, proxyURL string) string {
	if !enabled {
		return "未启用代理"
	}
	if strings.TrimSpace(proxyURL) == "" {
		return "已启用代理模块，但未保存代理 URL"
	}
	return "代理 URL 已配置"
}

func anyReadyProvider(rows []any) bool {
	return countReadyProviders(rows) > 0
}

func countReadyProviders(rows []any) int {
	count := 0
	for _, row := range rows {
		if boolValue(mapValue(row, "ready")["ready"]) {
			count++
			continue
		}
		if item, ok := row.(map[string]any); ok && boolValue(item["ready"]) {
			count++
		}
	}
	return count
}

func listValue(source any, key string) []any {
	item := mapValue(source, key)[key]
	if rows, ok := item.([]any); ok {
		return rows
	}
	return []any{}
}

func mapValue(source any, keys ...string) map[string]any {
	current := source
	if len(keys) == 0 {
		if typed, ok := current.(map[string]any); ok {
			return typed
		}
		return map[string]any{}
	}
	for _, key := range keys {
		if strings.Contains(key, ".") {
			for _, part := range strings.Split(key, ".") {
				current = mapValue(current)[part]
			}
			continue
		}
		current = mapValue(current)[key]
	}
	if typed, ok := current.(map[string]any); ok {
		return typed
	}
	return map[string]any{keys[len(keys)-1]: current}
}

func stringValueFromMap(source map[string]any, dottedKey string) string {
	current := any(source)
	for _, key := range strings.Split(dottedKey, ".") {
		current = mapValue(current)[key]
	}
	return strings.TrimSpace(fmt.Sprint(current))
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	default:
		parsed, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value)))
		return parsed
	}
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return fmt.Sprint(value) == "true"
	}
}

func yesNo(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func firstError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}
