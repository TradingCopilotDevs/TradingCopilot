# 测试矩阵

当前本地验证：

- 2026-05-11：`go test ./...` 已在此工作区通过。

外部集成验证方式记录在 `doc/integration-harness.md`。带日期的外部结果只是当时快照，不保证相同服务当前仍然可访问。

## 必需本地检查

| 范围 | 必需覆盖 | 主要检查 |
| --- | --- | --- |
| 架构边界 | `domain`/`app`/`transport` 导入边界、GORM 持久化边界、与提供方无关的消息运行时位置、OpenAPI 包装路由、仅 JSON:API 请求解码、camelCase API 参数、模拟交易风险配置绑定契约、日志契约、被忽略的本地路径，以及已移除的旧包 | `go test ./internal/architecture` |
| 完整 Go 套件 | 单元测试、跳过集成测试后的套件、`transport`、`persistence`、`runtime` 和 `architecture` 包 | `go test ./...` |
| API 契约 | 生成的 OpenAPI 服务端接口、路由挂载、JSON:API 响应和错误形状、受保护路由未授权行为、公开路由冒烟覆盖，以及消息领域路由重点覆盖 | `go test ./internal/transport/http ./internal/transport/http/jsonapi` |
| 数据库契约 | 30 张表表面、GORM 模型表名、Timescale 设置顺序、JSON 默认载荷、关键唯一约束，以及关键外键关系 | `go test ./internal/infra/persistence/gorm/connect ./internal/infra/persistence/gorm/repo` |
| 前端契约 | 从 OpenAPI 生成的 TypeScript API 类型和 Vue 生产构建 | `npm run typegen:api --prefix frontend`; `npm run build --prefix frontend` |
| 静态部署配置 | Compose 语法和服务图；验证环境需要安装 Docker | `docker compose config` |

## 领域覆盖

| 领域 | 必需场景 | 主要检查 |
| --- | --- | --- |
| 认证和安全 | 管理员初始化/登录、JWT 验证、密码哈希、兼容 Fernet 的密钥加密，以及受保护路由拒绝未授权请求 | `go test ./internal/app/auth ./internal/infra/security ./internal/transport/http` |
| 设置 | 运行时环境变量列表、应用设置覆盖、可编辑 `.env` 回写、密钥定义、加密密钥写入/列表，以及代理设置和测试诊断 | `go test ./internal/app/settings ./internal/infra/config ./internal/transport/http` |
| 日志 | 结构化 JSONL 日志、必需的 `event`/`group`/`method`/`status`/可空 `durationMs` 字段、`not_applicable` 填充行为、按大小/时间轮转设置、日志查询 API、HTTP/GORM/后台诊断、脱敏，以及前端日志查看器契约 | `go test ./internal/infra/logging ./internal/transport/http ./internal/architecture` |
| AI | 提供方增删改查、加密 API key 使用、OpenAI 兼容客户端解析、SSE/非 SSE 用量解析、重试分类、模型同步、角色增删改查、默认角色能力应用，以及 tokenizer fallback 计数 | `go test ./internal/infra/ai/client ./internal/app/ai ./internal/infra/persistence/gorm/meeting` |
| 研究团队 | 团队增删改查、一个模拟交易账户只能绑定一个团队、启动时默认团队种子、每团队会议角色、默认角色破坏性重置、消息订阅团队绑定，以及按团队限定的会议/自选列表/唤醒计划 | `go test ./internal/app/research ./internal/transport/http ./internal/architecture` |
| 行情 | A 股代码规范化、`adata-go` 映射、Tushare 日线解析/刷新、五日兼容路径、实时缓存 TTL、自选列表报价刷新、陈旧缓存兜底、已废弃提供方错误、标的/自选列表增删改查，以及工具查询名称 | `go test ./internal/app/market ./internal/infra/persistence/gorm/marketdata` |
| Telegram | App/bot 配置、频道/消息增删改查、引用规范化、AI 过滤、MTProto 登录/session/代理处理、公开/私有采集和回填行为、Bot 发送/轮询命令，以及会议引用创建/删除 | `go test ./internal/app/telegram ./internal/infra/persistence/gorm/telegram` |
| 消息 | 与提供方无关的 Telegram 和 RSS/Atom 订阅、可复用消息过滤器及每订阅绑定/默认选择、字符串来源消息身份、通过 gofeed 解析 RSS/Atom、每来源轮询状态、异步摄取消息过滤状态和队列处理、平台适配器增删改查/密钥处理、监听器标题/消息摄取、运行时适配器与 GORM 持久化分离，以及应用层拥有过滤/会议写入的事务边界 | `go test ./internal/app/messaging ./internal/infra/messaging ./internal/infra/queue ./internal/infra/persistence/gorm/repo ./internal/infra/persistence/gorm/runtime ./internal/transport/http` |
| 会议 | 增删改查、列表过滤、引用、SSE 流、取消/删除/重启语义、回顾、本地取消、托管运行器的规划/工具/JSON 修复/回顾路径、提示词和错误 golden 片段、token 预算限制，以及 Telegram 完成通知 | `go test ./internal/app/meeting ./internal/infra/persistence/gorm/meeting ./internal/transport/http` |
| 唤醒计划 | 增删改查和状态转换、到期计划处理、来源会议上下文、复合指标语法、Telegram 事件匹配、条件未满足时重新调度、旧事件抑制、worker 分发回调，以及可取消循环 | `go test ./internal/app/wake ./internal/infra/persistence/gorm/meeting ./internal/transport/http` |
| 模拟交易 | 风险配置默认值、账户绑定/解绑、未配置账户拒绝、板块校验、ETF/LOF 处理、仓位规模、费用/成交/盈亏、订单生命周期、定时维护、绩效回撤/胜率边界，以及 API 增删改查/错误路径 | `go test ./internal/app/paper ./internal/infra/persistence/gorm/paper ./internal/transport/http` |
| 仪表盘和运行时 | 数据库/Redis/提供方诊断、worker/scheduler/paper/Telegram 心跳状态、业务指标、逾期唤醒警报、新闻过滤就绪状态、本地后台循环、worker mux 处理，以及陈旧会议恢复 | `go test ./internal/infra/persistence/gorm/dashboard ./internal/infra/persistence/gorm/runtime ./internal/infra/persistence/gorm/queue` |

