package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	"strconv"
	"strings"
	"time"

	appsystem "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/system"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/connect"
	marketdata "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/marketdata"
	persistmodel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/model"
	infrapaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/paper"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	gormuow "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/uow"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
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
			"market_symbol_count": countWhere(u.db, &persistmodel.MarketSymbol{}, ""), "market_watchlist_count": countWhere(u.db, &persistmodel.WatchlistItem{}, ""), "market_active_watchlist_count": countWhere(u.db, &persistmodel.WatchlistItem{}, "active = ?", true), "paper_is_trading_time": infrapaper.IsTradingTime(time.Now()),
		},
		"recent_activity":        map[string]any{"recent_meetings": recentMeetings(u.db), "recent_ingested_messages": recentIngestedMessages(u.db)},
		"dependency_diagnostics": dashboardDependencyDiagnostics(u.db, u.settings, redisStatus, workerStatus, schedulerStatus, subscriptionStatus, platformAdapterStatus, paperStatus, newsFilterReady),
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

func dashboardDependencyDiagnostics(db *gorm.DB, settings config.Settings, redisStatus, workerStatus, schedulerStatus, subscriptionStatus, platformAdapterStatus, paperStatus map[string]any, newsFilterReady bool) map[string]any {
	return map[string]any{
		"redis": redisStatus,
		"background": map[string]any{
			"worker":    workerStatus,
			"scheduler": schedulerStatus,
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

func marketProviderDiagnostics(db *gorm.DB) map[string]any {
	cfg := marketdata.RuntimeConfigFromDB(db, config.Load())
	var latest persistmodel.RealtimeQuote
	lastQuoteAge := any(nil)
	lastQuoteProvider := ""
	if err := db.Order("quote_time desc").First(&latest).Error; err == nil {
		age := int(time.Since(latest.QuoteTime).Seconds())
		if age < 0 {
			age = 0
		}
		lastQuoteAge = age
		lastQuoteProvider = latest.Provider
	}
	return map[string]any{
		"default_provider":           cfg.DefaultProvider,
		"realtime_provider":          cfg.RealtimeProvider,
		"adata_enabled":              cfg.DefaultProvider == marketdata.ProviderAdata || cfg.RealtimeProvider == marketdata.ProviderAdata,
		"realtime_cache_ttl_seconds": int(cfg.RealtimeCacheTTL.Seconds()),
		"has_tushare_token":          countWhere(db, &persistmodel.Secret{}, "kind = ? AND name = ?", domainkernel.SecretKindMarketData, "tushare_token") > 0,
		"last_quote_provider":        lastQuoteProvider,
		"last_quote_age_seconds":     lastQuoteAge,
	}
}

func messagingProviderDiagnostics(db *gorm.DB, settings config.Settings, listenerStatus, adapterStatus map[string]any) map[string]any {
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
			"enabled":             countWhere(db, &persistmodel.MessageSubscription{}, "enabled = ?", true),
			"total":               countWhere(db, &persistmodel.MessageSubscription{}, ""),
			"unfiltered_messages": countWhere(db, &persistmodel.IngestedMessage{}, "filter_status = ? OR (filter_status = '' AND filter_decision IS NULL)", "unfiltered"),
		},
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
