package httptransport

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	applogging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/logging"
	appops "github.com/TradingCopilotDevs/TradingCopilot/internal/app/ops"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http/jsonapi"
	"github.com/hibiken/asynq"
	"github.com/shopspring/decimal"
)

func (s *Server) providerHealth(w http.ResponseWriter, r *http.Request) {
	dashboard, err := s.dashboardUsecase.Load(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "provider-health-load-failed", "Provider health load failed", err.Error(), "")
		return
	}
	diagnostics := mapValue(dashboard, "dependencyDiagnostics")
	predictionHealth := s.predictionProviderHealth(r.Context())
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("provider-health-reports", "current", map[string]any{
		"generatedAt":      time.Now(),
		"ai":               mapValue(diagnostics, "ai"),
		"meeting":          mapValue(diagnostics, "meeting"),
		"market":           mapValue(diagnostics, "market"),
		"messaging":        mapValue(diagnostics, "messaging"),
		"predictionMarket": predictionHealth,
	}))
}

func (s *Server) predictionProviderHealth(ctx context.Context) map[string]any {
	health := map[string]any{
		"provider":             "polymarket",
		"gammaBaseUrl":         "https://gamma-api.polymarket.com",
		"clobBaseUrl":          "https://clob.polymarket.com",
		"marketWebSocketUrl":   "wss://ws-subscriptions-clob.polymarket.com/ws/market",
		"proxyModule":          "market",
		"realtimeMode":         "meeting_websocket_snapshot_with_rest_fallback",
		"webSocketRunnerState": "enabled_on_linked_prediction_meetings",
	}
	if !s.predictionUsecase.Configured() {
		health["status"] = "skipped"
		health["lastError"] = "prediction usecase is not configured"
		return health
	}
	probe := s.predictionUsecase.ProviderHealth(ctx)
	for key, value := range probe {
		health[key] = value
	}
	result, err := s.predictionUsecase.Search(ctx, "", 1)
	if err != nil {
		health["status"] = "warning"
		health["lastError"] = err.Error()
		return health
	}
	health["status"] = "ok"
	for _, key := range []string{"gammaConnectivity", "clobConnectivity", "wsConnectivity"} {
		if strings.TrimSpace(fmt.Sprint(probe[key])) == "error" {
			health["status"] = "warning"
			break
		}
	}
	health["storedMarketSampleCount"] = len(result.Rows)
	if len(result.Rows) > 0 {
		health["lastStoredMarketSyncTime"] = result.Rows[0].UpdatedAt
	}
	return health
}

func (s *Server) opsJobs(w http.ResponseWriter, r *http.Request) {
	dashboard, err := s.dashboardUsecase.Load(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "ops-jobs-load-failed", "Ops jobs load failed", err.Error(), "")
		return
	}
	systemStatus := mapValue(dashboard, "systemStatus")
	summary := mapValue(dashboard, "summary")
	recentErrors := s.recentErrorDiagnostics(r.Context(), 8)
	queueFilters := queueDiagnosticsFiltersFromRequest(r)
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("ops-job-reports", "current", map[string]any{
		"generatedAt":         time.Now(),
		"meetingDispatchMode": summary["meetingDispatchMode"],
		"paperExecutionMode":  summary["paperExecutionMode"],
		"queue":               s.queueDiagnostics(queueFilters),
		"recentErrors":        recentErrors,
		"redis":               mapValue(systemStatus, "redis"),
		"worker":              mapValue(systemStatus, "worker"),
		"scheduler":           mapValue(systemStatus, "scheduler"),
		"messageListener":     mapValue(systemStatus, "messageSubscriptionListener"),
		"paperEngine":         mapValue(systemStatus, "paperEngine"),
	}))
}

