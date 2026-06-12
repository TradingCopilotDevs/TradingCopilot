# TradingCopilot

![TradingCopilot logo](docs/assets/tradingcopilot-logo.png)

TradingCopilot 是一个自托管的 AI 投研会议与 A 股模拟交易系统。它把消息订阅、市场数据、AI 多角色讨论、唤醒计划和模拟盘执行整合到一个可部署的 Web 控制台中，适合个人或小团队搭建自己的投研自动化工作台。

后端由单个 Go 二进制提供 API、任务队列、调度器和运行时进程，前端使用 Vue 3 与 Element Plus，接口契约由 `api/openapi.yaml` 维护。

> 本项目不提供投资建议。所有市场分析、AI 输出和模拟交易结果仅用于研究、测试和学习，真实投资风险由使用者自行承担。

## 功能特性

- **AI 投研会议**：围绕主题启动多角色讨论，支持事件流、上下文、工具调用、复盘和会议重启。
- **消息订阅与过滤**：支持 Telegram 与 RSS/Atom 来源，使用 AI 对消息做忽略、观察、触发会议等分诊。
- **A 股行情与自选**：集成 A 股代码识别、实时行情、历史数据和自选列表。
- **模拟交易系统**：支持账户、风控配置、订单审批、成交量约束的部分成交、成交量参与率价格冲击、剩余撤单、持仓、权益快照、公司行为、复盘时间线和交易时段处理。
- **唤醒计划**：按价格、涨跌幅、行情条件或消息事件触发后续投研。
- **运行时配置**：在界面中管理模型提供商、密钥、代理、角色能力和系统设置。
- **运维与恢复**：提供 provider health、队列治理、Prometheus 指标、本地/S3/OSS 备份归档 provider、恢复 dry-run 和保留策略。
- **一体化部署**：Docker Compose 启动 PostgreSQL/TimescaleDB、Redis 和一个 TradingCopilot 应用容器。

## 快速开始

推荐使用 Docker Compose 部署。首次启动会自动创建 `./env/app.env`，并生成随机 `APP_SECRET_KEY` 与 `JWT_SECRET_KEY`，不需要手动复制 `.env.example`。

```bash
docker compose up -d
```

启动后访问：

```text
http://localhost:8000
```

查看服务状态：

```bash
docker compose ps
docker compose logs -f app
```

停止服务：

```bash
docker compose down
```

持久化数据默认保存在这些目录中：

- `env/`：运行时环境配置，包含首次自动生成的 `app.env`
- `data/`：应用本地数据
- `logs/`：应用日志
- `postgres-data/`：PostgreSQL/TimescaleDB 数据
- `redis-data/`：Redis 数据

这些目录已在 `.gitignore` 中排除，不应提交到仓库。

## Docker 配置

Compose 默认启动 3 个服务：

- `postgres`：TimescaleDB/PostgreSQL
- `redis`：后台任务队列
- `app`：TradingCopilot 应用容器

`app` 容器默认执行 `all` 命令：先运行数据库迁移，然后在同一个容器内启动 `serve`、`worker`、`scheduler` 和 `message-subscription-listener` 多个进程。任一子进程退出时，容器会停止其余进程并退出，交给 Docker 重启策略处理。

常用环境变量：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `TC_IMAGE_REPOSITORY` | `ghcr.io/tradingcopilotdevs/tradingcopilot` | 应用镜像仓库 |
| `TC_IMAGE_TAG` | `latest` | 应用镜像标签 |
| `PUBLIC_BASE_URL` | `http://localhost:8000` | 对外访问地址 |
| `DATABASE_URL` | `postgresql://tradingcopilot:tradingcopilot@postgres:5432/tradingcopilot` | 应用数据库连接 |
| `REDIS_URL` | `redis://redis:6379/0` | Redis 连接 |
| `MEETING_DISPATCH_MODE` | `redis` | 会议任务分发模式 |
| `MESSAGE_TASK_QUEUE_MODE` | `redis` | 消息过滤任务队列模式 |
| `BACKUP_ARCHIVE_PROVIDER` | `local` | 备份归档 provider，可选 `local`、`s3` 或 `oss`；S3/OSS 配置完整后会真实写入对应对象存储 |
| `BACKUP_ARCHIVE_S3_*` | 空 | S3 归档配置：bucket、region、可选 endpoint、access key、secret key 和 prefix |
| `BACKUP_ARCHIVE_OSS_*` | 空 | OSS 归档配置：bucket、region、endpoint、access key、secret key 和 prefix |
| `BACKUP_RETENTION_COPIES` | `7` | 至少保留的最新备份归档数量 |
| `BACKUP_RETENTION_DAYS` | `30` | 备份归档和恢复演练沙箱至少保留的天数 |
| `BACKUP_RESTORE_DRILL_INTERVAL_HOURS` | `168` | Redis scheduler 周期性恢复 dry-run 间隔小时数，`0` 表示禁用 |
| `TC_RUN_MIGRATIONS` | `true` | 启动时是否执行数据库迁移 |
| `TC_RUN_MESSAGE_SUBSCRIPTION_LISTENER` | `true` | 是否启动消息订阅监听进程 |

