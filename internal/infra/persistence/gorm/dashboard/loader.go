package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	appsystem "github.com/TradingCopilotDevs/TradingCopilot/internal/app/system"
	domainmsg "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/messaging"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/connect"
	marketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/marketdata"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	infrapaper "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/paper"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	gormuow "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/uow"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type Loader struct {
	db       *gorm.DB
	settings config.Settings
}

func NewLoader(db *gorm.DB, settings config.Settings) Loader {
	return Loader{db: db, settings: settings}
}

func (u Loader) Load(context.Context) (map[string]any, error) {
	overview, _ := infrapaper.Overview(u.db)
	var meetingsTotal, running, failed int64
	since24h := time.Now().Add(-24 * time.Hour)
	u.db.Model(&persistmodel.Meeting{}).Count(&meetingsTotal)
	u.db.Model(&persistmodel.Meeting{}).Where("status = ?", domainkernel.MeetingRunning).Count(&running)
	u.db.Model(&persistmodel.Meeting{}).Where("status = ? AND created_at >= ?", domainkernel.MeetingFailed, since24h).Count(&failed)
	workerStatus := heartbeatStatus(u.db, "worker", "Worker", u.settings.MeetingDispatchMode, u.settings.MeetingDispatchMode != "local")
	schedulerStatus := heartbeatStatus(u.db, "scheduler", "Scheduler", u.settings.MeetingDispatchMode, u.settings.MeetingDispatchMode != "local")
	redisStatus := redisDependencyStatus(u.settings)
	subscriptionStatus := u.messageSubscriptionListenerStatus()
	platformAdapterStatus := u.platformAdapterStatus()
	paperStatus := paperEngineStatus(u.settings.MeetingDispatchMode, workerStatus, schedulerStatus)
	databaseInfo := database.DescribeConnection(u.settings.DatabaseURL, u.db)
	databaseStatus := u.databaseStatus(databaseInfo)
	aiUsage := aiUsageDiagnostics(u.db, since24h, u.settings)
	meetingRuntime := meetingRuntimeDiagnostics(u.db, since24h)
	wakeOverdueCount := countWhere(u.db, &persistmodel.WakePlan{}, "status = ? AND next_check_at IS NOT NULL AND next_check_at < ?", domainkernel.WakeActive, time.Now())
	newsFilterReady := newsFilterReady(u.db)
	activeAccountCount := int64FromDashboardValue(overview["active_account_count"])
	payload := map[string]any{
		"summary": map[string]any{"app_name": u.settings.AppName, "app_env": u.settings.AppEnv, "deployment_mode_label": "Go single binary", "meeting_dispatch_mode": u.settings.MeetingDispatchMode, "database_backend": databaseInfo.Backend, "database_target": databaseInfo.Target, "paper_execution_mode": paperMode(u.settings.MeetingDispatchMode)},
		"system_status": map[string]any{
			"database":                      databaseStatus,
			"redis":                         redisStatus,
			"worker":                        workerStatus,
			"scheduler":                     schedulerStatus,
			"message_subscription_listener": subscriptionStatus,
			"platform_adapter":              platformAdapterStatus,
			"paper_engine":                  paperStatus,
		},
		"business_metrics": map[string]any{
			"meetings_total": meetingsTotal, "meetings_running": running, "meetings_failed_24h": failed,
			"message_subscription_enabled_count": countWhere(u.db, &persistmodel.MessageSubscription{}, "enabled = ?", true),
			"ingested_messages_24h":              countWhere(u.db, &persistmodel.IngestedMessage{}, "message_time >= ?", since24h),
			"ingested_unfiltered_count":          countWhere(u.db, &persistmodel.IngestedMessage{}, "filter_status = ? OR (filter_status = '' AND filter_decision IS NULL)", "unfiltered"),
			"platform_adapter_enabled_count":     countWhere(u.db, &persistmodel.PlatformAdapter{}, "enabled = ?", true),
			"paper_account_count":                overview["account_count"], "paper_active_account_count": overview["active_account_count"], "paper_total_equity": overview["total_equity"], "paper_pending_order_count": overview["pending_order_count"],
			"wake_active_count": countWhere(u.db, &persistmodel.WakePlan{}, "status = ?", domainkernel.WakeActive), "wake_overdue_count": wakeOverdueCount,
			"ai_enabled_provider_count": countWhere(u.db, &persistmodel.AiProvider{}, "enabled = ?", true), "ai_ready_provider_count": countWhere(u.db, &persistmodel.AiProvider{}, "enabled = ? AND api_key_secret_id IS NOT NULL", true), "news_filter_ready": newsFilterReady,
			"ai_model_calls_24h": aiUsage["model_calls_24h"], "ai_prompt_tokens_24h": aiUsage["prompt_tokens_24h"], "ai_completion_tokens_24h": aiUsage["completion_tokens_24h"], "ai_total_tokens_24h": aiUsage["total_tokens_24h"], "ai_total_tokens_total": aiUsage["total_tokens_total"], "ai_cost_amount_24h": aiUsage["cost_amount_24h"], "ai_cost_budget_status": aiUsage["cost_budget_status"],
			"meeting_run_avg_seconds_24h": meetingRuntime["run_avg_seconds"], "meeting_run_p95_seconds_24h": meetingRuntime["run_p95_seconds"], "meeting_queue_wait_avg_seconds_24h": meetingRuntime["queue_wait_avg_seconds"],
			"market_symbol_count": countWhere(u.db, &persistmodel.MarketSymbol{}, ""), "market_watchlist_count": countWhere(u.db, &persistmodel.WatchlistItem{}, ""), "market_active_watchlist_count": countWhere(u.db, &persistmodel.WatchlistItem{}, "active = ?", true), "paper_is_trading_time": infrapaper.IsTradingTime(time.Now()),
		},
		"recent_activity":        map[string]any{"recent_meetings": recentMeetings(u.db), "recent_ingested_messages": recentIngestedMessages(u.db)},
		"dependency_diagnostics": dashboardDependencyDiagnostics(u.db, u.settings, redisStatus, workerStatus, schedulerStatus, subscriptionStatus, platformAdapterStatus, paperStatus, newsFilterReady, aiUsage, meetingRuntime),
		"alerts":                 dashboardAlerts(redisStatus, workerStatus, schedulerStatus, subscriptionStatus, platformAdapterStatus, paperStatus, wakeOverdueCount, newsFilterReady, activeAccountCount),
	}
	return camelizeJSONKeys(payload).(map[string]any), nil
}

func statusItem(key, title, status, summary, detail string) map[string]any {
	return map[string]any{"key": key, "title": title, "status": status, "summary": summary, "detail": detail, "checked_at": time.Now()}
}