func (s *Server) retryFailedOpsJobs(w http.ResponseWriter, r *http.Request) {
	result, err := s.retryFailedQueueTasks()
	if err != nil {
		writeJSONAPIError(w, http.StatusConflict, "ops-queue-retry-failed", "Queue retry failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("ops-job-actions", "retry-failed", result))
}

func (s *Server) runOpsJobTask(w http.ResponseWriter, r *http.Request, queue string, taskID string) {
	result, err := s.runQueueTask(queue, taskID)
	if err != nil {
		writeJSONAPIError(w, http.StatusConflict, "ops-queue-task-run-failed", "Queue task run failed", err.Error(), "")
		return
	}
	id := fmt.Sprintf("%s:%s", result["queue"], result["taskId"])
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("ops-job-actions", id, result))
}

func (s *Server) opsMetrics(w http.ResponseWriter, r *http.Request) {
	dashboard, err := s.dashboardUsecase.Load(r.Context())
	if err != nil {
		http.Error(w, "provider metrics load failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	summary := mapValue(dashboard, "summary")
	metrics := mapValue(dashboard, "businessMetrics")
	systemStatus := mapValue(dashboard, "systemStatus")
	diagnostics := mapValue(dashboard, "dependencyDiagnostics")
	aiUsage := mapValue(diagnostics, "ai.usage")
	aiCostCoverage := mapValue(aiUsage, "costRateCoverage")
	meetingRuntime := mapValue(diagnostics, "meeting.runtime")
	marketProvider := mapValue(diagnostics, "market.provider")
	messageSubscriptions := mapValue(diagnostics, "messaging.provider.subscriptions")
	queue := s.queueDiagnostics(defaultQueueDiagnosticsFilters())
	queueTotals := mapValue(queue, "totals")
	recentErrors := s.recentErrorDiagnostics(r.Context(), 8)
	var b strings.Builder
	emitted := map[string]bool{}
	writeMetric := func(name string, help string, labels map[string]string, value any) {
		if emitted[name] {
			help = ""
		}
		emitted[name] = true
		writePromGauge(&b, name, help, labels, value)
	}
	writeMetric("tradingcopilot_info", "Static TradingCopilot runtime information.", map[string]string{
		"app":             s.settings.AppName,
		"env":             s.settings.AppEnv,
		"databaseBackend": fmt.Sprint(summary["databaseBackend"]),
		"meetingMode":     fmt.Sprint(summary["meetingDispatchMode"]),
	}, 1)
	writeMetric("tradingcopilot_ai_providers_total", "Configured AI providers.", nil, metrics["aiProviderCount"])
	writeMetric("tradingcopilot_ai_providers_ready", "Ready AI providers.", nil, metrics["aiReadyProviderCount"])
	writeMetric("tradingcopilot_ai_model_calls_24h", "AI model calls with captured usage in the last 24 hours.", nil, aiUsage["modelCalls24h"])
	writeMetric("tradingcopilot_ai_prompt_tokens_24h", "AI prompt tokens captured in the last 24 hours.", nil, aiUsage["promptTokens24h"])
	writeMetric("tradingcopilot_ai_completion_tokens_24h", "AI completion tokens captured in the last 24 hours.", nil, aiUsage["completionTokens24h"])
	writeMetric("tradingcopilot_ai_total_tokens_24h", "AI total tokens captured in the last 24 hours.", nil, aiUsage["totalTokens24h"])
	writeMetric("tradingcopilot_ai_total_tokens_total", "AI total tokens captured since retention began.", nil, aiUsage["totalTokensTotal"])
	writeMetric("tradingcopilot_ai_cost_amount_24h", "AI cost captured or estimated in the last 24 hours.", map[string]string{"source": fmt.Sprint(aiUsage["costAmountSource"]), "currency": fmt.Sprint(aiUsage["costCurrency"])}, aiUsage["costAmount24h"])
	writeMetric("tradingcopilot_ai_cost_budget_used_pct", "Daily AI cost budget usage percentage.", map[string]string{"status": fmt.Sprint(aiUsage["costBudgetStatus"])}, aiUsage["costBudgetUsedPct"])
	writeMetric("tradingcopilot_ai_cost_missing_calls_24h", "AI model calls without provider payload cost or configured price in the last 24 hours.", nil, aiUsage["costMissingCalls24h"])
	writeMetric("tradingcopilot_ai_cost_rate_models_total", "Enabled AI provider models checked for configured cost rates.", map[string]string{"status": fmt.Sprint(aiCostCoverage["status"])}, aiCostCoverage["modelCount"])
	writeMetric("tradingcopilot_ai_cost_rate_missing_models", "Enabled AI provider models without configured cost rates.", map[string]string{"status": fmt.Sprint(aiCostCoverage["status"])}, aiCostCoverage["missingModelCount"])
	writeMetric("tradingcopilot_ai_cost_rate_coverage_pct", "Configured AI cost rate coverage percentage across enabled provider models.", map[string]string{"status": fmt.Sprint(aiCostCoverage["status"])}, aiCostCoverage["coveragePct"])
	writeMetric("tradingcopilot_ai_latency_observed_calls_24h", "AI model calls with observed latency in the last 24 hours.", nil, aiUsage["latencyObservedCalls24h"])
	writeMetric("tradingcopilot_ai_latency_avg_ms_24h", "Average observed AI model latency in milliseconds in the last 24 hours.", nil, aiUsage["latencyAvgMs24h"])
	writeMetric("tradingcopilot_ai_latency_p95_ms_24h", "P95 observed AI model latency in milliseconds in the last 24 hours.", nil, aiUsage["latencyP95Ms24h"])
	writeMetric("tradingcopilot_meetings_total", "Total meetings.", nil, metrics["meetingsTotal"])
	writeMetric("tradingcopilot_meetings_running", "Running meetings.", nil, metrics["meetingsRunning"])
	writeMetric("tradingcopilot_meetings_failed_24h", "Meetings failed in the last 24 hours.", nil, metrics["meetingsFailed24h"])
	writeMetric("tradingcopilot_meeting_queue_wait_avg_seconds_24h", "Average meeting queue wait in the last 24 hours.", nil, meetingRuntime["queueWaitAvgSeconds"])
	writeMetric("tradingcopilot_meeting_run_avg_seconds_24h", "Average meeting runtime in the last 24 hours.", nil, meetingRuntime["runAvgSeconds"])
	writeMetric("tradingcopilot_meeting_run_p95_seconds_24h", "P95 meeting runtime in the last 24 hours.", nil, meetingRuntime["runP95Seconds"])
	writeMetric("tradingcopilot_messages_ingested_24h", "Messages ingested in the last 24 hours.", nil, metrics["ingestedMessages24h"])
	writeMetric("tradingcopilot_messages_unfiltered", "Ingested messages waiting for filtering.", nil, metrics["ingestedUnfilteredCount"])
	writeMetric("tradingcopilot_market_symbols_total", "Tracked market symbols.", nil, metrics["marketSymbolCount"])
	writeMetric("tradingcopilot_market_watchlist_active", "Active watchlist items.", nil, metrics["marketActiveWatchlistCount"])
	writeMetric("tradingcopilot_market_last_quote_age_seconds", "Age of the latest realtime quote.", map[string]string{"freshness": fmt.Sprint(marketProvider["freshnessStatus"])}, marketProvider["lastQuoteAgeSeconds"])
	writeMetric("tradingcopilot_market_source_health_score", "Simple market data source health score.", map[string]string{"freshness": fmt.Sprint(marketProvider["freshnessStatus"])}, marketProvider["sourceHealthScore"])
	writeMetric("tradingcopilot_paper_accounts_total", "Paper trading accounts.", nil, metrics["paperAccountCount"])
	writeMetric("tradingcopilot_paper_accounts_active", "Active paper trading accounts.", nil, metrics["paperActiveAccountCount"])
	writeMetric("tradingcopilot_paper_total_equity", "Paper trading total equity.", nil, metrics["paperTotalEquity"])
	writeMetric("tradingcopilot_paper_pending_orders", "Pending or suggested paper orders.", nil, metrics["paperPendingOrderCount"])
	writeMetric("tradingcopilot_paper_is_trading_time", "Whether the paper engine currently sees an A-share trading session.", nil, boolToNumber(boolValue(metrics["paperIsTradingTime"])))
	writeMetric("tradingcopilot_wake_plans_active", "Active wake plans.", nil, metrics["wakeActiveCount"])
	writeMetric("tradingcopilot_wake_plans_overdue", "Overdue wake plans.", nil, metrics["wakeOverdueCount"])
	writeMetric("tradingcopilot_message_filter_failed", "Messages whose filter job failed.", nil, messageSubscriptions["failedFilterMessages"])
	writeMetric("tradingcopilot_message_collect_errors", "Message subscriptions with latest collection errors.", nil, messageSubscriptions["collectErrorCount"])
	writeMetric("tradingcopilot_message_subscriptions_due", "Enabled message subscriptions due for collection.", nil, messageSubscriptions["due"])
	for _, key := range []string{"size", "pending", "active", "scheduled", "retry", "archived", "failedToday", "failedTotal"} {
		writeMetric("tradingcopilot_queue_"+promKey(key), "Asynq queue total "+key+".", map[string]string{"status": fmt.Sprint(queue["status"])}, queueTotals[key])
	}
	writeMetric("tradingcopilot_queue_failed_task_samples", "Failed queue task samples exposed by the ops endpoint.", map[string]string{"status": fmt.Sprint(queue["status"])}, listLength(queue["failedTasks"]))
	writeMetric("tradingcopilot_recent_errors_24h", "Recent application error log entries in the last 24 hours.", map[string]string{"status": fmt.Sprint(recentErrors["status"])}, recentErrors["count"])
	for _, item := range []struct {
		component string
		value     map[string]any
	}{
		{component: "redis", value: mapValue(systemStatus, "redis")},
		{component: "worker", value: mapValue(systemStatus, "worker")},
		{component: "scheduler", value: mapValue(systemStatus, "scheduler")},
		{component: "message_listener", value: mapValue(systemStatus, "messageSubscriptionListener")},
		{component: "paper_engine", value: mapValue(systemStatus, "paperEngine")},
	} {
		status := strings.TrimSpace(fmt.Sprint(item.value["status"]))
		writeMetric("tradingcopilot_dependency_status", "Dependency status as a labeled gauge.", map[string]string{"component": item.component, "status": status}, boolToNumber(status == "ok"))
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(b.String()))
}

func (s *Server) recentErrorDiagnostics(ctx context.Context, limit int) map[string]any {
	if limit <= 0 {
		limit = 8
	}
	since := time.Now().Add(-24 * time.Hour).UTC()
	result, err := s.loggingUsecase.Query(ctx, applogging.Query{
		From:         &since,
		Level:        "error",
		Limit:        limit,
		IncludeNoise: false,
	})
	if err != nil {
		return map[string]any{
			"status":      "warning",
			"summary":     "recent error logs unavailable",
			"detail":      err.Error(),
			"windowHours": 24,
			"count":       0,
			"entries":     []map[string]any{},
		}
	}
	entries := make([]map[string]any, 0, len(result.Entries))
	for _, entry := range result.Entries {
		entries = append(entries, recentErrorEntryMap(entry))
	}
	status := "ok"
	summary := "no recent errors"
	if len(entries) > 0 {
		status = "warning"
		summary = fmt.Sprintf("%d recent error log entrie(s)", len(entries))
	}
	return map[string]any{
		"status":      status,
		"summary":     summary,
		"detail":      "Latest error-level structured log entries from the last 24 hours.",
		"windowHours": 24,
		"count":       len(entries),
		"entries":     entries,
	}
}

func recentErrorEntryMap(entry applogging.Entry) map[string]any {
	return map[string]any{
		"id":         entry.ID,
		"time":       zeroTimeNil(entry.Time),
		"level":      entry.Level,
		"role":       entry.Role,
		"source":     entry.Source,
		"event":      entry.Event,
		"group":      entry.Group,
		"method":     entry.Method,
		"path":       entry.Path,
		"status":     entry.Status,
		"message":    entry.Message,
		"caller":     entry.Caller,
		"durationMs": entry.DurationMS,
		"file":       entry.File,
	}
}

type queueDiagnosticsFilters struct {
	Queue string
	Type  string
	State string
	Limit int
}

func queueDiagnosticsFiltersFromRequest(r *http.Request) queueDiagnosticsFilters {
	query := r.URL.Query()
	limit := defaultQueueDiagnosticsFilters().Limit
	if parsed, err := strconv.Atoi(strings.TrimSpace(query.Get("failedLimit"))); err == nil && parsed > 0 {
		limit = parsed
	}
	if limit > 50 {
		limit = 50
	}
	state := strings.ToLower(strings.TrimSpace(query.Get("state")))
	if state != "retry" && state != "archived" {
		state = ""
	}
	return queueDiagnosticsFilters{
		Queue: strings.TrimSpace(query.Get("queue")),
		Type:  strings.TrimSpace(query.Get("type")),
		State: state,
		Limit: limit,
	}
}

func defaultQueueDiagnosticsFilters() queueDiagnosticsFilters {
	return queueDiagnosticsFilters{Limit: 8}
}

func (s *Server) queueDiagnostics(filters queueDiagnosticsFilters) map[string]any {
	if strings.TrimSpace(s.settings.RedisURL) == "" || strings.EqualFold(strings.TrimSpace(s.settings.MeetingMode), "local") {
		return map[string]any{
			"status":      "disabled",
			"summary":     "local dispatch mode",
			"detail":      "Redis queue inspection is disabled when meeting dispatch mode is local.",
			"queues":      []map[string]any{},
			"totals":      emptyQueueTotals(),
			"failedTasks": []map[string]any{},
			"filters":     queueDiagnosticsFilterMap(filters),
			"backlogRisk": queueBacklogRisk(emptyQueueTotals()),
			"actions":     queueActions(false),
			"generatedAt": time.Now(),
		}
	}
	inspector := asynq.NewInspector(redisClientOpt(s.settings.RedisURL))
	defer inspector.Close()
	queues, err := inspector.Queues()
	if err != nil {
		return map[string]any{
			"status":      "error",
			"summary":     "queue inspection failed",
			"detail":      err.Error(),
			"queues":      []map[string]any{},
			"totals":      emptyQueueTotals(),
			"failedTasks": []map[string]any{},
			"filters":     queueDiagnosticsFilterMap(filters),
			"backlogRisk": queueBacklogRisk(emptyQueueTotals()),
			"actions":     queueActions(false),
			"generatedAt": time.Now(),
		}
	}
	if len(queues) == 0 {
		queues = []string{"default"}
	}
	sort.Strings(queues)
	sampleQueues := queues
	if filters.Queue != "" {
		sampleQueues = []string{filters.Queue}
	}
	rows := []map[string]any{}
	totals := emptyQueueTotals()
	for _, name := range queues {
		info, err := inspector.GetQueueInfo(name)
		if err != nil {
			rows = append(rows, map[string]any{"name": name, "status": "error", "detail": err.Error()})
			continue
		}
		row := queueInfoMap(info)
		rows = append(rows, row)
		addQueueTotals(totals, row)
	}
	failedTasks := failedQueueTaskSamples(inspector, sampleQueues, filters)
	status := "ok"
	summary := "queue healthy"
	if intValue(totals["retry"]) > 0 || intValue(totals["archived"]) > 0 || intValue(totals["failedToday"]) > 0 {
		status = "warning"
		summary = "queue has failed or retrying tasks"
	}
	return map[string]any{
		"status":      status,
		"summary":     summary,
		"detail":      "Asynq queue snapshot from Redis.",
		"queueCount":  len(rows),
		"queues":      rows,
		"totals":      totals,
		"failedTasks": failedTasks,
		"filters":     queueDiagnosticsFilterMap(filters),
		"backlogRisk": queueBacklogRisk(totals),
		"actions":     queueActions(true),
		"generatedAt": time.Now(),
	}
}

func (s *Server) retryFailedQueueTasks() (map[string]any, error) {
	if strings.TrimSpace(s.settings.RedisURL) == "" || strings.EqualFold(strings.TrimSpace(s.settings.MeetingMode), "local") {
		return nil, fmt.Errorf("Redis queue retry is disabled when meeting dispatch mode is local")
	}
	inspector := asynq.NewInspector(redisClientOpt(s.settings.RedisURL))
	defer inspector.Close()
	queues, err := inspector.Queues()
	if err != nil {
		return nil, err
	}
	sort.Strings(queues)
	rows := []map[string]any{}
	totalRetry := 0
	totalArchived := 0
	for _, queue := range queues {
		retryCount, retryErr := inspector.RunAllRetryTasks(queue)
		archivedCount, archivedErr := inspector.RunAllArchivedTasks(queue)
		row := map[string]any{
			"queue":         queue,
			"retryCount":    retryCount,
			"archivedCount": archivedCount,
			"status":        "ok",
		}
		if retryErr != nil || archivedErr != nil {
			row["status"] = "warning"
			row["error"] = strings.Trim(strings.Join([]string{errorString(retryErr), errorString(archivedErr)}, " "), " ")
		}
		totalRetry += retryCount
		totalArchived += archivedCount
		rows = append(rows, row)
	}
	return map[string]any{
		"status":              "submitted",
		"action":              "retry_failed",
		"retriedRetryCount":   totalRetry,
		"retriedArchiveCount": totalArchived,
		"totalSubmitted":      totalRetry + totalArchived,
		"queues":              rows,
		"ranAt":               time.Now(),
		"destructive":         false,
		"detail":              "Retry and archived tasks were moved back to pending processing where supported by asynq.",
	}, nil
}

func (s *Server) runQueueTask(queue string, taskID string) (map[string]any, error) {
	queue = strings.TrimSpace(queue)
	taskID = strings.TrimSpace(taskID)
	if queue == "" || taskID == "" {
		return nil, fmt.Errorf("queue and task id are required")
	}
	if strings.TrimSpace(s.settings.RedisURL) == "" || strings.EqualFold(strings.TrimSpace(s.settings.MeetingMode), "local") {
		return nil, fmt.Errorf("Redis queue task operation is disabled when meeting dispatch mode is local")
	}
	inspector := asynq.NewInspector(redisClientOpt(s.settings.RedisURL))
	defer inspector.Close()
	info, _ := inspector.GetTaskInfo(queue, taskID)
	if err := inspector.RunTask(queue, taskID); err != nil {
		return nil, err
	}
	result := map[string]any{
		"status":      "submitted",
		"action":      "run_task",
		"queue":       queue,
		"taskId":      taskID,
		"ranAt":       time.Now(),
		"destructive": false,
		"detail":      "Task was moved back to pending processing where supported by asynq.",
	}
	if info != nil {
		result["taskType"] = info.Type
		result["state"] = info.State.String()
		result["retried"] = info.Retried
		result["maxRetry"] = info.MaxRetry
		result["lastError"] = info.LastErr
	}
	return result, nil
}

func redisClientOpt(redisURL string) asynq.RedisClientOpt {
	opt, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return asynq.RedisClientOpt{Addr: "127.0.0.1:6379"}
	}
	if clientOpt, ok := opt.(asynq.RedisClientOpt); ok {
		return clientOpt
	}
	return asynq.RedisClientOpt{Addr: "127.0.0.1:6379"}
}

func queueInfoMap(info *asynq.QueueInfo) map[string]any {
	return map[string]any{
		"name":              info.Queue,
		"status":            "ok",
		"paused":            info.Paused,
		"size":              info.Size,
		"pending":           info.Pending,
		"active":            info.Active,
		"scheduled":         info.Scheduled,
		"retry":             info.Retry,
		"archived":          info.Archived,
		"completed":         info.Completed,
		"aggregating":       info.Aggregating,
		"processedToday":    info.Processed,
		"failedToday":       info.Failed,
		"processedTotal":    info.ProcessedTotal,
		"failedTotal":       info.FailedTotal,
		"latencySeconds":    int(info.Latency.Seconds()),
		"memoryUsageBytes":  info.MemoryUsage,
		"snapshotTimestamp": info.Timestamp,
	}
}

func queueDiagnosticsFilterMap(filters queueDiagnosticsFilters) map[string]any {
	return map[string]any{
		"queue":       filters.Queue,
		"type":        filters.Type,
		"state":       filters.State,
		"failedLimit": filters.Limit,
	}
}

func failedQueueTaskSamples(inspector *asynq.Inspector, queues []string, filters queueDiagnosticsFilters) []map[string]any {
	if filters.Limit <= 0 {
		return []map[string]any{}
	}
	rows := []map[string]any{}
	for _, queue := range queues {
		if filters.State == "" || filters.State == "retry" {
			for _, item := range failedTaskList(inspector, queue, "retry", filters.Type, filters.Limit-len(rows)) {
				rows = append(rows, item)
			}
		}
		if len(rows) >= filters.Limit {
			return rows
		}
		if filters.State == "" || filters.State == "archived" {
			for _, item := range failedTaskList(inspector, queue, "archived", filters.Type, filters.Limit-len(rows)) {
				rows = append(rows, item)
			}
		}
		if len(rows) >= filters.Limit {
			return rows
		}
	}
	return rows
}

func failedTaskList(inspector *asynq.Inspector, queue string, state string, taskType string, limit int) []map[string]any {
	if limit <= 0 {
		return nil
	}
	var tasks []*asynq.TaskInfo
	var err error
	switch state {
	case "retry":
		tasks, err = inspector.ListRetryTasks(queue, asynq.PageSize(limit), asynq.Page(1))
	case "archived":
		tasks, err = inspector.ListArchivedTasks(queue, asynq.PageSize(limit), asynq.Page(1))
	default:
		return nil
	}
	if err != nil {
		return []map[string]any{{"queue": queue, "state": state, "status": "error", "lastError": err.Error()}}
	}
	rows := make([]map[string]any, 0, len(tasks))
	for _, task := range tasks {
		if taskType != "" && task != nil && task.Type != taskType {
			continue
		}
		rows = append(rows, queueTaskInfoMap(task))
	}
	return rows
}

func queueTaskInfoMap(task *asynq.TaskInfo) map[string]any {
	if task == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":             task.ID,
		"queue":          task.Queue,
		"type":           task.Type,
		"state":          task.State.String(),
		"maxRetry":       task.MaxRetry,
		"retried":        task.Retried,
		"lastError":      task.LastErr,
		"lastFailedAt":   zeroTimeNil(task.LastFailedAt),
		"nextProcessAt":  zeroTimeNil(task.NextProcessAt),
		"timeoutSeconds": int(task.Timeout.Seconds()),
		"payloadPreview": payloadPreview(task.Payload),
	}
}

func queueBacklogRisk(totals map[string]any) map[string]any {
	pending := intValue(totals["pending"])
	scheduled := intValue(totals["scheduled"])
	retry := intValue(totals["retry"])
	archived := intValue(totals["archived"])
	backlog := pending + scheduled + retry + archived
	level := "ok"
	reason := "queue backlog is within the normal range"
	switch {
	case archived > 0:
		level = "danger"
		reason = "archived failed tasks require operator review or retry"
	case retry > 0:
		level = "warning"
		reason = "retrying tasks may indicate provider or worker instability"
	case backlog >= 1000:
		level = "warning"
		reason = "queue backlog is high"
	case backlog == 0:
		reason = "queue is empty"
	}
	return map[string]any{
		"level":          level,
		"reason":         reason,
		"backlogCount":   backlog,
		"pendingCount":   pending,
		"scheduledCount": scheduled,
		"retryCount":     retry,
		"archivedCount":  archived,
	}
}

func queueActions(enabled bool) map[string]any {
	return map[string]any{
		"retryFailed": map[string]any{
			"enabled":     enabled,
			"method":      "POST",
			"path":        "/api/ops/jobs/retry-failed",
			"description": "Move retry and archived tasks back to pending processing.",
		},
		"runTask": map[string]any{
			"enabled":     enabled,
			"method":      "POST",
			"path":        "/api/ops/jobs/{queue}/tasks/{taskId}/run",
			"description": "Move a single failed task back to pending processing.",
		},
	}
}

func emptyQueueTotals() map[string]any {
	return map[string]any{
		"size": 0, "pending": 0, "active": 0, "scheduled": 0, "retry": 0, "archived": 0, "completed": 0,
		"aggregating": 0, "processedToday": 0, "failedToday": 0, "processedTotal": 0, "failedTotal": 0,
		"memoryUsageBytes": int64(0),
	}
}

func zeroTimeNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func payloadPreview(payload []byte) string {
	const max = 240
	text := strings.TrimSpace(string(payload))
	if len(text) <= max {
		return text
	}
	return text[:max] + "..."
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func addQueueTotals(totals map[string]any, row map[string]any) {
	for _, key := range []string{"size", "pending", "active", "scheduled", "retry", "archived", "completed", "aggregating", "processedToday", "failedToday", "processedTotal", "failedTotal"} {
		totals[key] = intValue(totals[key]) + intValue(row[key])
	}
	totals["memoryUsageBytes"] = int64Value(totals["memoryUsageBytes"]) + int64Value(row["memoryUsageBytes"])
}

func int64Value(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return int64(intValue(value))
	}
}

func listLength(value any) int {
	switch typed := value.(type) {
	case []map[string]any:
		return len(typed)
	case []any:
		return len(typed)
	default:
		return 0
	}
}

func writePromGauge(builder *strings.Builder, name string, help string, labels map[string]string, value any) {
	if help != "" {
		builder.WriteString("# HELP ")
		builder.WriteString(name)
		builder.WriteByte(' ')
		builder.WriteString(strings.ReplaceAll(help, "\n", " "))
		builder.WriteByte('\n')
		builder.WriteString("# TYPE ")
		builder.WriteString(name)
		builder.WriteString(" gauge\n")
	}
	builder.WriteString(name)
	if len(labels) > 0 {
		keys := make([]string, 0, len(labels))
		for key := range labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		builder.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				builder.WriteByte(',')
			}
			builder.WriteString(key)
			builder.WriteString("=\"")
			builder.WriteString(promLabelValue(labels[key]))
			builder.WriteByte('"')
		}
		builder.WriteByte('}')
	}
	builder.WriteByte(' ')
	builder.WriteString(promNumber(value))
	builder.WriteByte('\n')
}

