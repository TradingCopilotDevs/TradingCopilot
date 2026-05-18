package integration

import (
	"context"
	"encoding/json"
	"fmt"
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/ai/client"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/connect"
	marketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/marketdata"
	infrapaper "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/paper"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/queue"
	infratelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/telegram"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"github.com/glebarez/sqlite"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRedisAsynqIntegrationHarness(t *testing.T) {
	requireIntegration(t, "TC_INTEGRATION_REDIS")
	redisURL := firstEnv("TC_REDIS_URL", "REDIS_URL")
	if redisURL == "" {
		t.Skip("set TC_REDIS_URL or REDIS_URL to run Redis/asynq integration harness")
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("invalid Redis URL: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := redis.NewClient(opt)
	defer client.Close()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("redis ping failed: %v", err)
	}

	taskID := "integration:run_meeting:" + strconv.FormatInt(time.Now().UnixNano(), 10)
	asynqClient := asynq.NewClient(jobs.RedisClientOpt(redisURL))
	defer asynqClient.Close()
	task, _ := asynqClient.Enqueue(asynq.NewTask(jobs.TypeRunMeeting, []byte(`{"meeting_id":0}`)), asynq.TaskID(taskID), asynq.Retention(0), asynq.Timeout(time.Minute))
	if task == nil {
		t.Fatal("expected asynq task info")
	}
	inspector := asynq.NewInspector(jobs.RedisClientOpt(redisURL))
	defer inspector.Close()
	_ = inspector.DeleteTask(task.Queue, taskID)

	if os.Getenv("TC_REDIS_CONSUMER_E2E") == "1" {
		db := newIntegrationSQLiteDB(t)
		settings := config.Settings{RedisURL: redisURL, MeetingDispatchMode: "redis", MeetingRunTimeout: time.Minute, MeetingStaleAfter: time.Minute, MeetingAutoRequeueLimit: 1}
		server := asynq.NewServer(jobs.RedisClientOpt(redisURL), asynq.Config{Concurrency: 1})
		if err := server.Start(jobs.NewServeMux(db, settings)); err != nil {
			t.Fatalf("asynq worker start failed: %v", err)
		}
		defer server.Shutdown()
		consumerTaskID := "integration:paper_maintenance:" + strconv.FormatInt(time.Now().UnixNano(), 10)
		if _, err := asynqClient.Enqueue(asynq.NewTask(jobs.TypeRunPaperMaintenance, nil), asynq.TaskID(consumerTaskID), asynq.Retention(time.Minute), asynq.Timeout(time.Minute)); err != nil {
			t.Fatalf("enqueue consumer E2E task failed: %v", err)
		}
		waitForAsynqTaskState(t, inspector, "default", consumerTaskID, asynq.TaskStateCompleted)
		_ = inspector.DeleteTask("default", consumerTaskID)

		scheduled := []jobs.PeriodicJob{{Type: jobs.TypeRecoverQueuedMeetings, Interval: time.Minute}}
		next := map[string]time.Time{}
		now := time.Now()
		schedulerTaskID := jobs.PeriodicTaskID(scheduled[0], now)
		_ = inspector.DeleteTask("default", schedulerTaskID)
		if err := jobs.EnqueueDuePeriodicJobs(asynqClient, now, scheduled, next); err != nil {
			t.Fatalf("scheduler one-shot enqueue failed: %v", err)
		}
		waitForAsynqTaskVisible(t, inspector, "default", schedulerTaskID)
		_ = inspector.DeleteTask("default", schedulerTaskID)
	}
}

func TestPostgresTimescaleSchemaInitHarness(t *testing.T) {
	requireIntegration(t, "TC_INTEGRATION_POSTGRES")
	databaseURL := firstEnv("TC_DATABASE_URL", "DATABASE_URL")
	if !strings.HasPrefix(databaseURL, "postgres://") && !strings.HasPrefix(databaseURL, "postgresql://") && !strings.HasPrefix(databaseURL, "postgresql+asyncpg://") {
		t.Skip("set TC_DATABASE_URL or DATABASE_URL to a PostgreSQL/TimescaleDB URL")
	}
	db, err := database.Open(config.Settings{DatabaseURL: databaseURL})
	if err != nil {
		t.Fatalf("open postgres failed: %v", err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatalf("postgres/timescale schema initialization failed: %v", err)
	}
	var dailyHypertable int64
	_ = db.Raw("SELECT COUNT(*) FROM timescaledb_information.hypertables WHERE hypertable_name = ?", "daily_bars").Scan(&dailyHypertable).Error
	if os.Getenv("TC_REQUIRE_TIMESCALE") == "1" && dailyHypertable == 0 {
		t.Fatal("daily_bars is not registered as a Timescale hypertable")
	}
	verifyPostgresPaperNumericDefaults(t, db)
}

func verifyPostgresPaperNumericDefaults(t *testing.T, db *gorm.DB) {
	t.Helper()
	nameSuffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	cfg := infrapaper.DefaultPaperRiskConfig()
	cfg.Name = "integration-risk-" + nameSuffix
	if err := db.Create(&cfg).Error; err != nil {
		t.Fatalf("create paper risk config failed: %v", err)
	}
	account := domainpaper.Account{Name: "integration-account-" + nameSuffix, InitialCash: decimal.NewFromInt(100000), Cash: decimal.NewFromInt(100000), RiskConfigID: &cfg.ID}
	if err := db.Create(&account).Error; err != nil {
		t.Fatalf("create paper account failed: %v", err)
	}
	executeAt := time.Now().Add(-time.Minute)
	order := domainpaper.Order{AccountID: account.ID, Code: "600519", Side: domainkernel.OrderBuy, Quantity: 100, Status: domainkernel.OrderPending, SuggestedPrice: decimal.RequireFromString("100.123456"), ExecuteAfter: &executeAt}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create paper order failed: %v", err)
	}
	var loaded domainpaper.Order
	if err := db.First(&loaded, order.ID).Error; err != nil {
		t.Fatalf("load paper order failed: %v", err)
	}
	if loaded.Status != domainkernel.OrderPending || loaded.Quantity != 100 || !loaded.SuggestedPrice.Round(4).Equal(decimal.RequireFromString("100.1235")) {
		t.Fatalf("postgres paper numeric/default mismatch: %+v", loaded)
	}
}

func TestMarketProviderIntegrationHarness(t *testing.T) {
	requireIntegration(t, "TC_INTEGRATION_MARKET")
	code := firstNonEmpty(os.Getenv("TC_MARKET_CODE"), "600519")
	providerMode := strings.ToLower(firstNonEmpty(os.Getenv("TC_MARKET_PROVIDER"), "all"))
	if providerMode == "all" || providerMode == "adata" {
		provider := marketdata.NewAdataProvider()
		rows, err := provider.FetchDaily(code, nil, nil)
		if err != nil {
			t.Fatalf("adata daily fetch failed: %v", err)
		}
		if len(rows) == 0 {
			t.Fatalf("adata daily fetch returned no rows for %s", code)
		}
		if quoteProvider, quotes, err := marketdata.FetchRealtimeQuotes([]string{code}, marketdata.ProviderAdata); err != nil {
			t.Fatalf("adata realtime fetch failed: %v", err)
		} else if quoteProvider != marketdata.ProviderAdata || len(quotes) == 0 {
			t.Fatalf("adata realtime fetch returned no quote for %s: provider=%s rows=%d", code, quoteProvider, len(quotes))
		}
	}
	if os.Getenv("TC_INTEGRATION_MARKET_FIVE_DAY") == "1" {
		rows, err := marketdata.SeriesForCode(code, "five_day")
		if err != nil {
			t.Fatalf("five_day compatibility fetch failed: %v", err)
		}
		if len(rows) == 0 {
			t.Fatalf("five_day compatibility fetch returned no rows for %s", code)
		}
	}

	if token := strings.TrimSpace(os.Getenv("TC_TUSHARE_TOKEN")); token != "" {
		if providerMode != "all" && providerMode != "tushare" {
			t.Skipf("TC_TUSHARE_TOKEN is set, but TC_MARKET_PROVIDER=%s does not include tushare", providerMode)
		}
		tushareMode := strings.ToLower(firstNonEmpty(os.Getenv("TC_TUSHARE_MODE"), "daily"))
		provider := &marketdata.HTTPHistoryProvider{ProviderName: "tushare", Token: token, BaseURL: "http://api.tushare.pro"}
		if tushareMode != "all" && tushareMode != "daily" && tushareMode != "stock_basic" && tushareMode != "symbols" {
			t.Fatalf("unsupported TC_TUSHARE_MODE=%s", tushareMode)
		}
		if tushareMode == "all" || tushareMode == "daily" {
			db := newIntegrationSQLiteDB(t)
			settings := config.Settings{AppSecretKey: "integration-market-secret", DefaultMarketProvider: "tushare"}
			encrypted, err := security.New(settings).EncryptSecret(token)
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&domainsettings.Secret{Kind: domainkernel.SecretKindMarketData, Name: "tushare_token", EncryptedValue: encrypted}).Error; err != nil {
				t.Fatal(err)
			}
			count, err := marketdata.RefreshDailyBars(db, settings, code, nil, nil)
			if err != nil {
				t.Fatalf("Tushare daily refresh failed: %v", err)
			}
			if count == 0 {
				t.Fatalf("Tushare daily refresh returned no rows for %s", code)
			}
		}
		if tushareMode == "all" || tushareMode == "stock_basic" || tushareMode == "symbols" {
			symbols, err := provider.FetchSymbols()
			if err != nil {
				if marketdata.IsTushareAccessBoundaryError(err) && allowTushareAccessBoundary() {
					t.Logf("Tushare stock_basic access boundary allowed by harness config: %v", err)
					return
				}
				t.Fatalf("Tushare stock_basic fetch failed: %v", err)
			}
			if len(symbols) == 0 {
				t.Fatal("Tushare stock_basic returned no A-share symbols")
			}
		}
	} else if providerMode == "tushare" {
		t.Skip("set TC_TUSHARE_TOKEN to run Tushare market integration harness")
	}
}