func (u Loader) databaseStatus(info database.ConnectionInfo) map[string]any {
	sqlDB, err := u.db.DB()
	if err != nil {
		return statusItem("database", "Database", "error", info.Backend, err.Error())
	}
	if err := sqlDB.Ping(); err != nil {
		return statusItem("database", "Database", "error", info.Backend, err.Error())
	}
	return statusItem("database", "Database", "ok", info.Backend, fmt.Sprintf("Connected to %s database %s.", info.Backend, info.Target))
}

func redisDependencyStatus(settings config.Settings) map[string]any {
	if settings.MeetingDispatchMode == "local" {
		return statusItem("redis", "Redis", "disabled", "local mode", "Local dispatch mode does not require Redis.")
	}
	opt, err := redis.ParseURL(settings.RedisURL)
	if err != nil {
		return statusItem("redis", "Redis", "error", "invalid Redis URL", err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()
	client := redis.NewClient(opt)
	defer client.Close()
	if err := client.Ping(ctx).Err(); err != nil {
		return statusItem("redis", "Redis", "error", "ping failed", err.Error())
	}
	return statusItem("redis", "Redis", "ok", "ping ok", "Redis queue dependency responded to PING.")
}

func (u Loader) platformAdapterStatus() map[string]any {
	enabled := countWhere(u.db, &persistmodel.PlatformAdapter{}, "enabled = ?", true)
	if enabled == 0 {
		return statusItem("platform_adapter", "Platform Adapter", "disabled", "not configured", "Configure a platform adapter to enable outbound notifications.")
	}
	return statusItem("platform_adapter", "Platform Adapter", "ok", "configured", fmt.Sprintf("%d enabled platform adapter(s).", enabled))
}

func (u Loader) messageSubscriptionListenerStatus() map[string]any {
	hasAppID := hasUsableSecret(u.db, u.settings, domainkernel.SecretKindMessageSubscription, "telegram:app_id")
	hasAppHash := hasUsableSecret(u.db, u.settings, domainkernel.SecretKindMessageSubscription, "telegram:app_hash")
	hasSession := hasUsableSecret(u.db, u.settings, domainkernel.SecretKindMessageSubscription, "telegram:mtproto_session")
	if !hasAppID && !hasAppHash && !hasSession {
		return statusItem("message_subscription_listener", "Message Subscription Listener", "disabled", "not configured", "Configure Telegram App ID, App Hash, and complete MTProto login to enable live subscription listening.")
	}
	if !hasAppID || !hasAppHash || !hasSession {
		return statusItem("message_subscription_listener", "Message Subscription Listener", "warning", "partial config", "Live subscription listening requires App ID, App Hash, and a stored MTProto session.")
	}
	if countWhere(u.db, &persistmodel.MessageSubscription{}, "enabled = ?", true) == 0 {
		return statusItem("message_subscription_listener", "Message Subscription Listener", "warning", "no enabled subscriptions", "MTProto is configured, but no message subscriptions are enabled.")
	}
	return heartbeatStatus(u.db, "message_subscription_listener", "Message Subscription Listener", u.settings.MeetingDispatchMode, true)
}

func heartbeatStatus(db *gorm.DB, serviceName string, title string, mode string, enabled bool) map[string]any {
	if !enabled {
		return statusItem(serviceName, title, "disabled", "not enabled", "Current local mode does not require this background process.")
	}
	systemUsecase := appsystem.NewUsecase(gormrepo.NewSystemRepository(db), gormuow.NewSystemUnitOfWork(db))
	deleted, err := systemUsecase.DeleteInvalidHeartbeat(context.Background(), serviceName)
	if err != nil {
		return statusItem(serviceName, title, "error", "heartbeat cleanup failed", err.Error())
	}
	if deleted {
		return statusItem(serviceName, title, "error", "no heartbeat", "Invalid heartbeat payload was deleted. Waiting for this process to record a new heartbeat.")
	}
	heartbeat, err := systemUsecase.LoadHeartbeat(context.Background(), serviceName)
	if err != nil {
		return statusItem(serviceName, title, "error", "no heartbeat", "No heartbeat has been recorded by this process.")
	}
	age := appsystem.HeartbeatAgeSeconds(heartbeat, time.Now())
	if age == nil {
		if _, err := systemUsecase.DeleteInvalidHeartbeat(context.Background(), serviceName); err != nil {
			return statusItem(serviceName, title, "error", "heartbeat cleanup failed", err.Error())
		}
		return statusItem(serviceName, title, "error", "no heartbeat", "Invalid heartbeat payload was deleted. Waiting for this process to record a new heartbeat.")
	}
	if heartbeat.Status == "stopped" {
		return statusItem(serviceName, title, "error", "stopped", fmt.Sprintf("Last heartbeat reported stopped about %d seconds ago.", *age))
	}
	if *age > appsystem.HeartbeatStaleAfterSeconds {
		return statusItem(serviceName, title, "error", "stale heartbeat", fmt.Sprintf("Last heartbeat was about %d seconds ago, exceeding %d seconds.", *age, appsystem.HeartbeatStaleAfterSeconds))
	}
	detail := fmt.Sprintf("Last heartbeat was about %d seconds ago, mode %s.", *age, firstNonEmptyString(heartbeat.Mode, mode))
	if heartbeat.Error != "" {
		detail += " Last error: " + heartbeat.Error
		return statusItem(serviceName, title, "warning", "running with errors", detail)
	}
	return statusItem(serviceName, title, "ok", "running", detail)
}

func paperEngineStatus(mode string, workerStatus map[string]any, schedulerStatus map[string]any) map[string]any {
	if mode == "local" {
		return statusItem("paper_engine", "Paper Engine", "ok", "local loop", "Paper maintenance runs in the application process in local mode.")
	}
	if workerStatus["status"] == "ok" && schedulerStatus["status"] == "ok" {
		return statusItem("paper_engine", "Paper Engine", "ok", "queue mode", "Worker and scheduler heartbeats are healthy.")
	}
	return statusItem("paper_engine", "Paper Engine", "error", "queue mode unhealthy", "Paper maintenance requires healthy worker and scheduler heartbeats in queue mode.")
}

func paperMode(mode string) string {
	if mode == "local" {
		return "local_loop"
	}
	return "redis_queue"
}

func meetingRuntimeDiagnostics(db *gorm.DB, since time.Time) map[string]any {
	var meetings []persistmodel.Meeting
	db.Where("created_at >= ? OR started_at >= ? OR completed_at >= ?", since, since, since).Find(&meetings)
	queueWaits := []float64{}
	runDurations := []float64{}
	totalDurations := []float64{}
	completed := int64(0)
	failed := int64(0)
	running := int64(0)
	for _, meeting := range meetings {
		switch meeting.Status {
		case domainkernel.MeetingCompleted:
			completed++
		case domainkernel.MeetingFailed:
			failed++
		case domainkernel.MeetingRunning:
			running++
		}
		if meeting.StartedAt != nil {
			queueWaits = append(queueWaits, durationSeconds(meeting.StartedAt.Sub(meeting.CreatedAt)))
		}
		if meeting.StartedAt != nil && meeting.CompletedAt != nil {
			runDurations = append(runDurations, durationSeconds(meeting.CompletedAt.Sub(*meeting.StartedAt)))
		}
		if meeting.CompletedAt != nil {
			totalDurations = append(totalDurations, durationSeconds(meeting.CompletedAt.Sub(meeting.CreatedAt)))
		}
	}
	eventCounts := meetingEventCounts(db, since)
	status := "ok"
	summary := fmt.Sprintf("%d completed, %d failed in the last 24h", completed, failed)
	if failed > 0 {
		status = "warning"
	}
	if len(meetings) == 0 {
		status = "empty"
		summary = "no meeting activity in the last 24h"
	}
	return map[string]any{
		"status":                 status,
		"summary":                summary,
		"meeting_count_24h":      int64(len(meetings)),
		"completed_count_24h":    completed,
		"failed_count_24h":       failed,
		"running_count":          running,
		"queue_wait_avg_seconds": roundSeconds(avgFloat(queueWaits)),
		"queue_wait_p95_seconds": roundSeconds(p95Float(queueWaits)),
		"run_avg_seconds":        roundSeconds(avgFloat(runDurations)),
		"run_p95_seconds":        roundSeconds(p95Float(runDurations)),
		"total_avg_seconds":      roundSeconds(avgFloat(totalDurations)),
		"total_p95_seconds":      roundSeconds(p95Float(totalDurations)),
		"event_counts_24h":       eventCounts,
		"window_seconds":         int64(24 * 60 * 60),
		"generated_at":           time.Now(),
	}
}

func meetingEventCounts(db *gorm.DB, since time.Time) []map[string]any {
	type row struct {
		Type  string
		Count int64
	}
	var rows []row
	db.Model(&persistmodel.MeetingEvent{}).
		Select("type, count(*) as count").
		Where("created_at >= ?", since).
		Group("type").
		Order("type").
		Scan(&rows)
	out := make([]map[string]any, 0, len(rows))
	for _, item := range rows {
		out = append(out, map[string]any{"type": item.Type, "count": item.Count})
	}
	return out
}

func durationSeconds(duration time.Duration) float64 {
	if duration < 0 {
		return 0
	}
	return duration.Seconds()
}

func avgFloat(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func p95Float(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copied := append([]float64(nil), values...)
	sort.Float64s(copied)
	index := int(math.Ceil(float64(len(copied))*0.95)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(copied) {
		index = len(copied) - 1
	}
	return copied[index]
}

func roundSeconds(value float64) float64 {
	return math.Round(value*100) / 100
}

func recentMeetings(db *gorm.DB) []map[string]any {
	var rows []persistmodel.Meeting
	db.Order("created_at desc").Limit(5).Find(&rows)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"id": row.ID, "topic": row.Topic, "status": row.Status, "trigger_source": row.TriggerSource,
			"summary": row.Summary, "conclusion": row.Conclusion, "tags": row.Tags,
			"recap_status": row.RecapStatus, "recap_updated_at": row.RecapUpdatedAt,
			"run_attempt": row.RunAttempt, "heartbeat_at": row.HeartbeatAt, "auto_requeue_count": row.AutoRequeueCount,
			"created_at": row.CreatedAt, "started_at": row.StartedAt, "completed_at": row.CompletedAt,
		})
	}
	return out
}

func recentIngestedMessages(db *gorm.DB) []map[string]any {
	var rows []persistmodel.IngestedMessage
	db.Order("message_time desc").Limit(5).Find(&rows)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		var subscription persistmodel.MessageSubscription
		_ = db.First(&subscription, row.SubscriptionID).Error
		out = append(out, map[string]any{
			"id": row.ID, "subscription_id": row.SubscriptionID, "subscription_title": subscription.Title, "source_ref": subscription.SourceRef,
			"message_time": row.MessageTime, "text": row.Text, "filter_decision": row.FilterDecision, "filter_status": row.FilterStatus,
		})
	}
	return out
}