func promNumber(value any) string {
	switch typed := value.(type) {
	case nil:
		return "0"
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case uint:
		return strconv.FormatUint(uint64(typed), 10)
	case uint64:
		return strconv.FormatUint(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.Itoa(boolToNumber(typed))
	case decimal.Decimal:
		return typed.String()
	case json.Number:
		return typed.String()
	default:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64)
		if err != nil {
			return "0"
		}
		return strconv.FormatFloat(parsed, 'f', -1, 64)
	}
}

func promLabelValue(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"")
	return replacer.Replace(value)
}

func promKey(value string) string {
	var b strings.Builder
	for i, r := range value {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func boolToNumber(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s *Server) opsBackups(w http.ResponseWriter, r *http.Request) {
	report, err := s.opsBackupService().Report(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "ops-backups-load-failed", "Ops backups load failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("ops-backup-reports", "current", report))
}

func (s *Server) createOpsBackup(w http.ResponseWriter, r *http.Request) {
	result, err := s.opsBackupService().CreateArchive(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "ops-backup-create-failed", "Ops backup create failed", err.Error(), "")
		return
	}
	name, _ := result["backupName"].(string)
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("ops-backup-runs", name, result))
}

func (s *Server) restoreOpsBackupDryRun(w http.ResponseWriter, r *http.Request, backupName string) {
	result, err := s.opsBackupService().RestoreDryRun(r.Context(), backupName)
	if os.IsNotExist(err) {
		writeJSONAPIError(w, http.StatusNotFound, "ops-backup-not-found", "Ops backup not found", err.Error(), "")
		return
	}
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "ops-backup-restore-dry-run-failed", "Ops backup restore dry-run failed", err.Error(), "")
		return
	}
	name, _ := result["backupName"].(string)
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("ops-backup-restore-dry-runs", name, result))
}