示例：跳过启动迁移。

```bash
TC_RUN_MIGRATIONS=false docker compose up -d app
```

如果你使用 PowerShell：

```powershell
$env:TC_RUN_MIGRATIONS="false"
docker compose up -d app
```

## 本地开发

本地直接运行 Go 后端时，可以使用本地开发示例配置：

```powershell
Copy-Item .env.local.example .env
go run ./cmd/tradingcopilot serve
```

后端常用命令：

```bash
go test ./...
go vet ./...
go generate ./...
golangci-lint run
```

前端开发：

```bash
cd frontend
npm ci
npm run dev
```

前端构建与 API 类型生成：

```bash
cd frontend
npm run typegen:api
npm run build
```

## 运行命令

Go 二进制提供以下运行模式：

```bash
go run ./cmd/tradingcopilot serve
go run ./cmd/tradingcopilot worker
go run ./cmd/tradingcopilot scheduler
go run ./cmd/tradingcopilot message-subscription-listener
go run ./cmd/tradingcopilot migrate
```

Telegram MTProto 配置与诊断命令：

```bash
go run ./cmd/tradingcopilot message-subscription-login
go run ./cmd/tradingcopilot message-subscription-login-start
go run ./cmd/tradingcopilot message-subscription-login-verify
go run ./cmd/tradingcopilot message-subscription-mtproto-test
```

## 配置与密钥

不要把真实 `.env`、数据库、日志、Telegram session、API key 或代理凭据提交到仓库。仓库只保留示例配置：

- `.env.example`：生产部署参考配置
- `.env.local.example`：本地开发参考配置

Docker 部署时，首次启动自动生成的 `./env/app.env` 会被复用。生产环境中建议在首次启动后检查并按需修改：

- `PUBLIC_BASE_URL`
- `CORS_ORIGINS`
- `POSTGRES_PASSWORD`
- `DATABASE_URL`
- `REDIS_URL`
- AI 模型提供商配置
- Telegram MTProto 或 Bot 配置

业务密钥会通过应用的密钥管理能力加密保存；请妥善保管 `APP_SECRET_KEY`，丢失后已加密的密钥将无法解密。

## 项目结构

```text
api/                 OpenAPI 契约
cmd/tradingcopilot/ Go CLI 入口
docker/              容器入口脚本
doc/                 架构、契约、测试和集成说明
frontend/            Vue 3 前端
internal/app/        应用用例
internal/domain/     领域模型
internal/infra/      数据库、队列、行情、AI、Telegram 等基础设施
internal/transport/  HTTP API 与前端静态资源服务
```

## 文档

- `doc/api-contract.md`：HTTP API 与 JSON:API 契约说明
- `doc/database-contract.md`：数据库表与持久化约束
- `doc/prediction-market-prd.md`：预测市场（Polymarket v1）需求与验收说明
- `doc/integration-harness.md`：集成测试环境说明
- `doc/upgrade-migration.md`：自托管升级、迁移、回滚和验收说明
- `doc/test-matrix.md`：测试矩阵
- `doc/adr/`：架构决策记录

## 贡献

欢迎提交 Issue 和 Pull Request。建议在提交前运行：

```bash
go test ./...
go vet ./...
cd frontend && npm run build
```

如果修改了 OpenAPI 契约，请同步运行前端类型生成：

```bash
cd frontend
npm run typegen:api
```

## 许可证

本项目基于 Apache License 2.0 开源，详见 [LICENSE](LICENSE)。