func newsFilterReady(db *gorm.DB) bool {
	var filters []persistmodel.MessageSubscriptionFilter
	if err := db.Preload("Provider").Where("enabled = ?", true).Find(&filters).Error; err != nil {
		return false
	}
	for _, filter := range filters {
		if filter.Provider == nil || !filter.Provider.Enabled || filter.Provider.APIKeySecretID == nil {
			continue
		}
		if filter.Model != nil && strings.TrimSpace(*filter.Model) != "" {
			return true
		}
		if strings.TrimSpace(filter.Provider.DefaultModel) != "" {
			return true
		}
	}
	return false
}

func dashboardAlerts(redisStatus, workerStatus, schedulerStatus, subscriptionStatus, platformAdapterStatus, paperStatus map[string]any, wakeOverdueCount int64, newsFilterReady bool, activeAccountCount int64) []map[string]any {
	alerts := make([]map[string]any, 0, 6)
	add := func(level, title, detail, link string) {
		if len(alerts) >= 6 {
			return
		}
		alerts = append(alerts, map[string]any{"level": level, "title": title, "detail": detail, "link": link})
	}
	for _, item := range []struct {
		status map[string]any
		link   string
	}{{redisStatus, "/settings"}, {workerStatus, "/settings"}, {schedulerStatus, "/settings"}, {subscriptionStatus, "/message-subscriptions"}, {platformAdapterStatus, "/platform-adapters"}, {paperStatus, "/paper"}} {
		if fmt.Sprint(item.status["status"]) == "error" {
			add("error", fmt.Sprint(item.status["title"]), fmt.Sprint(item.status["detail"]), item.link)
		}
	}
	if !newsFilterReady {
		add("warning", "Message filter AI is not ready", "Message subscription filters require an enabled provider, API key, and model before automatic filtering can run.", "/message-subscriptions")
	}
	if wakeOverdueCount > 0 {
		add("warning", "Overdue wake plans", fmt.Sprintf("%d active wake plan(s) are due and have not been processed.", wakeOverdueCount), "/wake")
	}
	if activeAccountCount > 0 && fmt.Sprint(paperStatus["status"]) != "ok" {
		add("error", "Paper engine is unhealthy", "There are active paper accounts, but paper execution is not currently healthy.", "/paper")
	}
	return alerts
}