func (s *Server) opsBackupService() appops.BackupService {
	return appops.NewBackupService(appops.BackupSettings{
		AppName:                    s.settings.AppName,
		AppEnv:                     s.settings.AppEnv,
		DatabaseURL:                s.settings.DatabaseURL,
		LogDir:                     s.settings.LogDir,
		RuntimeEnvFile:             s.settings.RuntimeEnvFile,
		BackupArchiveProvider:      s.settings.BackupArchiveProvider,
		BackupArchiveS3Bucket:      s.settings.BackupArchiveS3Bucket,
		BackupArchiveS3Region:      s.settings.BackupArchiveS3Region,
		BackupArchiveS3Endpoint:    s.settings.BackupArchiveS3Endpoint,
		BackupArchiveS3AccessKeyID: s.settings.BackupArchiveS3AccessKeyID,
		BackupArchiveS3SecretKey:   s.settings.BackupArchiveS3SecretKey,
		BackupArchiveS3Prefix:      s.settings.BackupArchiveS3Prefix,
		BackupArchiveOSSBucket:     s.settings.BackupArchiveOSSBucket,
		BackupArchiveOSSRegion:     s.settings.BackupArchiveOSSRegion,
		BackupArchiveOSSEndpoint:   s.settings.BackupArchiveOSSEndpoint,
		BackupArchiveOSSAccessKey:  s.settings.BackupArchiveOSSAccessKey,
		BackupArchiveOSSSecretKey:  s.settings.BackupArchiveOSSSecretKey,
		BackupArchiveOSSPrefix:     s.settings.BackupArchiveOSSPrefix,
		BackupRetentionCopies:      s.settings.BackupCopies,
		BackupRetentionDays:        s.settings.BackupDays,
		BackupRestoreDrillInterval: s.settings.BackupRestoreDrillInterval,
	}, func(ctx context.Context) (appops.DatabaseSummary, error) {
		dashboard, err := s.dashboardUsecase.Load(ctx)
		if err != nil {
			return appops.DatabaseSummary{}, err
		}
		summary := mapValue(dashboard, "summary")
		return appops.DatabaseSummary{Backend: summary["databaseBackend"], Target: summary["databaseTarget"]}, nil
	}).WithArchiveStore(s.backupStore)
}

