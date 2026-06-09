# 版本升级与迁移说明

本文档用于自托管部署的版本升级、数据库迁移和回滚演练。TradingCopilot 当前仍是模块化单体，OpenAPI 与 GORM schema 是升级兼容性的主要边界。

## 升级前检查

1. 在“运维状态 -> 备份与恢复”创建最新备份，并对最新备份执行 dry-run 沙箱恢复演练。
2. 确认 `GET /ops/backups` 中 `restoreDrill` 为 `drilled` 或已评估 `drill_warning` 的人工恢复影响。
3. Redis 调度模式下确认 `BACKUP_RESTORE_DRILL_INTERVAL_HOURS` 符合演练频率要求；默认 `168` 小时，`0` 表示禁用周期性恢复 dry-run。
3. 记录当前镜像标签、Git commit、`.env` 或 `env/app.env`、数据库后端和 Redis 地址。
4. 运行当前版本 smoke test：`GET /health`、登录、仪表盘、设置向导 readiness、创建一条测试会议或查看最近会议。

## Docker Compose 升级

1. 修改 `TC_IMAGE_TAG` 或拉取新的 compose 文件。
2. 预览配置：

```bash
docker compose config
```

3. 拉取镜像并重启应用：

```bash
docker compose pull app
docker compose up -d app
```

4. 默认启动流程会先运行数据库迁移，再启动 `serve`、`worker`、`scheduler` 和 `message-subscription-listener`。如需手动控制迁移，可临时设置：

```bash
TC_RUN_MIGRATIONS=false docker compose up -d app
```

## 本地二进制升级

1. 停止正在运行的 `serve`、`worker`、`scheduler` 和消息监听进程。
2. 备份数据库、`.env`、日志目录和 Telegram session。
3. 构建或替换二进制。
4. 运行迁移：

```bash
go run ./cmd/tradingcopilot migrate
```

5. 启动服务：

```bash
go run ./cmd/tradingcopilot all
```

## 迁移约束

- 数据库迁移必须保持幂等；重复执行不得破坏已有数据。
- 新增字段优先提供默认值或允许空值，再由应用层逐步回填。
- 本轮支持从上一公开版本直接升级，不需要重建数据库；新增 `auth_sessions`、`audit_events`、`paper_corporate_actions` 表和管理员/消息反馈相关列均由启动迁移自动补齐。
- OpenAPI 是 HTTP 契约事实源；升级涉及 API 形状时必须同步 `api/openapi.yaml`、生成代码、前端类型和 `doc/api-contract.md`。
- GORM 模型表名、索引和 Timescale 初始化 SQL 变更必须同步 `doc/database-contract.md` 和 schema 测试。
- 队列任务类型变更必须兼容已入队 payload，或提供明确的废弃/重试/归档处理说明。

## 回滚流程

1. 停止新版本应用。
2. 如果迁移只新增兼容字段，可直接切回旧镜像或旧二进制。
3. 如果迁移包含破坏性数据变更，先从最近备份恢复数据库，再切回旧版本。
4. 恢复后运行：

```bash
go test ./internal/architecture
```

并执行登录、仪表盘、会议详情、模拟盘账户和消息订阅 smoke test。

## 升级后验收

- `go test ./...`
- `go vet ./...`
- `go generate ./...` 后确认生成文件无意外漂移。
- `npm ci --prefix frontend`
- `npm run build --prefix frontend`
- `docker compose config`
- `GET /setup/readiness`
- `GET /ops/provider-health`
- `GET /ops/jobs`
- `GET /ops/backups`
- 对最新备份执行 `POST /ops/backups/{backupName}/restore-dry-run`

## 版本记录模板

每次发布建议记录：

- 版本号和 Git commit。
- 数据库迁移摘要。
- API 契约变化。
- 新增环境变量和默认值。
- 需要人工执行的迁移或回填任务。
- 回滚注意事项。
- 已通过的本地和外部集成测试。