func dashboardDependencyDiagnostics(db *gorm.DB, settings config.Settings, redisStatus, workerStatus, schedulerStatus, subscriptionStatus, platformAdapterStatus, paperStatus map[string]any, newsFilterReady bool, aiUsage map[string]any, meetingRuntime map[string]any) map[string]any {
	return map[string]any{
		"redis": redisStatus,
		"background": map[string]any{
			"worker":    workerStatus,
			"scheduler": schedulerStatus,
		},
		"meeting": map[string]any{
			"runtime": meetingRuntime,
		},
		"messaging": map[string]any{
			"subscription_listener": subscriptionStatus,
			"platform_adapter":      platformAdapterStatus,
			"enabled_subscriptions": countWhere(db, &persistmodel.MessageSubscription{}, "enabled = ?", true),
			"provider":              messagingProviderDiagnostics(db, settings, subscriptionStatus, platformAdapterStatus),
		},
		"ai": map[string]any{
			"enabled_providers": countWhere(db, &persistmodel.AiProvider{}, "enabled = ?", true),
			"ready_providers":   countWhere(db, &persistmodel.AiProvider{}, "enabled = ? AND api_key_secret_id IS NOT NULL", true),
			"news_filter_ready": newsFilterReady,
			"providers":         aiProviderDiagnostics(db),
			"usage":             aiUsage,
		},
		"paper": map[string]any{
			"engine":          paperStatus,
			"active_accounts": countWhere(db, &persistmodel.PaperAccount{}, "active = ?", true),
			"pending_orders":  countWhere(db, &persistmodel.PaperOrder{}, "status = ?", domainkernel.OrderPending),
		},
		"market": map[string]any{
			"symbols":          countWhere(db, &persistmodel.MarketSymbol{}, ""),
			"watchlist_active": countWhere(db, &persistmodel.WatchlistItem{}, "active = ?", true),
			"cached_quotes":    countWhere(db, &persistmodel.RealtimeQuote{}, ""),
			"provider":         marketProviderDiagnostics(db),
		},
	}
}

func aiProviderDiagnostics(db *gorm.DB) []map[string]any {
	var providers []persistmodel.AiProvider
	db.Order("id").Find(&providers)
	out := make([]map[string]any, 0, len(providers))
	for _, provider := range providers {
		modelCount := countWhere(db, &persistmodel.AiProviderModel{}, "provider_id = ?", provider.ID)
		enabledModelCount := countWhere(db, &persistmodel.AiProviderModel{}, "provider_id = ? AND enabled = ?", provider.ID, true)
		roleCount := countWhere(db, &persistmodel.AgentRole{}, "provider_id = ?", provider.ID)
		ready := provider.Enabled && provider.APIKeySecretID != nil && strings.TrimSpace(provider.DefaultModel) != ""
		status := "ok"
		summary := "ready"
		if !provider.Enabled {
			status, summary = "disabled", "provider disabled"
		} else if provider.APIKeySecretID == nil {
			status, summary = "warning", "missing API key"
		} else if strings.TrimSpace(provider.DefaultModel) == "" {
			status, summary = "warning", "missing default model"
		}
		out = append(out, map[string]any{
			"id": provider.ID, "name": provider.Name, "status": status, "summary": summary, "enabled": provider.Enabled,
			"has_api_key": provider.APIKeySecretID != nil, "default_model": provider.DefaultModel, "ready": ready,
			"model_count": modelCount, "enabled_model_count": enabledModelCount, "role_count": roleCount,
		})
	}
	return out
}

type aiUsageAccumulator struct {
	ModelCalls         int64
	PromptTokens       int64
	CompletionTokens   int64
	TotalTokens        int64
	LatencyMsTotal     int64
	LatencyObserved    int64
	LatencySamplesMs   []float64
	CostAmount         float64
	PayloadCostCalls   int64
	EstimatedCostCalls int64
	MissingCostCalls   int64
}

type aiUsageGroup struct {
	ProviderID   uint
	ProviderName string
	Model        string
	Total        aiUsageAccumulator
	Window       aiUsageAccumulator
}

type aiTokenUsage struct {
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}

type aiCostConfig struct {
	Currency         string
	Rates            []aiCostRate
	DailyBudget      float64
	BudgetConfigured bool
	RatesConfigured  bool
}

type aiCostRate struct {
	ProviderID            uint
	ProviderName          string
	Model                 string
	InputPerMillion       float64
	OutputPerMillion      float64
	TotalPerMillion       float64
	ConfiguredSourceLabel string
}