func (s *Server) buildBackupReport(ctx context.Context) (map[string]any, error) {
	dashboard, err := s.dashboardUsecase.Load(ctx)
	if err != nil {
		return nil, err
	}
	summary := mapValue(dashboard, "summary")
	backupDir := s.backupDir()
	backups, latest, err := listBackupArchives(backupDir)
	if err != nil {
		return nil, err
	}
	status := "manual"
	if latest != nil {
		status = "ready"
	}
	restoreDrill := backupRestoreDrillStatus(latest, backupDir)
	copies, days := s.backupRetentionConfig()
	return map[string]any{
		"generatedAt":           time.Now(),
		"status":                status,
		"databaseBackend":       summary["databaseBackend"],
		"databaseTarget":        summary["databaseTarget"],
		"backupDir":             backupDir,
		"latestBackup":          latest,
		"backups":               backups,
		"retentionPolicy":       "automatic_rotation",
		"retentionPolicyDetail": backupRetentionPolicy(backups, copies, days),
		"retentionLastRun":      nullableMap(latestBackupRetentionRun(backupDir)),
		"restoreDrill":          restoreDrill["status"],
		"restoreDrillDetail":    restoreDrill,
		"supportedActions": []string{
			"database_snapshot",
			"runtime_env_export",
			"logs_archive",
			"restore_dry_run",
		},
	}, nil
}