## 日志语义

- 预期的空操作、保护分支或重复抑制路径必须使用 `level=info` 和 `status=skipped`；`level=error` 只用于需要兜底、重试、操作员处理或调用方可见错误处理的失败。

## 外部集成检查

这些检查是可选的，因为它们需要外部服务、凭据或部署网络路径。

| 检查 | 何时需要 | 命令 |
| --- | --- | --- |
| Redis/asynq | 验证队列连通性、worker 消费或 scheduler 入队语义 | `TC_INTEGRATION_REDIS=1 go test ./internal/integration -run TestRedisAsynqIntegrationHarness -v -count=1` |
| PostgreSQL/TimescaleDB | 验证生产 schema 初始化或 Timescale hypertable | `TC_INTEGRATION_POSTGRES=1 TC_REQUIRE_TIMESCALE=1 go test ./internal/integration -run TestPostgresTimescaleSchemaInitHarness -v -count=1` |
| 行情提供方 | 验证部署网络访问实时行情提供方的出口能力 | `TC_INTEGRATION_MARKET=1 go test ./internal/integration -run TestMarketProviderIntegrationHarness -v -count=1` |
| AI 提供方 | 验证 OpenAI 兼容提供方/模型/API key 组合 | `TC_INTEGRATION_AI=1 go test ./internal/integration -run TestAIProviderIntegrationHarness -v -count=1` |
| Telegram MTProto | 验证 app 凭据、已存 session、频道访问和最新消息拉取 | `TC_INTEGRATION_TELEGRAM=1 go test ./internal/integration -run TestTelegramMTProtoIntegrationHarness -v -count=1` |

## 维护规则

- 每次行为变更影响 API 形状、持久化形状、后台处理或外部集成边界时，都要新增或更新测试。
- Redis/asynq 周期任务必须按 task type 合并，并使用稳定的 `periodic:<task_type>` ID。不要创建按窗口变化的周期任务 ID，也不要附加可能把 scheduler 停机转换为大量失败积压任务的过期或截止时间选项。
- 消息摄取和回填路径不得同步调用 AI 过滤；它们必须先持久化消息和过滤状态，再把消息过滤工作加入队列。
- 保持 `api/openapi.yaml`、生成的 Go 服务端类型、生成的前端类型和 `doc/api-contract.md` 对齐。
- 保持 `doc/database-contract.md` 与 GORM 模型表名、Timescale 设置 SQL 和 schema 初始化测试对齐。
- 在自己的环境中依赖提供方、网络或凭据路径前，重新运行相关集成检查。