func aiUsageDiagnostics(db *gorm.DB, since time.Time, settings config.Settings) map[string]any {
	costConfig := aiCostConfigFromSettings(db, settings)
	costCoverage := aiCostRateCoverage(db, costConfig)
	providerNames := map[uint]string{}
	var providers []persistmodel.AiProvider
	db.Select("id", "name").Find(&providers)
	for _, provider := range providers {
		providerNames[provider.ID] = provider.Name
	}
	var events []persistmodel.MeetingEvent
	db.Select("id", "payload", "created_at").Where("payload IS NOT NULL").Find(&events)
	total := aiUsageAccumulator{}
	window := aiUsageAccumulator{}
	byProviderModel := map[string]*aiUsageGroup{}
	for _, event := range events {
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil || payload == nil {
			continue
		}
		usage, ok := tokenUsageFromPayload(payload)
		if !ok {
			continue
		}
		providerID := uintFromAny(firstNonNil(payload["provider_id"], payload["providerId"]))
		model := firstNonEmptyString(stringFromAny(payload["model"]), "unknown")
		costAmount, hasCost := costAmountFromPayload(payload)
		hasEstimatedCost := false
		if !hasCost {
			costAmount, hasEstimatedCost = estimateAICost(usage, providerID, providerNames[providerID], model, costConfig)
		}
		latencyMs, hasLatency := latencyMillisFromPayload(payload)
		addAIUsage(&total, usage, costAmount, hasCost, hasEstimatedCost, latencyMs, hasLatency)
		inWindow := !event.CreatedAt.Before(since)
		if inWindow {
			addAIUsage(&window, usage, costAmount, hasCost, hasEstimatedCost, latencyMs, hasLatency)
		}
		key := fmt.Sprintf("%d:%s", providerID, model)
		group := byProviderModel[key]
		if group == nil {
			group = &aiUsageGroup{ProviderID: providerID, ProviderName: providerNames[providerID], Model: model}
			if group.ProviderName == "" {
				group.ProviderName = "unknown"
			}
			byProviderModel[key] = group
		}
		addAIUsage(&group.Total, usage, costAmount, hasCost, hasEstimatedCost, latencyMs, hasLatency)
		if inWindow {
			addAIUsage(&group.Window, usage, costAmount, hasCost, hasEstimatedCost, latencyMs, hasLatency)
		}
	}
	groups := make([]*aiUsageGroup, 0, len(byProviderModel))
	for _, group := range byProviderModel {
		groups = append(groups, group)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Window.TotalTokens != groups[j].Window.TotalTokens {
			return groups[i].Window.TotalTokens > groups[j].Window.TotalTokens
		}
		return groups[i].Total.TotalTokens > groups[j].Total.TotalTokens
	})
	rows := make([]map[string]any, 0, len(groups))
	for _, group := range groups {
		rows = append(rows, map[string]any{
			"provider_id":                  group.ProviderID,
			"provider_name":                group.ProviderName,
			"model":                        group.Model,
			"model_calls_total":            group.Total.ModelCalls,
			"model_calls_24h":              group.Window.ModelCalls,
			"prompt_tokens_total":          group.Total.PromptTokens,
			"prompt_tokens_24h":            group.Window.PromptTokens,
			"completion_tokens_total":      group.Total.CompletionTokens,
			"completion_tokens_24h":        group.Window.CompletionTokens,
			"total_tokens_total":           group.Total.TotalTokens,
			"total_tokens_24h":             group.Window.TotalTokens,
			"cost_amount_total":            nullableCost(group.Total),
			"cost_amount_24h":              nullableCost(group.Window),
			"cost_amount_source":           costSource(group.Total),
			"cost_missing_calls_total":     group.Total.MissingCostCalls,
			"cost_missing_calls_24h":       group.Window.MissingCostCalls,
			"cost_estimated_calls_total":   group.Total.EstimatedCostCalls,
			"cost_estimated_calls_24h":     group.Window.EstimatedCostCalls,
			"latency_observed_calls_total": group.Total.LatencyObserved,
			"latency_observed_calls_24h":   group.Window.LatencyObserved,
			"latency_avg_ms_total":         nullableLatencyAvg(group.Total),
			"latency_avg_ms_24h":           nullableLatencyAvg(group.Window),
			"latency_p95_ms_24h":           nullableLatencyP95(group.Window),
		})
	}
	status, summary := "empty", "no token usage captured"
	if total.ModelCalls > 0 {
		status = "observed"
		summary = fmt.Sprintf("%d model call(s), %d token(s) in the last 24h", window.ModelCalls, window.TotalTokens)
	}
	return map[string]any{
		"status":                       status,
		"summary":                      summary,
		"window_seconds":               int64(24 * time.Hour / time.Second),
		"model_calls_total":            total.ModelCalls,
		"model_calls_24h":              window.ModelCalls,
		"prompt_tokens_total":          total.PromptTokens,
		"prompt_tokens_24h":            window.PromptTokens,
		"completion_tokens_total":      total.CompletionTokens,
		"completion_tokens_24h":        window.CompletionTokens,
		"total_tokens_total":           total.TotalTokens,
		"total_tokens_24h":             window.TotalTokens,
		"cost_amount_total":            nullableCost(total),
		"cost_amount_24h":              nullableCost(window),
		"cost_amount_source":           costSource(total),
		"cost_currency":                costConfig.Currency,
		"cost_budget_status":           costBudgetStatus(window, costConfig),
		"cost_budget_amount":           nullableBudget(costConfig),
		"cost_budget_used_pct":         costBudgetUsedPct(window, costConfig),
		"cost_rates_configured":        costConfig.RatesConfigured,
		"cost_rate_coverage":           costCoverage,
		"cost_missing_calls_total":     total.MissingCostCalls,
		"cost_missing_calls_24h":       window.MissingCostCalls,
		"cost_estimated_calls_total":   total.EstimatedCostCalls,
		"cost_estimated_calls_24h":     window.EstimatedCostCalls,
		"latency_observed_calls_total": total.LatencyObserved,
		"latency_observed_calls_24h":   window.LatencyObserved,
		"latency_avg_ms_total":         nullableLatencyAvg(total),
		"latency_avg_ms_24h":           nullableLatencyAvg(window),
		"latency_p95_ms_24h":           nullableLatencyP95(window),
		"by_provider_model":            rows,
	}
}

func addAIUsage(acc *aiUsageAccumulator, usage aiTokenUsage, costAmount float64, hasPayloadCost bool, hasEstimatedCost bool, latencyMs int64, hasLatency bool) {
	acc.ModelCalls++
	acc.PromptTokens += usage.PromptTokens
	acc.CompletionTokens += usage.CompletionTokens
	acc.TotalTokens += usage.TotalTokens
	if hasLatency && latencyMs >= 0 {
		acc.LatencyObserved++
		acc.LatencyMsTotal += latencyMs
		acc.LatencySamplesMs = append(acc.LatencySamplesMs, float64(latencyMs))
	}
	switch {
	case hasPayloadCost:
		acc.CostAmount += costAmount
		acc.PayloadCostCalls++
	case hasEstimatedCost:
		acc.CostAmount += costAmount
		acc.EstimatedCostCalls++
	default:
		acc.MissingCostCalls++
	}
}

func tokenUsageFromPayload(payload map[string]any) (aiTokenUsage, bool) {
	raw := firstNonNil(payload["token_usage"], payload["tokenUsage"], payload["usage"])
	switch typed := raw.(type) {
	case map[string]any:
		return tokenUsageFromMap(typed)
	case string:
		var parsed map[string]any
		if err := json.Unmarshal([]byte(typed), &parsed); err == nil {
			return tokenUsageFromMap(parsed)
		}
	}
	return aiTokenUsage{}, false
}

