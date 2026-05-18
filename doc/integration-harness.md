# 集成测试工具

TradingCopilot 在 `internal/integration` 下包含可选集成测试。正常执行 `go test ./...` 时这些测试会被跳过，只有设置匹配的 `TC_INTEGRATION_*` 环境变量时才会运行。

不要提交真实 API key、token、Telegram hash、session 载荷、数据库 dump 或本地 `.env` 文件。凭据应通过 shell 环境、本地已忽略的 env 文件，或部署环境的密钥存储提供。

## 本地测试服务

当集成测试需要本地 PostgreSQL/TimescaleDB 和 Redis 服务时，使用仓库根目录的集成 compose 文件：

```bash
docker compose -f docker-compose.integration.yml up -d
docker compose -f docker-compose.integration.yml ps
```

该 compose 文件把服务绑定到 localhost：

- TimescaleDB: `127.0.0.1:15432 -> 5432`
- Redis: `127.0.0.1:16379 -> 6379`
- PostgreSQL database: `tradingcopilot_test`
- PostgreSQL user/password: `tc_test` / `tc_test_password_change_me`
- Redis password: `tc_redis_password_change_me`

运行数据库或 Redis 集成检查前，先设置连接 URL：

```bash
export TC_DATABASE_URL="postgresql://tc_test:tc_test_password_change_me@127.0.0.1:15432/tradingcopilot_test"
export TC_REDIS_URL="redis://:tc_redis_password_change_me@127.0.0.1:16379/0"
```

PowerShell:

```powershell
$env:TC_DATABASE_URL="postgresql://tc_test:tc_test_password_change_me@127.0.0.1:15432/tradingcopilot_test"
$env:TC_REDIS_URL="redis://:tc_redis_password_change_me@127.0.0.1:16379/0"
```

## Redis / asynq

```bash
TC_INTEGRATION_REDIS=1 go test ./internal/integration -run TestRedisAsynqIntegrationHarness -v -count=1
```

该检查会执行 Redis `PING`，创建一个带唯一 ID 的 asynq 任务，并从默认队列中删除它。

如需同时启动短生命周期 worker 并验证消费者级处理：

```bash
TC_INTEGRATION_REDIS=1 TC_REDIS_CONSUMER_E2E=1 go test ./internal/integration -run TestRedisAsynqIntegrationHarness -v -count=1
```

## PostgreSQL / TimescaleDB

```bash
TC_INTEGRATION_POSTGRES=1 TC_REQUIRE_TIMESCALE=1 go test ./internal/integration -run TestPostgresTimescaleSchemaInitHarness -v -count=1
```

该检查会执行数据库自动迁移和 Timescale 设置。`TC_REQUIRE_TIMESCALE=1` 会额外验证 Timescale hypertable 可见。

## 行情提供方

```bash
TC_INTEGRATION_MARKET=1 TC_MARKET_PROVIDER=adata TC_MARKET_CODE=600519 go test ./internal/integration -run TestMarketProviderIntegrationHarness -v -count=1
```

默认免费行情数据路径使用 Go 原生的 `github.com/onepiecelover/adata-go` SDK。提供 token 后也可以测试 Tushare：

```bash
TC_INTEGRATION_MARKET=1 \
TC_MARKET_PROVIDER=tushare \
TC_TUSHARE_MODE=daily \
TC_TUSHARE_TOKEN="<your-token>" \
go test ./internal/integration -run TestMarketProviderIntegrationHarness -v -count=1
```

部分 Tushare 接口需要额外账户权限，并可能触发速率限制。只有在你明确希望把权限拒绝或限流响应记录为访问边界诊断，并且受支持路径已经通过时，才使用 `TC_TUSHARE_ALLOW_ACCESS_BOUNDARY=1`。

## AI 提供方

```bash
TC_INTEGRATION_AI=1 \
TC_AI_BASE_URL="<openai-compatible-base-url>" \
TC_AI_API_KEY="<api-key>" \
TC_AI_MODEL="<model>" \
go test ./internal/integration -run TestAIProviderIntegrationHarness -v -count=1
```

该检查会发送一个非流式的 OpenAI 兼容 chat 请求，并验证响应解析、重试行为、模型选择，以及会议流程所需的 JSON 输出可用性。

## Telegram MTProto

使用 CLI 两步登录流程，将 Telegram app 凭据和 MTProto session 存入已配置的应用数据库：

```bash
go run ./cmd/tradingcopilot message-subscription-login-start -app-id "<app_id>" -app-hash "<app_hash>" -phone "<phone>"
go run ./cmd/tradingcopilot message-subscription-login-verify -code "<telegram_code>"
```

如果 Telegram 账户启用了两步验证，在 verify 命令中增加 `-password "<2fa_password>"`。

登录后测试一个频道引用：

```bash
go run ./cmd/tradingcopilot message-subscription-mtproto-test -channel "@public_channel"
```

针对已存储在配置数据库中的 session 运行集成检查：

```bash
TC_INTEGRATION_TELEGRAM=1 \
TC_TELEGRAM_USE_DATABASE=1 \
TC_TELEGRAM_CHANNEL_REF="@public_channel" \
go test ./internal/integration -run TestTelegramMTProtoIntegrationHarness -v -count=1
```

也可以直接通过环境变量提供凭据：

```bash
TC_INTEGRATION_TELEGRAM=1 \
TC_TELEGRAM_APP_ID="<app_id>" \
TC_TELEGRAM_APP_HASH="<app_hash>" \
TC_TELEGRAM_SESSION="<gotd-session-payload>" \
TC_TELEGRAM_CHANNEL_REF="@public_channel" \
go test ./internal/integration -run TestTelegramMTProtoIntegrationHarness -v -count=1
```

Telegram 私有频道或数字频道引用要求该账户拥有访问对应 peer 的权限。如果 Telegram 没有为未知私有频道暴露 access hash，集成检查会失败并返回可执行的错误信息。