func (s *Server) createBackupArchive(ctx context.Context) (map[string]any, error) {
	report, err := s.buildBackupReport(ctx)
	if err != nil {
		return nil, err
	}
	backupDir := s.backupDir()
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return nil, err
	}
	now := time.Now()
	name := "tradingcopilot-backup-" + now.Format("20060102-150405") + ".zip"
	path := filepath.Join(backupDir, name)
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	zipWriter := zip.NewWriter(file)
	included := []map[string]any{}
	notes := []string{}
	addFile := func(zipName string, sourcePath string) {
		info, err := addZipFile(zipWriter, zipName, sourcePath)
		if err != nil {
			notes = append(notes, zipName+": "+err.Error())
			return
		}
		included = append(included, info)
	}

	if envFile := strings.TrimSpace(s.settings.RuntimeEnvFile); envFile != "" {
		addFile("runtime/"+filepath.Base(envFile), envFile)
	}
	if dbPath, ok := sqliteDatabasePath(s.settings.DatabaseURL); ok {
		addFile("database/"+filepath.Base(dbPath), dbPath)
		for _, suffix := range []string{"-wal", "-shm"} {
			if _, err := os.Stat(dbPath + suffix); err == nil {
				addFile("database/"+filepath.Base(dbPath+suffix), dbPath+suffix)
			}
		}
	} else {
		notes = append(notes, "database snapshot requires external database backup tooling for this backend")
	}
	for _, logPath := range listLogFilesForBackup(s.settings.LogDir) {
		addFile("logs/"+filepath.Base(logPath), logPath)
	}

	manifest := map[string]any{
		"generatedAt":       now,
		"appName":           s.settings.AppName,
		"appEnv":            s.settings.AppEnv,
		"databaseBackend":   report["databaseBackend"],
		"databaseTarget":    report["databaseTarget"],
		"runtimeEnvFile":    s.settings.RuntimeEnvFile,
		"logDir":            s.settings.LogDir,
		"included":          included,
		"notes":             notes,
		"restoreStatus":     "manual_restore_required",
		"manifestVersion":   1,
		"requestAuditScope": "archive path and metadata only",
	}
	if err := addZipJSON(zipWriter, "manifest.json", manifest); err != nil {
		_ = zipWriter.Close()
		_ = file.Close()
		return nil, err
	}
	if err := zipWriter.Close(); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	backups, _, err := listBackupArchives(backupDir)
	if err != nil {
		notes = append(notes, "retention list failed: "+err.Error())
	}
	retentionRun := map[string]any{}
	if err == nil {
		copies, days := s.backupRetentionConfig()
		retentionRun = pruneBackupArtifacts(backupDir, backups, copies, days, now)
		if err := saveBackupRetentionRun(backupDir, retentionRun); err != nil {
			notes = append(notes, "retention record failed: "+err.Error())
		}
	}
	return map[string]any{
		"status":          "created",
		"backupName":      name,
		"backupPath":      path,
		"backupDir":       backupDir,
		"sizeBytes":       stat.Size(),
		"createdAt":       now,
		"included":        included,
		"notes":           notes,
		"databaseBackend": report["databaseBackend"],
		"databaseTarget":  report["databaseTarget"],
		"retentionRun":    nullableMap(retentionRun),
	}, nil
}