func tokenUsageFromMap(payload map[string]any) (aiTokenUsage, bool) {
	usage := aiTokenUsage{
		PromptTokens:     int64FromAny(firstNonNil(payload["prompt_tokens"], payload["promptTokens"])),
		CompletionTokens: int64FromAny(firstNonNil(payload["completion_tokens"], payload["completionTokens"])),
		TotalTokens:      int64FromAny(firstNonNil(payload["total_tokens"], payload["totalTokens"])),
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 {
		return aiTokenUsage{}, false
	}
	return usage, true
}

func costAmountFromPayload(payload map[string]any) (float64, bool) {
	for _, value := range []any{
		payload["cost_amount"], payload["costAmount"], payload["estimated_cost_amount"], payload["estimatedCostAmount"], payload["cost"],
	} {
		if amount, ok := float64FromAny(value); ok {
			return amount, true
		}
	}
	return 0, false
}

func latencyMillisFromPayload(payload map[string]any) (int64, bool) {
	for _, value := range []any{
		payload["latency_ms"], payload["latencyMs"], payload["duration_ms"], payload["durationMs"],
	} {
		if value == nil {
			continue
		}
		if amount, ok := float64FromAny(value); ok && amount >= 0 {
			return int64(math.Round(amount)), true
		}
	}
	return 0, false
}

func nullableLatencyAvg(acc aiUsageAccumulator) any {
	if acc.LatencyObserved == 0 {
		return nil
	}
	return math.Round(float64(acc.LatencyMsTotal)/float64(acc.LatencyObserved)*100) / 100
}

func nullableLatencyP95(acc aiUsageAccumulator) any {
	if len(acc.LatencySamplesMs) == 0 {
		return nil
	}
	return math.Round(p95Float(acc.LatencySamplesMs)*100) / 100
}

func nullableCost(acc aiUsageAccumulator) any {
	if acc.PayloadCostCalls == 0 && acc.EstimatedCostCalls == 0 {
		return nil
	}
	return roundCost(acc.CostAmount)
}

func costSource(acc aiUsageAccumulator) string {
	hasPayload := acc.PayloadCostCalls > 0
	hasEstimated := acc.EstimatedCostCalls > 0
	switch {
	case hasPayload && hasEstimated:
		return "mixed"
	case hasPayload:
		return "provider_payload"
	case hasEstimated:
		return "configured_rates"
	default:
		return "not_configured"
	}
}

func aiCostConfigFromSettings(db *gorm.DB, settings config.Settings) aiCostConfig {
	cfg := aiCostConfig{Currency: "USD", DailyBudget: settings.AIDailyCostBudget, BudgetConfigured: settings.AIDailyCostBudget > 0}
	var rows []persistmodel.AppSetting
	db.Where("key IN ?", []string{domainsettings.SettingAICostRates, domainsettings.SettingAIDailyCostBudget}).Find(&rows)
	for _, row := range rows {
		raw := appSettingValue(row.Value)
		switch row.Key {
		case domainsettings.SettingAIDailyCostBudget:
			if budget, ok := float64FromAny(raw); ok && (budget == -1 || budget > 0) {
				cfg.DailyBudget = budget
				cfg.BudgetConfigured = budget > 0
			}
		case domainsettings.SettingAICostRates:
			applyAICostRates(&cfg, raw)
		}
	}
	return cfg
}

func applyAICostRates(cfg *aiCostConfig, raw any) {
	payload, ok := raw.(map[string]any)
	if !ok {
		return
	}
	if currency := strings.ToUpper(strings.TrimSpace(stringFromAny(payload["currency"]))); currency != "" {
		cfg.Currency = currency
	}
	rawRates, ok := payload["rates"].([]any)
	if !ok {
		return
	}
	for _, rawRate := range rawRates {
		item, ok := rawRate.(map[string]any)
		if !ok {
			continue
		}
		rate := aiCostRate{
			ProviderID:            uintFromAny(firstNonNil(item["provider_id"], item["providerId"])),
			ProviderName:          strings.ToLower(strings.TrimSpace(stringFromAny(firstNonNil(item["provider_name"], item["providerName"])))),
			Model:                 strings.ToLower(strings.TrimSpace(stringFromAny(item["model"]))),
			ConfiguredSourceLabel: "configured_rates",
		}
		rate.InputPerMillion, _ = float64FromAny(firstNonNil(item["input_per_million"], item["inputPerMillion"], item["prompt_per_million"], item["promptPerMillion"]))
		rate.OutputPerMillion, _ = float64FromAny(firstNonNil(item["output_per_million"], item["outputPerMillion"], item["completion_per_million"], item["completionPerMillion"]))
		rate.TotalPerMillion, _ = float64FromAny(firstNonNil(item["total_per_million"], item["totalPerMillion"]))
		if rate.Model == "" || rate.InputPerMillion < 0 || rate.OutputPerMillion < 0 || rate.TotalPerMillion < 0 {
			continue
		}
		if rate.InputPerMillion == 0 && rate.OutputPerMillion == 0 && rate.TotalPerMillion == 0 {
			continue
		}
		cfg.Rates = append(cfg.Rates, rate)
	}
	cfg.RatesConfigured = len(cfg.Rates) > 0
}

func aiCostRateCoverage(db *gorm.DB, cfg aiCostConfig) map[string]any {
	var providers []persistmodel.AiProvider
	db.Preload("Models", "enabled = ?", true).Where("enabled = ?", true).Find(&providers)
	type coverageTarget struct {
		ProviderID   uint
		ProviderName string
		Model        string
		Source       string
	}
	targets := []coverageTarget{}
	seen := map[string]struct{}{}
	addTarget := func(providerID uint, providerName string, model string, source string) {
		model = strings.TrimSpace(model)
		if model == "" {
			return
		}
		key := fmt.Sprintf("%d:%s", providerID, strings.ToLower(model))
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, coverageTarget{ProviderID: providerID, ProviderName: providerName, Model: model, Source: source})
	}
	for _, provider := range providers {
		for _, model := range provider.Models {
			addTarget(provider.ID, provider.Name, model.ModelID, "synced_model")
		}
		addTarget(provider.ID, provider.Name, provider.DefaultModel, "default_model")
	}
	missing := []map[string]any{}
	covered := 0
	for _, target := range targets {
		score := -1
		providerName := strings.ToLower(strings.TrimSpace(target.ProviderName))
		modelName := strings.ToLower(strings.TrimSpace(target.Model))
		for _, rate := range cfg.Rates {
			if candidate := aiCostRateMatchScore(rate, target.ProviderID, providerName, modelName); candidate > score {
				score = candidate
			}
		}
		if score >= 0 {
			covered++
			continue
		}
		missing = append(missing, map[string]any{
			"provider_id":   target.ProviderID,
			"provider_name": target.ProviderName,
			"model":         target.Model,
			"source":        target.Source,
		})
	}
	status := "empty"
	if len(targets) > 0 {
		status = "ok"
		if len(missing) > 0 {
			status = "warning"
		}
	}
	coveragePct := any(nil)
	if len(targets) > 0 {
		coveragePct = math.Round(float64(covered)/float64(len(targets))*10000) / 100
	}
	return map[string]any{
		"status":              status,
		"currency":            cfg.Currency,
		"rates_configured":    cfg.RatesConfigured,
		"model_count":         len(targets),
		"covered_model_count": covered,
		"missing_model_count": len(missing),
		"coverage_pct":        coveragePct,
		"missing_models":      missing,
	}
}

func estimateAICost(usage aiTokenUsage, providerID uint, providerName string, model string, cfg aiCostConfig) (float64, bool) {
	if len(cfg.Rates) == 0 {
		return 0, false
	}
	model = strings.ToLower(strings.TrimSpace(model))
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	bestScore := -1
	var best aiCostRate
	for _, rate := range cfg.Rates {
		score := aiCostRateMatchScore(rate, providerID, providerName, model)
		if score > bestScore {
			bestScore = score
			best = rate
		}
	}
	if bestScore < 0 {
		return 0, false
	}
	if best.TotalPerMillion > 0 {
		return roundCost(float64(usage.TotalTokens) * best.TotalPerMillion / 1_000_000), true
	}
	cost := float64(usage.PromptTokens)*best.InputPerMillion + float64(usage.CompletionTokens)*best.OutputPerMillion
	if cost == 0 && usage.TotalTokens > 0 && best.InputPerMillion > 0 && best.InputPerMillion == best.OutputPerMillion {
		cost = float64(usage.TotalTokens) * best.InputPerMillion
	}
	if cost <= 0 {
		return 0, false
	}
	return roundCost(cost / 1_000_000), true
}