func TestAIProviderIntegrationHarness(t *testing.T) {
	requireIntegration(t, "TC_INTEGRATION_AI")
	baseURL := strings.TrimSpace(os.Getenv("TC_AI_BASE_URL"))
	apiKey := strings.TrimSpace(os.Getenv("TC_AI_API_KEY"))
	model := strings.TrimSpace(os.Getenv("TC_AI_MODEL"))
	if baseURL == "" || apiKey == "" || model == "" {
		t.Skip("set TC_AI_BASE_URL, TC_AI_API_KEY, and TC_AI_MODEL to run AI provider integration harness")
	}
	settings := config.Settings{
		AppSecretKey:      "integration-ai-secret",
		AIChatTimeout:     120 * time.Second,
		AIChatMaxAttempts: 2,
		AIChatBackoffBase: 500 * time.Millisecond,
		AIChatBackoffMax:  2 * time.Second,
	}
	sec := security.New(settings)
	encrypted, err := sec.EncryptSecret(apiKey)
	if err != nil {
		t.Fatal(err)
	}
	client := ai.Client{
		Provider: domainai.Provider{
			Name:         "integration-ai",
			BaseURL:      baseURL,
			DefaultModel: model,
			APIKeySecret: &domainsettings.Secret{EncryptedValue: encrypted},
		},
		Security: sec,
		Settings: settings,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	result, err := client.ChatWithUsageWithContext(ctx, []map[string]string{
		{"role": "system", "content": "Return only compact JSON. Do not use markdown."},
		{"role": "user", "content": `Return exactly this JSON object with no extra text: {"ok":true,"provider":"tradingcopilot"}`},
	}, model)
	if err != nil {
		t.Fatalf("AI provider chat failed: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(extractJSONObject(result.Content)), &payload); err != nil {
		t.Fatalf("AI provider did not return parseable JSON: %v; response preview=%q", err, truncateForTest(result.Content, 240))
	}
	if payload["ok"] != true || fmt.Sprint(payload["provider"]) != "tradingcopilot" {
		t.Fatalf("AI provider JSON payload mismatch: %+v", payload)
	}
	if result.Usage != nil {
		t.Logf("AI provider usage total_tokens=%d", result.Usage.TotalTokens)
	}
}

func allowTushareAccessBoundary() bool {
	return os.Getenv("TC_TUSHARE_ALLOW_ACCESS_BOUNDARY") == "1" || os.Getenv("TC_TUSHARE_ALLOW_PERMISSION_DENIED") == "1"
}

func TestTelegramMTProtoIntegrationHarness(t *testing.T) {
	requireIntegration(t, "TC_INTEGRATION_TELEGRAM")
	if os.Getenv("TC_TELEGRAM_USE_DATABASE") == "1" {
		settings := config.Load()
		db, err := database.Open(settings)
		if err != nil {
			t.Fatalf("open configured database failed: %v", err)
		}
		sec := security.New(settings)
		status := infratelegram.TelegramMTProtoStatus(db)
		if !status["has_app_id"] || !status["has_app_hash"] || !status["has_session"] {
			t.Fatalf("telegram status mismatch from configured database: %+v", status)
		}
		if channelRef := strings.TrimSpace(os.Getenv("TC_TELEGRAM_CHANNEL_REF")); channelRef != "" {
			if _, err := infratelegram.GetLatestTelegramMTProtoMessage(db, sec, channelRef); err != nil {
				t.Fatalf("latest Telegram MTProto message fetch failed for %s: %v", channelRef, err)
			}
		}
		return
	}
	appID := strings.TrimSpace(os.Getenv("TC_TELEGRAM_APP_ID"))
	appHash := strings.TrimSpace(os.Getenv("TC_TELEGRAM_APP_HASH"))
	session := strings.TrimSpace(os.Getenv("TC_TELEGRAM_SESSION"))
	if appID == "" || appHash == "" || session == "" {
		t.Skip("set TC_TELEGRAM_APP_ID, TC_TELEGRAM_APP_HASH, and TC_TELEGRAM_SESSION to run Telegram MTProto integration harness")
	}
	settings := config.Settings{AppSecretKey: "integration-telegram-secret"}
	sec := security.New(settings)
	db := newIntegrationSQLiteDB(t)
	for name, value := range map[string]string{"app_id": appID, "app_hash": appHash, "mtproto_session": session} {
		encrypted, err := sec.EncryptSecret(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&domainsettings.Secret{Kind: domainkernel.SecretKindTelegram, Name: name, EncryptedValue: encrypted}).Error; err != nil {
			t.Fatal(err)
		}
	}
	status := infratelegram.TelegramMTProtoStatus(db)
	if !status["has_app_id"] || !status["has_app_hash"] || !status["has_session"] {
		t.Fatalf("telegram status mismatch: %+v", status)
	}
	if channelRef := strings.TrimSpace(os.Getenv("TC_TELEGRAM_CHANNEL_REF")); channelRef != "" {
		if _, err := infratelegram.GetLatestTelegramMTProtoMessage(db, sec, channelRef); err != nil {
			t.Fatalf("latest Telegram MTProto message fetch failed for %s: %v", channelRef, err)
		}
	}
}

func requireIntegration(t *testing.T, key string) {
	t.Helper()
	if os.Getenv(key) != "1" {
		t.Skipf("set %s=1 to run this external integration harness", key)
	}
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func extractJSONObject(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end >= start {
		return text[start : end+1]
	}
	return text
}

func truncateForTest(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

func newIntegrationSQLiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func waitForAsynqTaskState(t *testing.T, inspector *asynq.Inspector, queue string, taskID string, state asynq.TaskState) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastState string
	for time.Now().Before(deadline) {
		info, err := inspector.GetTaskInfo(queue, taskID)
		if err == nil {
			lastState = info.State.String()
			if info.State == state {
				return
			}
			if info.State == asynq.TaskStateArchived || info.State == asynq.TaskStateRetry {
				t.Fatalf("task %s reached state=%s last_err=%s", taskID, info.State, info.LastErr)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("task %s did not reach state=%s, last_state=%s", taskID, state, lastState)
}

func waitForAsynqTaskVisible(t *testing.T, inspector *asynq.Inspector, queue string, taskID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, err := inspector.GetTaskInfo(queue, taskID); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("task %s did not become visible in asynq inspector: %v", taskID, lastErr)
}
