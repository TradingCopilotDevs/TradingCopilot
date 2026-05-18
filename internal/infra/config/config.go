package config

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"
)

type Settings struct {
	AppName                             string
	AppEnv                              string
	PublicBaseURL                       string
	APIBaseURL                          string
	CORSOrigins                         []string
	DatabaseURL                         string
	RedisURL                            string
	AppSecretKey                        string
	JWTSecretKey                        string
	JWTAlgorithm                        string
	AccessTokenExpireMinutes            int
	AutoCreateTables                    bool
	DefaultMarketProvider               string
	MarketRealtimeProvider              string
	MarketRealtimeCompatProvider        string
	MarketRealtimeCacheTTL              time.Duration
	MeetingDispatchMode                 string
	MeetingMaxRounds                    int
	MeetingDailyTokenBudget             int
	MeetingStaleAfter                   time.Duration
	MeetingAutoRequeueLimit             int
	MeetingRunTimeout                   time.Duration
	AIChatTimeout                       time.Duration
	AIChatMaxAttempts                   int
	AIChatBackoffBase                   time.Duration
	AIChatBackoffMax                    time.Duration
	AIJSONMaxAttempts                   int
	ToolResultLimit                     int
	SQLStatementTimeoutMillis           int
	MessageTaskQueueMode                string
	MessageSubscriptionListenersInServe bool
	LogDir                              string
	LogLevel                            string
	LogRotationMode                     string
	LogRotationSizeMB                   int
	LogRotationTotalSizeMB              int
	LogRotationMaxAgeDays               int
	FrontendDist                        string
	RuntimeEnvFile                      string
	HTTPAddr                            string
}

func Load() Settings {
	loadDotEnv(env("TC_ENV_FILE", ".env"))
	return Settings{
		AppName:                             env("APP_NAME", "TradingCopilot"),
		AppEnv:                              env("APP_ENV", "dev"),
		PublicBaseURL:                       env("PUBLIC_BASE_URL", "http://localhost:5173"),
		APIBaseURL:                          env("API_BASE_URL", "http://localhost:8000"),
		CORSOrigins:                         envJSONList("CORS_ORIGINS", []string{"http://localhost:5173"}),
		DatabaseURL:                         env("DATABASE_URL", "sqlite+aiosqlite:///./data/local-dev.db"),
		RedisURL:                            env("REDIS_URL", "redis://localhost:6379/0"),
		AppSecretKey:                        env("APP_SECRET_KEY", "local-dev-secret-change-before-real-use-32-bytes"),
		JWTSecretKey:                        env("JWT_SECRET_KEY", "local-dev-jwt-secret-change-before-real-use"),
		JWTAlgorithm:                        env("JWT_ALGORITHM", "HS256"),
		AccessTokenExpireMinutes:            envInt("ACCESS_TOKEN_EXPIRE_MINUTES", 60*24),
		AutoCreateTables:                    envBool("AUTO_CREATE_TABLES", true),
		DefaultMarketProvider:               env("DEFAULT_MARKET_PROVIDER", "adata"),
		MarketRealtimeProvider:              env("MARKET_REALTIME_PROVIDER", "adata"),
		MarketRealtimeCompatProvider:        env("MARKET_REALTIME_COMPAT_PROVIDER", "tencent"),
		MarketRealtimeCacheTTL:              time.Duration(envInt("MARKET_REALTIME_CACHE_TTL_SECONDS", 5)) * time.Second,
		MeetingDispatchMode:                 env("MEETING_DISPATCH_MODE", "local"),
		MeetingMaxRounds:                    envInt("MEETING_MAX_ROUNDS", 6),
		MeetingDailyTokenBudget:             envMeetingDailyTokenBudget(),
		MeetingStaleAfter:                   time.Duration(envInt("MEETING_STALE_AFTER_SECONDS", 1800)) * time.Second,
		MeetingAutoRequeueLimit:             envInt("MEETING_AUTO_REQUEUE_LIMIT", 2),
		MeetingRunTimeout:                   time.Duration(envInt("MEETING_RUN_TIMEOUT_SECONDS", 7200)) * time.Second,
		AIChatTimeout:                       time.Duration(envInt("AI_CHAT_TIMEOUT_SECONDS", 90)) * time.Second,
		AIChatMaxAttempts:                   envInt("AI_CHAT_MAX_ATTEMPTS", 5),
		AIChatBackoffBase:                   time.Duration(envFloatMillis("AI_CHAT_BACKOFF_BASE_SECONDS", 1.0) * float64(time.Second)),
		AIChatBackoffMax:                    time.Duration(envFloatMillis("AI_CHAT_BACKOFF_MAX_SECONDS", 20.0) * float64(time.Second)),
		AIJSONMaxAttempts:                   envInt("AI_JSON_MAX_ATTEMPTS", 3),
		ToolResultLimit:                     envInt("TOOL_RESULT_LIMIT", 200),
		SQLStatementTimeoutMillis:           envInt("SQL_STATEMENT_TIMEOUT_MS", 5000),
		MessageTaskQueueMode:                env("MESSAGE_TASK_QUEUE_MODE", "auto"),
		MessageSubscriptionListenersInServe: envBool("MESSAGE_SUBSCRIPTION_LISTENERS_IN_SERVE", true),
		LogDir:                              env("LOG_DIR", "./logs"),
		LogLevel:                            env("LOG_LEVEL", "info"),
		LogRotationMode:                     env("LOG_ROTATION_MODE", "size"),
		LogRotationSizeMB:                   envInt("LOG_ROTATION_SIZE_MB", 5),
		LogRotationTotalSizeMB:              envInt("LOG_ROTATION_TOTAL_SIZE_MB", 100),
		LogRotationMaxAgeDays:               envInt("LOG_ROTATION_MAX_AGE_DAYS", 7),
		FrontendDist:                        env("TC_FRONTEND_DIST", "frontend/dist"),
		RuntimeEnvFile:                      env("TC_ENV_FILE", ".env"),
		HTTPAddr:                            env("HTTP_ADDR", ":8000"),
	}
}

func env(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envMeetingDailyTokenBudget() int {
	parsed := envInt("MEETING_DAILY_TOKEN_BUDGET", -1)
	if parsed == -1 || parsed > 0 {
		return parsed
	}
	return -1
}

func envFloatMillis(key string, fallback float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envJSONList(key string, fallback []string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	var parsed []string
	if err := json.Unmarshal([]byte(value), &parsed); err == nil && len(parsed) > 0 {
		return parsed
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func loadDotEnv(path string) {
	content, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		key, value, _ := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}