func aiCostRateMatchScore(rate aiCostRate, providerID uint, providerName string, model string) int {
	modelMatch := rate.Model == model || rate.Model == "*"
	if !modelMatch {
		return -1
	}
	score := 1
	if rate.Model == model {
		score += 2
	}
	if rate.ProviderID > 0 {
		if providerID == rate.ProviderID {
			score += 4
		} else {
			return -1
		}
	}
	if rate.ProviderName != "" {
		if providerName == rate.ProviderName {
			score += 3
		} else {
			return -1
		}
	}
	return score
}

func costBudgetStatus(window aiUsageAccumulator, cfg aiCostConfig) string {
	if !cfg.BudgetConfigured {
		return "not_configured"
	}
	if window.PayloadCostCalls == 0 && window.EstimatedCostCalls == 0 {
		return "no_cost_data"
	}
	used := window.CostAmount
	if used > cfg.DailyBudget {
		return "exceeded"
	}
	if used >= cfg.DailyBudget*0.8 {
		return "warning"
	}
	return "ok"
}

func nullableBudget(cfg aiCostConfig) any {
	if !cfg.BudgetConfigured {
		return nil
	}
	return roundCost(cfg.DailyBudget)
}

func costBudgetUsedPct(window aiUsageAccumulator, cfg aiCostConfig) any {
	if !cfg.BudgetConfigured || cfg.DailyBudget <= 0 || window.PayloadCostCalls == 0 && window.EstimatedCostCalls == 0 {
		return nil
	}
	return math.Round(window.CostAmount/cfg.DailyBudget*10000) / 100
}

func appSettingValue(raw []byte) any {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	if obj, ok := value.(map[string]any); ok {
		if inner, exists := obj["value"]; exists {
			return inner
		}
	}
	return value
}

func roundCost(value float64) float64 {
	return math.Round(value*1_000_000) / 1_000_000
}

func marketProviderDiagnostics(db *gorm.DB) map[string]any {
	cfg := marketdata.RuntimeConfigFromDB(db, config.Load())
	var latest persistmodel.RealtimeQuote
	lastQuoteAge := any(nil)
	lastQuoteProvider := ""
	lastQuoteCode := ""
	lastQuoteTime := any(nil)
	if err := db.Order("quote_time desc").First(&latest).Error; err == nil {
		age := int(time.Since(latest.QuoteTime).Seconds())
		if age < 0 {
			age = 0
		}
		lastQuoteAge = age
		lastQuoteProvider = latest.Provider
		lastQuoteCode = latest.Code
		lastQuoteTime = latest.QuoteTime
	}
	var latestDaily persistmodel.DailyBar
	lastDailyCode := ""
	lastDailyDate := any(nil)
	lastDailyAgeDays := any(nil)
	if err := db.Order("trade_date desc").First(&latestDaily).Error; err == nil {
		lastDailyCode = latestDaily.Code
		lastDailyDate = latestDaily.TradeDate
		ageDays := int(time.Since(latestDaily.TradeDate).Hours() / 24)
		if ageDays < 0 {
			ageDays = 0
		}
		lastDailyAgeDays = ageDays
	}
	freshnessStatus, healthScore := marketFreshnessStatus(lastQuoteAge, lastDailyAgeDays)
	return map[string]any{
		"default_provider":           cfg.DefaultProvider,
		"realtime_provider":          cfg.RealtimeProvider,
		"adata_enabled":              cfg.DefaultProvider == marketdata.ProviderAdata || cfg.RealtimeProvider == marketdata.ProviderAdata,
		"realtime_cache_ttl_seconds": int(cfg.RealtimeCacheTTL.Seconds()),
		"has_tushare_token":          countWhere(db, &persistmodel.Secret{}, "kind = ? AND name = ?", domainkernel.SecretKindMarketData, "tushare_token") > 0,
		"freshness_status":           freshnessStatus,
		"source_health_score":        healthScore,
		"last_quote_code":            lastQuoteCode,
		"last_quote_time":            lastQuoteTime,
		"last_quote_provider":        lastQuoteProvider,
		"last_quote_age_seconds":     lastQuoteAge,
		"last_daily_bar_code":        lastDailyCode,
		"last_daily_bar_date":        lastDailyDate,
		"last_daily_bar_age_days":    lastDailyAgeDays,
	}
}

func messagingProviderDiagnostics(db *gorm.DB, settings config.Settings, listenerStatus, adapterStatus map[string]any) map[string]any {
	filterCounts := messageFilterStatusCounts(db)
	return map[string]any{
		"mtproto": map[string]any{
			"has_app_id":   hasUsableSecret(db, settings, domainkernel.SecretKindMessageSubscription, "telegram:app_id"),
			"has_app_hash": hasUsableSecret(db, settings, domainkernel.SecretKindMessageSubscription, "telegram:app_hash"),
			"has_session":  hasUsableSecret(db, settings, domainkernel.SecretKindMessageSubscription, "telegram:mtproto_session"),
			"status":       listenerStatus,
		},
		"platform_adapter": map[string]any{
			"enabled": countWhere(db, &persistmodel.PlatformAdapter{}, "enabled = ?", true),
			"status":  adapterStatus,
		},
		"subscriptions": map[string]any{
			"enabled":                  countWhere(db, &persistmodel.MessageSubscription{}, "enabled = ?", true),
			"total":                    countWhere(db, &persistmodel.MessageSubscription{}, ""),
			"due":                      countWhere(db, &persistmodel.MessageSubscription{}, "enabled = ? AND next_collect_at IS NOT NULL AND next_collect_at <= ?", true, time.Now()),
			"collect_error_count":      countWhere(db, &persistmodel.MessageSubscription{}, "last_collect_error IS NOT NULL AND last_collect_error <> ''"),
			"unfiltered_messages":      filterCounts[domainmsg.FilterStatusUnfiltered],
			"filtering_messages":       filterCounts[domainmsg.FilterStatusFiltering],
			"filtered_messages":        filterCounts[domainmsg.FilterStatusFiltered],
			"failed_filter_messages":   filterCounts[domainmsg.FilterStatusFailed],
			"filter_status_counts":     filterCounts,
			"provider_counts":          messageProviderCounts(db),
			"dedupe_policy":            map[string]any{"scope": "subscription_id + source_message_id", "behavior": "duplicate source messages update the existing resource instead of creating a new row"},
			"source_trust_scoring":     "manual_feedback_enabled",
			"source_trust_scores":      messageSourceTrustScores(db),
			"private_source_readiness": privateSourceReadiness(db, settings),
		},
	}
}

func marketFreshnessStatus(lastQuoteAge any, lastDailyAgeDays any) (string, int) {
	quoteAge, hasQuote := lastQuoteAge.(int)
	dailyAge, hasDaily := lastDailyAgeDays.(int)
	if !hasQuote && !hasDaily {
		return "empty", 0
	}
	if hasQuote && quoteAge <= int((30*time.Minute).Seconds()) {
		return "fresh", 100
	}
	if hasDaily && dailyAge <= 5 {
		return "daily_only", 70
	}
	return "stale", 40
}