func (s *Server) inspectBackupArchive(ctx context.Context, backupName string) (map[string]any, error) {
	name := filepath.Base(strings.TrimSpace(backupName))
	if name == "." || name == "" || name != strings.TrimSpace(backupName) || !strings.HasSuffix(strings.ToLower(name), ".zip") {
		return nil, os.ErrInvalid
	}
	path := filepath.Join(s.backupDir(), name)
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	entries := []map[string]any{}
	notes := []string{}
	manifest := map[string]any{}
	databaseEntries := 0
	logEntries := 0
	sandboxChecks := []map[string]any{}
	checkedAt := time.Now()
	sandboxDir := restoreDrillSandboxDir(s.backupDir(), name, checkedAt)
	if err := os.MkdirAll(sandboxDir, 0o700); err != nil {
		return nil, err
	}
	sandboxFileCount := 0
	var sandboxSizeBytes int64
	sandboxDatabaseFiles := []string{}
	sandboxLogFiles := []string{}
	sandboxRuntimeFiles := []string{}
	sandboxStatus := "extracted"
	for _, file := range reader.File {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry := map[string]any{"name": file.Name, "compressedSizeBytes": file.CompressedSize64, "sizeBytes": file.UncompressedSize64}
		entries = append(entries, entry)
		switch {
		case file.Name == "manifest.json":
			body, err := readZipFile(file)
			if err != nil {
				notes = append(notes, "manifest.json: "+err.Error())
				continue
			}
			if err := json.Unmarshal(body, &manifest); err != nil {
				notes = append(notes, "manifest.json: "+err.Error())
			}
		case strings.HasPrefix(file.Name, "database/"):
			databaseEntries++
		case strings.HasPrefix(file.Name, "logs/"):
			logEntries++
		}
		extractedPath, ok := safeZipExtractPath(sandboxDir, file.Name)
		if !ok {
			sandboxStatus = "warning"
			notes = append(notes, "skipped unsafe archive entry: "+file.Name)
			continue
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(extractedPath, 0o700); err != nil {
				sandboxStatus = "warning"
				notes = append(notes, file.Name+": sandbox directory create failed: "+err.Error())
			}
			continue
		}
		written, err := extractZipFile(file, extractedPath)
		if err != nil {
			sandboxStatus = "warning"
			notes = append(notes, file.Name+": sandbox extract failed: "+err.Error())
			continue
		}
		sandboxFileCount++
		sandboxSizeBytes += written
		switch {
		case strings.HasPrefix(file.Name, "database/"):
			sandboxDatabaseFiles = append(sandboxDatabaseFiles, file.Name)
		case strings.HasPrefix(file.Name, "logs/"):
			sandboxLogFiles = append(sandboxLogFiles, file.Name)
		case file.Name == "manifest.json" || strings.HasPrefix(file.Name, "runtime/"):
			sandboxRuntimeFiles = append(sandboxRuntimeFiles, file.Name)
		}
	}
	if len(manifest) == 0 {
		notes = append(notes, "manifest.json is missing or unreadable")
	}
	if databaseEntries == 0 {
		notes = append(notes, "no embedded database snapshot; restore requires external database backup tooling")
	}
	sandboxChecks = append(sandboxChecks,
		map[string]any{"name": "manifest", "status": checkStatus(len(manifest) > 0), "detail": "manifest.json parsed from archive"},
		map[string]any{"name": "database_snapshot", "status": checkStatus(databaseEntries > 0), "detail": fmt.Sprintf("%d database archive entries", databaseEntries)},
		map[string]any{"name": "sandbox_extraction", "status": checkStatus(sandboxStatus == "extracted"), "detail": fmt.Sprintf("%d files extracted to sandbox", sandboxFileCount)},
	)
	status := "valid"
	if len(notes) > 0 {
		status = "warning"
	}
	restorePlan := []string{
		"Verify manifest metadata and archive checksums.",
		"Restore database artifacts with environment-specific tooling.",
		"Restore runtime environment overrides and log archives only after operator review.",
		"Restart TradingCopilot and run setup readiness plus smoke tests.",
	}
	return map[string]any{
		"status":               status,
		"backupName":           name,
		"backupPath":           path,
		"checkedAt":            checkedAt,
		"manifest":             manifest,
		"manifestVersion":      manifest["manifestVersion"],
		"entryCount":           len(entries),
		"databaseEntryCount":   databaseEntries,
		"logEntryCount":        logEntries,
		"entries":              entries,
		"notes":                notes,
		"sandboxStatus":        sandboxStatus,
		"sandboxDir":           sandboxDir,
		"sandboxFileCount":     sandboxFileCount,
		"sandboxSizeBytes":     sandboxSizeBytes,
		"sandboxDatabaseFiles": sandboxDatabaseFiles,
		"sandboxLogFiles":      sandboxLogFiles,
		"sandboxRuntimeFiles":  sandboxRuntimeFiles,
		"sandboxChecks":        sandboxChecks,
		"restorePlan":          restorePlan,
		"destructive":          false,
		"operatorAction":       "manual_restore_required",
		"retentionPolicyHint":  "Keep at least the latest 7 local archives or 30 days, whichever retains more recovery points.",
	}, nil
}

func (s *Server) backupDir() string {
	if dbPath, ok := sqliteDatabasePath(s.settings.DatabaseURL); ok {
		return filepath.Join(filepath.Dir(dbPath), "backups")
	}
	if strings.TrimSpace(s.settings.LogDir) != "" {
		return filepath.Join(s.settings.LogDir, "backups")
	}
	return "backups"
}

func sqliteDatabasePath(databaseURL string) (string, bool) {
	value := strings.TrimSpace(databaseURL)
	switch {
	case strings.HasPrefix(value, "sqlite+aiosqlite:///"):
		value = strings.TrimPrefix(value, "sqlite+aiosqlite:///")
	case strings.HasPrefix(value, "sqlite:///"):
		value = strings.TrimPrefix(value, "sqlite:///")
	default:
		if strings.Contains(value, "://") {
			return "", false
		}
	}
	if value == "" || value == ":memory:" {
		return "", false
	}
	return filepath.Clean(value), true
}

func listLogFilesForBackup(logDir string) []string {
	logDir = strings.TrimSpace(logDir)
	if logDir == "" {
		return nil
	}
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return nil
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, ".log") || strings.HasSuffix(name, ".jsonl") || strings.HasSuffix(name, ".gz") {
			paths = append(paths, filepath.Join(logDir, name))
		}
	}
	sort.Strings(paths)
	return paths
}

func listBackupArchives(dir string) ([]map[string]any, map[string]any, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []map[string]any{}, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	backups := []map[string]any{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".zip") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		backups = append(backups, map[string]any{
			"name":      entry.Name(),
			"path":      filepath.Join(dir, entry.Name()),
			"sizeBytes": info.Size(),
			"createdAt": info.ModTime(),
		})
	}
	sort.Slice(backups, func(i, j int) bool {
		return backups[i]["createdAt"].(time.Time).After(backups[j]["createdAt"].(time.Time))
	})
	var latest map[string]any
	if len(backups) > 0 {
		latest = backups[0]
	}
	return backups, latest, nil
}

func (s *Server) backupRetentionConfig() (int, int) {
	copies := s.settings.BackupCopies
	if copies <= 0 {
		copies = 7
	}
	days := s.settings.BackupDays
	if days <= 0 {
		days = 30
	}
	return copies, days
}

func backupRetentionPolicy(backups []map[string]any, copies int, days int) map[string]any {
	if copies <= 0 {
		copies = 7
	}
	if days <= 0 {
		days = 30
	}
	prunable := 0
	cutoff := time.Now().AddDate(0, 0, -days)
	for index, backup := range backups {
		createdAt, _ := backup["createdAt"].(time.Time)
		if index >= copies && !createdAt.IsZero() && createdAt.Before(cutoff) {
			prunable++
		}
	}
	return map[string]any{
		"mode":                "automatic_rotation",
		"recommendedCopies":   copies,
		"recommendedDays":     days,
		"currentCopies":       len(backups),
		"prunableBackupCount": prunable,
	}
}

func pruneBackupArtifacts(backupDir string, backups []map[string]any, copies int, days int, now time.Time) map[string]any {
	if copies <= 0 {
		copies = 7
	}
	if days <= 0 {
		days = 30
	}
	cutoff := now.AddDate(0, 0, -days)
	prunedBackups := []string{}
	errors := []string{}
	for index, backup := range backups {
		createdAt, _ := backup["createdAt"].(time.Time)
		if index < copies || createdAt.IsZero() || !createdAt.Before(cutoff) {
			continue
		}
		name := filepath.Base(strings.TrimSpace(fmt.Sprint(backup["name"])))
		if name == "." || name == "" || !strings.HasSuffix(strings.ToLower(name), ".zip") {
			errors = append(errors, "invalid backup archive name: "+fmt.Sprint(backup["name"]))
			continue
		}
		target := filepath.Join(backupDir, name)
		if !pathInsideRoot(backupDir, target) {
			errors = append(errors, "backup archive outside retention root: "+name)
			continue
		}
		if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
			errors = append(errors, name+": "+err.Error())
			continue
		}
		prunedBackups = append(prunedBackups, name)
	}
	prunedDrills, drillErrors := pruneRestoreDrillDirs(backupDir, cutoff)
	errors = append(errors, drillErrors...)
	status := "applied"
	if len(errors) > 0 {
		status = "warning"
	}
	return map[string]any{
		"status":                status,
		"mode":                  "automatic_rotation",
		"appliedAt":             now,
		"retentionCopies":       copies,
		"retentionDays":         days,
		"cutoffAt":              cutoff,
		"prunedBackupCount":     len(prunedBackups),
		"prunedBackups":         prunedBackups,
		"prunedSandboxDirCount": len(prunedDrills),
		"prunedSandboxDirs":     prunedDrills,
		"errors":                errors,
	}
}

