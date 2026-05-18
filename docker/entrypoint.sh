#!/bin/sh
set -eu

ENV_FILE="${TC_ENV_FILE:-/app/runtime/env/app.env}"
mkdir -p "$(dirname "$ENV_FILE")" /app/data

random_secret() {
  LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c 48
}

if [ ! -f "$ENV_FILE" ]; then
  APP_SECRET_KEY="${APP_SECRET_KEY:-$(random_secret)}"
  JWT_SECRET_KEY="${JWT_SECRET_KEY:-$(random_secret)}"
  cat > "$ENV_FILE" <<EOF
APP_ENV=${APP_ENV:-prod}
APP_NAME=${APP_NAME:-TradingCopilot}
APP_SECRET_KEY=${APP_SECRET_KEY}
JWT_SECRET_KEY=${JWT_SECRET_KEY}
JWT_ALGORITHM=${JWT_ALGORITHM:-HS256}
ACCESS_TOKEN_EXPIRE_MINUTES=${ACCESS_TOKEN_EXPIRE_MINUTES:-1440}
PUBLIC_BASE_URL=${PUBLIC_BASE_URL:-http://localhost:8000}
API_BASE_URL=${API_BASE_URL:-http://localhost:8000/api}
CORS_ORIGINS=${CORS_ORIGINS:-["http://localhost:8000"]}
DATABASE_URL=${DATABASE_URL:-postgresql://tradingcopilot:tradingcopilot@postgres:5432/tradingcopilot}
REDIS_URL=${REDIS_URL:-redis://redis:6379/0}
AUTO_CREATE_TABLES=${AUTO_CREATE_TABLES:-true}
DEFAULT_MARKET_PROVIDER=${DEFAULT_MARKET_PROVIDER:-adata}
MARKET_REALTIME_PROVIDER=${MARKET_REALTIME_PROVIDER:-adata}
MARKET_REALTIME_COMPAT_PROVIDER=${MARKET_REALTIME_COMPAT_PROVIDER:-tencent}
MARKET_REALTIME_CACHE_TTL_SECONDS=${MARKET_REALTIME_CACHE_TTL_SECONDS:-5}
MEETING_DISPATCH_MODE=${MEETING_DISPATCH_MODE:-auto}
MEETING_MAX_ROUNDS=${MEETING_MAX_ROUNDS:-6}
MEETING_DAILY_TOKEN_BUDGET=${MEETING_DAILY_TOKEN_BUDGET:--1}
MEETING_STALE_AFTER_SECONDS=${MEETING_STALE_AFTER_SECONDS:-1800}
MEETING_AUTO_REQUEUE_LIMIT=${MEETING_AUTO_REQUEUE_LIMIT:-2}
MEETING_RUN_TIMEOUT_SECONDS=${MEETING_RUN_TIMEOUT_SECONDS:-7200}
AI_CHAT_TIMEOUT_SECONDS=${AI_CHAT_TIMEOUT_SECONDS:-90}
AI_CHAT_MAX_ATTEMPTS=${AI_CHAT_MAX_ATTEMPTS:-5}
AI_CHAT_BACKOFF_BASE_SECONDS=${AI_CHAT_BACKOFF_BASE_SECONDS:-1}
AI_CHAT_BACKOFF_MAX_SECONDS=${AI_CHAT_BACKOFF_MAX_SECONDS:-20}
AI_JSON_MAX_ATTEMPTS=${AI_JSON_MAX_ATTEMPTS:-3}
TOOL_RESULT_LIMIT=${TOOL_RESULT_LIMIT:-200}
SQL_STATEMENT_TIMEOUT_MS=${SQL_STATEMENT_TIMEOUT_MS:-5000}
MESSAGE_TASK_QUEUE_MODE=${MESSAGE_TASK_QUEUE_MODE:-redis}
MESSAGE_SUBSCRIPTION_LISTENERS_IN_SERVE=${MESSAGE_SUBSCRIPTION_LISTENERS_IN_SERVE:-false}
LOG_DIR=${LOG_DIR:-/app/logs}
LOG_LEVEL=${LOG_LEVEL:-info}
LOG_ROTATION_MODE=${LOG_ROTATION_MODE:-size}
LOG_ROTATION_SIZE_MB=${LOG_ROTATION_SIZE_MB:-5}
LOG_ROTATION_TOTAL_SIZE_MB=${LOG_ROTATION_TOTAL_SIZE_MB:-100}
LOG_ROTATION_MAX_AGE_DAYS=${LOG_ROTATION_MAX_AGE_DAYS:-7}
HTTP_ADDR=${HTTP_ADDR:-:8000}
EOF
  chmod 600 "$ENV_FILE"
  echo "[entrypoint] created runtime env file: $ENV_FILE"
fi

if [ "${1:-serve}" = "all" ]; then
  if [ "${TC_RUN_MIGRATIONS:-true}" != "false" ]; then
    echo "[entrypoint] running database migrations"
    /app/tradingcopilot migrate
  fi

  pids=""

  stop_children() {
    trap - INT TERM
    if [ -n "$pids" ]; then
      echo "[entrypoint] stopping child processes"
      kill -TERM $pids 2>/dev/null || true
      wait $pids 2>/dev/null || true
    fi
  }

  trap stop_children INT TERM

  start_process() {
    name="$1"
    shift
    echo "[entrypoint] starting $name: /app/tradingcopilot $*"
    /app/tradingcopilot "$@" &
    pids="$pids $!"
  }

  start_process serve serve
  start_process worker worker
  start_process scheduler scheduler

  if [ "${TC_RUN_MESSAGE_SUBSCRIPTION_LISTENER:-true}" != "false" ]; then
    start_process message-subscription-listener message-subscription-listener
  fi

  set +e
  wait -n $pids
  status=$?
  set -e
  echo "[entrypoint] a child process exited with status $status"
  stop_children
  exit "$status"
fi

exec /app/tradingcopilot "$@"