func messageFilterStatusCounts(db *gorm.DB) map[string]int64 {
	out := map[string]int64{
		domainmsg.FilterStatusUnfiltered: 0,
		domainmsg.FilterStatusFiltering:  0,
		domainmsg.FilterStatusFiltered:   0,
		domainmsg.FilterStatusFailed:     0,
	}
	var rows []struct {
		Status string
		Count  int64
	}
	db.Model(&persistmodel.IngestedMessage{}).Select("filter_status as status, COUNT(*) as count").Where("filter_status <> ''").Group("filter_status").Scan(&rows)
	for _, row := range rows {
		status := strings.TrimSpace(row.Status)
		out[status] = row.Count
	}
	legacy := countWhere(db, &persistmodel.IngestedMessage{}, "filter_status = '' AND filter_decision IS NULL")
	if legacy > 0 {
		out[domainmsg.FilterStatusUnfiltered] += legacy
	}
	return out
}

func messageProviderCounts(db *gorm.DB) []map[string]any {
	var rows []struct {
		Provider string
		Count    int64
	}
	db.Model(&persistmodel.IngestedMessage{}).Select("provider, COUNT(*) as count").Group("provider").Scan(&rows)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		provider := strings.TrimSpace(row.Provider)
		if provider == "" {
			provider = "unknown"
		}
		out = append(out, map[string]any{"provider": provider, "count": row.Count})
	}
	return out
}

func messageSourceTrustScores(db *gorm.DB) []map[string]any {
	var rows []persistmodel.IngestedMessage
	db.Preload("Subscription").Where("feedback_label IS NOT NULL AND feedback_label <> ''").Order("provider, subscription_id").Find(&rows)
	type bucket struct {
		Provider      string
		SourceRef     string
		Title         string
		Samples       int
		Helpful       int
		Noise         int
		Misclassified int
		Neutral       int
		Score         int
	}
	buckets := map[string]*bucket{}
	for _, row := range rows {
		sourceRef := ""
		title := ""
		if row.Subscription != nil {
			sourceRef = row.Subscription.SourceRef
			title = row.Subscription.Title
		}
		if sourceRef == "" {
			sourceRef = fmt.Sprintf("subscription:%d", row.SubscriptionID)
		}
		key := row.Provider + "\x00" + sourceRef
		item := buckets[key]
		if item == nil {
			item = &bucket{Provider: row.Provider, SourceRef: sourceRef, Title: title}
			buckets[key] = item
		}
		item.Samples++
		label := ""
		if row.FeedbackLabel != nil {
			label = strings.TrimSpace(*row.FeedbackLabel)
		}
		switch label {
		case domainmsg.FeedbackHelpful:
			item.Helpful++
			item.Score++
		case domainmsg.FeedbackNoise:
			item.Noise++
			item.Score--
		case domainmsg.FeedbackMisclassified:
			item.Misclassified++
			item.Score--
		case domainmsg.FeedbackNeutral:
			item.Neutral++
		}
	}
	out := make([]map[string]any, 0, len(buckets))
	for _, item := range buckets {
		trustScore := 50
		if item.Samples > 0 {
			trustScore = 50 + int(float64(item.Score)/float64(item.Samples)*50)
		}
		if trustScore < 0 {
			trustScore = 0
		}
		if trustScore > 100 {
			trustScore = 100
		}
		out = append(out, map[string]any{
			"provider": item.Provider, "source_ref": item.SourceRef, "title": item.Title, "feedback_count": item.Samples,
			"helpful_count": item.Helpful, "noise_count": item.Noise, "misclassified_count": item.Misclassified, "neutral_count": item.Neutral,
			"trust_score": trustScore,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i]["trust_score"].(int) != out[j]["trust_score"].(int) {
			return out[i]["trust_score"].(int) > out[j]["trust_score"].(int)
		}
		return fmt.Sprint(out[i]["source_ref"]) < fmt.Sprint(out[j]["source_ref"])
	})
	return out
}

func privateSourceReadiness(db *gorm.DB, settings config.Settings) map[string]any {
	return map[string]any{
		"telegram_private_channels": hasUsableSecret(db, settings, domainkernel.SecretKindMessageSubscription, "telegram:mtproto_session"),
		"rss_authenticated_feeds":   false,
		"proxy_config_required":     "per runtime proxy settings",
	}
}

func countWhere(db *gorm.DB, model any, cond string, args ...any) int64 {
	var count int64
	q := db.Model(model)
	if cond != "" {
		q = q.Where(cond, args...)
	}
	q.Count(&count)
	return count
}

func hasUsableSecret(db *gorm.DB, settings config.Settings, kind domainkernel.SecretKind, name string) bool {
	var row persistmodel.Secret
	if err := db.First(&row, "kind = ? AND name = ?", kind, name).Error; err != nil {
		return false
	}
	value, err := security.New(settings).DecryptSecret(row.EncryptedValue)
	if err != nil || strings.TrimSpace(value) == "" {
		return false
	}
	if kind == domainkernel.SecretKindMessageSubscription && name == "telegram:app_id" {
		appID, err := strconv.Atoi(strings.TrimSpace(value))
		return err == nil && appID > 0
	}
	return true
}

func int64FromDashboardValue(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func int64FromAny(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case int32:
		return int64(typed)
	case uint:
		return int64(typed)
	case uint64:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	case string:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		return parsed
	default:
		parsed, _ := strconv.ParseInt(strings.TrimSpace(fmt.Sprint(value)), 10, 64)
		return parsed
	}
}

func uintFromAny(value any) uint {
	parsed := int64FromAny(value)
	if parsed <= 0 {
		return 0
	}
	return uint(parsed)
}

func float64FromAny(value any) (float64, bool) {
	switch typed := value.(type) {
	case nil:
		return 0, false
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	default:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64)
		return parsed, err == nil
	}
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value == nil {
			continue
		}
		if text := strings.TrimSpace(fmt.Sprint(value)); text == "" || text == "<nil>" {
			continue
		}
		return value
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func camelizeJSONKeys(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[snakeToLowerCamel(key)] = camelizeJSONKeys(item)
		}
		return out
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, camelizeJSONKeys(item))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, camelizeJSONKeys(item))
		}
		return out
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return value
		}
		var object map[string]any
		if err := json.Unmarshal(raw, &object); err == nil && object != nil {
			return camelizeJSONKeys(object)
		}
		var list []any
		if err := json.Unmarshal(raw, &list); err == nil && list != nil {
			return camelizeJSONKeys(list)
		}
		return value
	}
}

func snakeToLowerCamel(value string) string {
	parts := strings.Split(value, "_")
	if len(parts) == 1 {
		return value
	}
	var b strings.Builder
	b.WriteString(parts[0])
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			b.WriteString(part[1:])
		}
	}
	return b.String()
}