func pruneRestoreDrillDirs(backupDir string, cutoff time.Time) ([]string, []string) {
	root := filepath.Join(backupDir, "restore-drills")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []string{"restore-drills: " + err.Error()}
	}
	pruned := []string{}
	errors := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			errors = append(errors, entry.Name()+": "+err.Error())
			continue
		}
		if !info.ModTime().Before(cutoff) {
			continue
		}
		target := filepath.Join(root, entry.Name())
		if !pathInsideRoot(root, target) {
			errors = append(errors, "restore drill outside retention root: "+entry.Name())
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			errors = append(errors, entry.Name()+": "+err.Error())
			continue
		}
		pruned = append(pruned, entry.Name())
	}
	return pruned, errors
}

func pathInsideRoot(root string, target string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}

func backupRestoreDrillStatus(latest map[string]any, backupDir string) map[string]any {
	status := "not_ready"
	if latest != nil {
		status = "dry_run_available"
	}
	record := latestBackupRestoreDrill(backupDir)
	if len(record) > 0 {
		recordBackup := strings.TrimSpace(fmt.Sprint(record["backupName"]))
		latestBackup := ""
		if latest != nil {
			latestBackup = strings.TrimSpace(fmt.Sprint(latest["name"]))
		}
		switch {
		case latestBackup == "" || recordBackup == latestBackup:
			if fmt.Sprint(record["status"]) == "valid" {
				status = "drilled"
			} else {
				status = "drill_warning"
			}
		default:
			status = "stale_drill"
		}
	}
	return map[string]any{
		"status":           status,
		"lastDryRun":       nullableMap(record),
		"supportedAction":  "POST /api/ops/backups/{backupName}/restore-dry-run",
		"destructive":      false,
		"requiresOperator": true,
	}
}

func saveBackupRestoreDrill(backupDir string, result map[string]any) error {
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return err
	}
	record := map[string]any{
		"backupName":         result["backupName"],
		"backupPath":         result["backupPath"],
		"status":             result["status"],
		"checkedAt":          result["checkedAt"],
		"manifestVersion":    result["manifestVersion"],
		"entryCount":         result["entryCount"],
		"databaseEntryCount": result["databaseEntryCount"],
		"logEntryCount":      result["logEntryCount"],
		"notes":              result["notes"],
		"sandboxStatus":      result["sandboxStatus"],
		"sandboxDir":         result["sandboxDir"],
		"sandboxFileCount":   result["sandboxFileCount"],
		"sandboxSizeBytes":   result["sandboxSizeBytes"],
		"sandboxChecks":      result["sandboxChecks"],
		"destructive":        false,
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(backupRestoreDrillPath(backupDir), raw, 0o600)
}

func latestBackupRestoreDrill(backupDir string) map[string]any {
	raw, err := os.ReadFile(backupRestoreDrillPath(backupDir))
	if err != nil {
		return nil
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil
	}
	return record
}

func backupRestoreDrillPath(backupDir string) string {
	return filepath.Join(backupDir, ".restore-drill.json")
}

func saveBackupRetentionRun(backupDir string, result map[string]any) error {
	if len(result) == 0 {
		return nil
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return err
	}
	record := map[string]any{
		"status":                result["status"],
		"mode":                  result["mode"],
		"appliedAt":             result["appliedAt"],
		"retentionCopies":       result["retentionCopies"],
		"retentionDays":         result["retentionDays"],
		"cutoffAt":              result["cutoffAt"],
		"prunedBackupCount":     result["prunedBackupCount"],
		"prunedBackups":         result["prunedBackups"],
		"prunedSandboxDirCount": result["prunedSandboxDirCount"],
		"prunedSandboxDirs":     result["prunedSandboxDirs"],
		"errors":                result["errors"],
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(backupRetentionRunPath(backupDir), raw, 0o600)
}

func latestBackupRetentionRun(backupDir string) map[string]any {
	raw, err := os.ReadFile(backupRetentionRunPath(backupDir))
	if err != nil {
		return nil
	}
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil
	}
	return record
}

func backupRetentionRunPath(backupDir string) string {
	return filepath.Join(backupDir, ".retention-run.json")
}

func restoreDrillSandboxDir(backupDir string, backupName string, checkedAt time.Time) string {
	base := strings.TrimSuffix(filepath.Base(backupName), filepath.Ext(backupName))
	base = safeFileToken(base)
	return filepath.Join(backupDir, "restore-drills", base+"-"+checkedAt.UTC().Format("20060102-150405.000000000"))
}

func safeFileToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "backup"
	}
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('-')
		}
	}
	token := strings.Trim(builder.String(), "-")
	if token == "" {
		return "backup"
	}
	return token
}

func safeZipExtractPath(root string, entryName string) (string, bool) {
	if strings.TrimSpace(entryName) == "" || strings.ContainsRune(entryName, '\x00') {
		return "", false
	}
	normalized := strings.ReplaceAll(entryName, "\\", "/")
	if strings.HasPrefix(normalized, "/") || strings.Contains(normalized, ":") {
		return "", false
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == "." || part == ".." {
			return "", false
		}
	}
	cleaned := path.Clean(normalized)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	targetAbs, err := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(cleaned)))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return targetAbs, true
}

func extractZipFile(file *zip.File, targetPath string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return 0, err
	}
	reader, err := file.Open()
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	writer, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(writer, reader)
	closeErr := writer.Close()
	if copyErr != nil {
		return written, copyErr
	}
	return written, closeErr
}

func checkStatus(ok bool) string {
	if ok {
		return "pass"
	}
	return "warning"
}

func nullableMap(value map[string]any) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func addZipFile(writer *zip.Writer, zipName string, sourcePath string) (map[string]any, error) {
	file, err := os.Open(sourcePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		return nil, os.ErrInvalid
	}
	header, err := zip.FileInfoHeader(stat)
	if err != nil {
		return nil, err
	}
	header.Name = filepath.ToSlash(zipName)
	header.Method = zip.Deflate
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(entry, file); err != nil {
		return nil, err
	}
	return map[string]any{"name": header.Name, "sourcePath": sourcePath, "sizeBytes": stat.Size()}, nil
}

func addZipJSON(writer *zip.Writer, zipName string, payload any) error {
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	header := &zip.FileHeader{Name: filepath.ToSlash(zipName), Method: zip.Deflate}
	header.SetModTime(time.Now())
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = entry.Write(raw)
	return err
}

func readZipFile(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
