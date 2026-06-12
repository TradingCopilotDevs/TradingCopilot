# API 契约

> 2026-06-09 更新：`GET /meetings/{meetingId}` 的 `trustReport` 现在额外暴露 `recapActionReviewStatus`、`recapActionSuggestionCount`、`recapActionSuggestions`、`recapActionReviewCount` 和 `recapActionReviews`。当主持人 recap 的可执行动作因证据缺失或弱引用被阻断时，详情页可以展示待复核动作、原始 spec、处置状态、原因、证据摘要和最近人工判定；列表接口仍不返回该重对象。

所有路由都以 `/api` 为根路径。除健康检查、是否需要初始化、登录和初始化接口外，受保护路由都需要 `Authorization: Bearer <jwt>`。

`api/openapi.yaml` 是当前 HTTP 契约的事实来源。下面的路由列表是面向前端 API 表面的人工索引；如果本文档与 `api/openapi.yaml` 冲突，应更新本文档以匹配 OpenAPI。

- `data.type` 使用 kebab-case 的复数资源名。
- `id` 始终是字符串。
- 业务字段放在 `attributes` 下，并使用 camelCase。
- 关联关系放在 `relationships` 下。
- 错误使用 `errors: [{ code, message, detail?, field? }]`。
- 列表使用 `page[limit]` 和 `page[cursor]`，并返回 `links.next`、`meta.pageSize`、`meta.nextCursor` 和 `meta.hasMore`。游标是不透明的 API 值（Cursors are opaque）；客户端必须原样透传，不得解析、递增或自行构造。
- 消息摄取 API 使用与提供方无关的资源类型：`message-subscriptions`、`message-subscription-diagnostics`、`message-subscription-filters`、`message-subscription-app-configs`、`platform-adapters` 和 `ingested-messages`。Telegram 和 RSS/Atom 仍然是属性或 provider 值，不是这些路由的 JSON:API 资源类型。
- 消息订阅支持 `telegram_channel` 和 `rss_feed` 提供方。所有提供方的 `sourceMessageId` 都是字符串；Telegram 数字消息 ID 序列化为十进制字符串，RSS/Atom 条目 ID 使用 GUID、链接或哈希身份。订阅暴露 `pollIntervalSeconds`、`lastCollectedAt`、`nextCollectAt` 和 `lastCollectError`，用于表示与提供方无关的采集状态。`GET /message-subscriptions/diagnostics` 返回每个订阅源的 `sourceKind`、`proxyRoute`、`status`、`ready`、`privateCapable`、`checks` 和 `recommendedActions`，用于诊断 Telegram 私有源、RSS URL 凭据、RSS Basic/Bearer feed authentication、过滤器 AI provider、团队绑定、代理路由和最近采集错误；`audit_telegram_access` 维护动作会复用订阅测试能力巡检 Telegram 源访问权限，失败写入 `lastCollectError`，成功清理既有错误。
- 消息订阅过滤器是由消息订阅域拥有的可复用配置。每个订阅只绑定一个过滤器；多个订阅可以共享一个过滤器，默认过滤器只用于新建订阅时的预选。启动种子数据只会在没有任何过滤器记录时创建内置默认过滤器。
- 摄取消息通过 `filterStatus` 暴露 `unfiltered`、`filtering`、`filtered` 或 `failed` 状态。采集和重新过滤 API 会把工作加入队列，并在任务被接受后返回；AI 过滤和会议创建通过消息任务队列异步执行。
- 摄取消息可通过 `POST /ingested-messages/{messageId}/feedback` 记录单条人工反馈，也可通过 `POST /ingested-messages/feedback/batch` 对最多 500 条消息批量记录人工反馈；标签为 `helpful`、`noise`、`misclassified` 或 `neutral`。消息资源暴露 `feedbackLabel`、`feedbackComment` 和 `feedbackAt`。`GET /message-feedback/training-samples` 导出可用于过滤器训练/评估的人工反馈样本，`GET /message-feedback/evaluation` 返回当前反馈集的离线评估指标，`GET/POST /message-feedback/training-snapshots` 列出或创建带版本、指纹、样本 manifest、来源可信摘要和评估摘要的训练集快照；`GET/POST /message-feedback/training-exports` 列出或创建 JSONL 训练集导出归档，列表只返回版本、样本数、大小和哈希等元数据，创建和 `GET /message-feedback/training-exports/{exportVersion}` 返回完整 `content`、`contentSha256`、`byteCount` 与 `lineCount`；`GET /message-feedback/source-trust` 返回按来源聚合的可信评分、状态、解释、治理动作、`autoAction` 和 `autoActionReason`，`POST /message-feedback/source-trust/recompute` 以幂等方式重算当前报告；`POST /message-subscriptions/maintenance` 的 `apply_source_trust_governance` 会把严重低可信来源暂停采集，把中等风险来源写入治理观察状态，并在订阅 `config.sourceTrustGovernance` 与 `lastCollectError` 中保留原因；provider health 仍会汇总来源可信概览。
- 消息过滤默认启用来源可信自动降权策略：同一订阅源至少 3 条反馈后，低可信来源会把模型给出的 `meeting` 降为 `observe`，极低可信或严重负反馈来源会把模型给出的 `observe` 降为 `ignore`；降权会追加到消息 `filterReason`，避免静默改变模型输出。可通过 `MESSAGE_SOURCE_TRUST_POLICY` app setting 覆盖策略，支持 `enabled`、`minFeedback`、`meetingToObserveMaxScore`、`observeToIgnoreMaxScore`、`moderateNegativeRate` 和 `severeNegativeRate`。
- 研究团队 API 使用 `research-teams` 和 `research-team-roles`。会议、自选列表、唤醒计划、摄取消息过滤器和消息订阅使用显式的 `researchTeamId` 或 `teamIds` 字段，而不是隐式全局团队。`research-teams` 额外暴露 `assetClass`，取值为 `a_share`、`prediction_market` 或 `mixed`；`prediction_market` 团队允许 `paperAccountId=null`，`a_share` 和 `mixed` 仍要求有效模拟盘账户。
- 预测市场 API 使用独立资源类型：`prediction-events`、`prediction-markets`、`prediction-market-quotes`、`prediction-market-matches` 和 `prediction-watchlist-items`。v1 仅接入 Polymarket 公共数据，不保存钱包、API key、订单、仓位、充值或提现信息；预测市场不会写入 A 股 `market_symbols`。Polymarket Gamma/CLOB 请求复用 Market 代理模块，前端与设置向导把该代理语义展示为“行情/预测市场”。`GET /prediction-markets/search` 使用 Gamma 搜索并持久化候选，`POST /prediction-markets/sync` 同步活跃市场，`GET /prediction-matches` 和 `POST /prediction-matches/{matchId}/review` 用于新闻到预测市场匹配治理；`POST /prediction-watchlist` 仅允许 `assetClass=prediction_market|mixed` 的团队关注已存在预测市场。摄取消息额外暴露只读 `relatedPredictionMarkets` 和 `predictionMarketMatchStatus`；会议创建可传 `predictionMarketIds`，会议工具可通过 `prediction.search_markets`、`prediction.market_snapshot`、`prediction.orderbook`、`prediction.price_history` 和 `prediction.related_matches` 拉取证据。当前会议实时价格接入优先使用 Polymarket Market WebSocket 短采样并写入 tool result，失败时降级 CLOB REST 盘口；WebSocket runner 状态在 provider health 中明示。
- 行情运行时设置暴露一个历史数据提供方、一个实时数据提供方、`MARKET_REALTIME_COMPAT_PROVIDER` 和实时缓存 TTL。`MARKET_REALTIME_COMPAT_PROVIDER` 是一个范围很窄的实时兼容来源，用于 ETF/LOF 或主数据源缺失报价（`tencent`、`sina` 或 `disabled`），不是通用兜底提供方列表（not a generic fallback provider list）。实时兜底数据源列表不是当前契约的一部分。
- `MEETING_DAILY_TOKEN_BUDGET` 使用 `-1` 表示会议 token 使用量不受限制（`-1` for unlimited）；正整数表示每日上限，`0` 或小于 `-1` 的值无效（`0` or values below `-1` are invalid）。
- `AI_DAILY_COST_BUDGET` 使用 `-1` 表示 AI 成本不设每日预算，正数表示每日成本预算，`0` 或小于 `-1` 的值无效。`AI_COST_RATES` 存储在 `app_settings`，值为 `{ currency, rates }`；每条 rate 支持 `providerId`/`providerName`、`model`、`inputPerMillion`/`outputPerMillion` 或 `totalPerMillion`，并兼容 snake_case 和 prompt/completion 命名。
- 日志是基于文件的 JSON:API 资源。日志条目暴露 UTC 时间戳，以及必需的通用字段 `event`、`group`、`method`、`status` 和可空的 `durationMs`；不适用的字符串字段使用 `not_applicable`。日志配置通过运行时环境设置编辑，并在重启后生效。`GET /logs` 默认隐藏噪声较大的静态资源、前端和日志查看器请求；传入 `includeNoise=true` 可返回这些记录。
- 运行时环境设置页可维护 `BACKUP_ARCHIVE_PROVIDER`、S3/OSS bucket、region、endpoint 和 prefix 等归档配置变量；`BACKUP_ARCHIVE_PROVIDER=s3|oss` 且对应 bucket/region/access key/secret key 等必需字段完整时，备份归档会真实写入 S3/S3-compatible endpoint 或阿里云 OSS。S3/OSS access key 与 secret key 在 `GET /settings/runtime-env` 中只返回 `configured` 或空字符串，不回显已有明文。前端保存时凭据字段留空表示不更新当前 `.env` 值。
- 前端日志页支持从 URL query 初始化筛选条件，包括 `level`、`event`、`group`、`method`、`path`、`status`、`role`、`file`、`q`、`includeNoise`、`slowOnly`、`from` 和 `to`；运维页近期错误表会通过这些参数跳转到对应日志筛选视图。
- 设置向导 API 使用 `setup-readinesses` 和 `setup-action-results`。`GET /setup/readiness` 汇总管理员、AI、团队、预测市场、模拟盘风控、行情、消息源、代理、日志和队列状态；`POST /setup/actions/{key}` 只执行幂等初始化动作，不替代各领域的完整配置页面。`ensure-prediction-market-defaults` 会幂等创建默认预测市场投研团队和默认预测市场消息过滤器，不会创建预测市场模拟盘。
- 认证仍使用 bearer token 和 `Authorization` 头。前端默认把当前会话 token 存在 `sessionStorage`，并在读取时迁移和清理旧版 `localStorage` key；`POST /auth/logout` 会撤销当前 `auth_sessions` 记录，之后同一 token 访问受保护资源应返回未授权。
- 受保护路由执行 HTTP RBAC：`owner` 和 `admin` 可访问全部受保护路由；`operator` 可读取非 `/admin/*`、非 `/audit-events` 路由，并可写入行情、消息订阅/过滤器、摄取消息、会议、唤醒计划、模拟盘和运维任务重试/单任务重跑；`operator` 不可执行 Telegram 登录、平台适配器、设置、AI provider/角色、研究团队、设置向导动作和备份写操作；`viewer` 可读取非 `/admin/*`、非 `/audit-events` 路由，并只允许自助 `POST /auth/logout`。
- 受保护的 `POST`、`PUT`、`PATCH` 和 `DELETE` 请求会自动写入 `audit-events`，记录 actor、方法、路径、资源、状态和来源；审计事件不得保存请求体，避免泄漏 API key、密码和其他 secret。
- `GET /ops/jobs` 在后台心跳之外返回 `queue` 诊断对象，包含 Redis/asynq 队列 totals、各队列 pending/active/retry/archived/failed 计数、延迟、内存估算、失败任务样本、积压风险和可执行动作；失败任务样本支持 `queue`、`type`、`state=retry|archived` 和 `failedLimit` 查询参数，`failedLimit` 最大裁剪为 50；同时返回 `recentErrors`，汇总近 24 小时 error 级别结构化日志样本。本地调度模式下队列对象状态为 `disabled`。
- `POST /ops/jobs/retry-failed` 将 retry 和 archived 状态的 asynq 任务重新提交处理，返回按队列统计的提交数量；本地模式或未配置 Redis 时返回冲突错误。该路由是受保护写操作，会被审计中间件记录。
- `POST /ops/jobs/{queue}/tasks/{taskId}/run` 将单个失败任务重新提交处理，返回任务 ID、队列、任务类型、原状态和提交时间；本地模式或未配置 Redis 时返回冲突错误。该路由是受保护写操作，会被审计中间件记录。
- `GET /ops/metrics` 返回受保护的 Prometheus text 指标，覆盖业务计数、AI model call/token/cost 用量、成本预算使用率、缺价格调用、会议排队/运行耗时、行情新鲜度、消息过滤失败、队列 totals、失败任务样本数、近 24 小时错误日志数和后台依赖状态；该路由不是 JSON:API 响应。
- HTTP 入口会提取 W3C `traceparent` 并创建 OpenTelemetry span；结构化 HTTP 请求日志会在 trace context 有效时暴露 `traceId` 和 `spanId`，便于从日志跳转到外部 tracing 后端。
- `GET /ops/backups` 返回备份目录、归档提供方、归档列表、最新备份、恢复演练状态和自动保留策略状态；`archiveProvider` 与 `archiveProviderDetail` 会显示当前实际归档 provider。默认 provider 为 `local_filesystem`；当 `BACKUP_ARCHIVE_PROVIDER=s3|oss` 且配置完整时，provider 为 `s3` 或 `oss`，list/write/read/delete 均走对应对象存储。`archiveProviderDetail` 额外暴露 `configuredProvider`、`configuredExternalProvider`、`externalReady`、`missingExternalConfig` 和 `setupHint`；若配置完整但当前运行时未注入外部 provider，会以 warning 降级并继续使用本地归档。`backupMetadataDir` 表示本地恢复演练记录和沙箱目录，S3/OSS 归档启用后该目录仍保留在本机。`retentionPolicyDetail` 暴露保留份数、保留天数和当前可清理数量，`retentionLastRun` 暴露最近一次创建备份后自动清理旧归档/旧沙箱目录的结果。
- `POST /ops/backups` 通过归档 provider 创建备份 zip 后会自动执行保留策略：至少保留最新 `BACKUP_RETENTION_COPIES` 份，且至少保留最近 `BACKUP_RETENTION_DAYS` 天内的归档；同时清理早于保留窗口的 `restore-drills/` 本地沙箱目录，结果通过 `archiveProvider`、`archiveProviderDetail` 和 `retentionRun` 返回。
- `POST /ops/backups/{backupName}/restore-dry-run` 通过归档 provider 解析备份 zip 并执行非破坏性沙箱恢复演练；S3/OSS provider 会先把远端对象下载到临时文件，再复用同一套 zip 校验和安全解压流程。演练会校验 manifest、条目清单、数据库/日志归档数量和人工恢复步骤，并把归档安全解压到 `backupMetadataDir/restore-drills/` 沙箱供人工检查；该动作不会覆盖运行数据库、日志或环境文件。成功执行后会在 `backupMetadataDir` 写入最近一次恢复演练记录，`GET /ops/backups` 通过 `restoreDrill` 和 `restoreDrillDetail.lastDryRun` 暴露演练状态。
- `GET /ops/provider-health` 的 AI 诊断暴露 provider 可用性、近 24 小时/累计 token 用量、provider/model 聚合、成本金额、成本来源、预算状态、预算使用率、启用模型价格覆盖率、缺价格模型样本、缺价格调用、估算调用和模型调用延迟均值/P95；meeting runtime 诊断暴露近 24 小时会议完成/失败/运行数量、排队等待、运行时长 P95 和事件类型计数；market/messaging 诊断暴露行情最新报价年龄、日线年龄、简易健康分、消息过滤状态分布、采集错误、到期订阅和去重策略说明；predictionMarket 诊断暴露 Polymarket Gamma/CLOB/Market WS endpoint、复用的 Market 代理模块、本地市场样本状态、Gamma/CLOB/WS 主动探测状态、WebSocket runner 状态、最近同步时间和 Cloudflare throttling/429/5xx 错误分类样本。
- `GET /meetings/{meetingId}` 在详情响应中返回可选 `trustReport`，从会议事件派生证据链、结构化引用、模型快照、提示词哈希快照、声明到证据/引用的绑定、证据门禁、结论历史、逐句结论变更摘要、结论句人工复核记录、recap action 人工复核记录、事实/假设/推断、证据缺口和缺口提示；列表接口不返回该重对象。`evidenceGate` 会对 `fact` 和 `inference` 类型声明执行报告级门禁，要求至少绑定一个结构化证据事件或引用，返回 `evidenceGateStatus`（`pass`、`warning`、`blocked`）、`unsupportedClaimCount` 和 `unsupportedClaims`；`assumption` 与 `evidence_gap` 不作为阻断项。托管会议主持人 recap 若包含 `watchlist_actions`、`wake_plans` 或 `orders`，运行时会要求响应同时包含 `facts`/`inferences` 和 `citations`，否则触发语义重试，要求补证据或移除可执行动作。提示词快照只暴露哈希、message count、role/provider/model/tool/skill 摘要和版本号，不返回完整 prompt 文本。结论 diff 会返回新增、移除、保留句子的计数和截断样例，用于审计结论如何演化。`POST /meetings/{meetingId}/trust-reviews` 接受 JSON:API `meeting-trust-reviews` 资源，使用 camelCase attributes：`sentence`、`verdict`、可选 `sentenceId`、`citationIds`、`evidenceEventIds` 和 `comment`；`verdict` 只能为 `confirmed`、`needs_evidence`、`rejected` 或 `superseded`，服务端会写入审计友好的会议系统事件。`POST /meetings/{meetingId}/recap-action-reviews` 接受 JSON:API `meeting-recap-action-reviews` 资源，使用 camelCase attributes：`suggestionId`、`decision`、可选 `sourceEventId`、`actionIndex`、`actionType`、`citationIds`、`evidenceEventIds`、`comment` 和 `confirm`；`decision` 只能为 `approved`、`needs_evidence`、`rejected` 或 `superseded`，其中 `approved` 必须带 `confirm=true`。该接口只记录人工复核结论和 `manual_execution_required` 等执行处置，不会绕过证据门禁自动创建自选、唤醒计划或模拟盘订单。
- 会议来源的模拟盘订单默认进入 `suggested` 审批队列。订单资源会暴露 `approvalRiskLevel`、`approvalRiskReasons`、`approvalRiskMetrics`、`approvalReviewRequired` 和 `approvalConfirmRequired`，从来源证据、账户状态、风控绑定、订单金额、现金占用、买后仓位与卖出持仓覆盖派生复核提示。`POST /paper/orders/{orderId}/approve` 接受 JSON:API `paper-order-approvals` 资源，attributes 可传 `confirmHighRisk`；高风险订单未显式确认时服务端会拒绝提交，确认后再进入既有风控和待执行流程。`POST /paper/orders/{orderId}/reject` 会以人工原因拒绝订单。
- 模拟盘自动执行会执行 A 股 T+1 可卖数量、涨跌停价格区间、零成交量疑似停牌和过期单检查；自动成交会应用基础滑点，并在存在当日实时成交量时按成交量参与率叠加价格冲击，`executionNote` 会记录滑点、价格冲击和撮合报告。待执行订单按 `executeAfter, id` 队列顺序处理；若存在当日实时成交量，维护批次会按默认 10% 参与率生成同标的共享成交量预算，预算不足时只成交可用数量，剩余继续保持 `pending`。`POST /paper/orders/{orderId}/fill` 支持可选 `quantity`，不传或传 `0` 表示成交剩余数量；部分成交后订单保持 `pending`，订单资源通过 `filledQuantity`、`remainingQuantity` 和 `partialFillCount` 暴露成交进度，剩余数量仍可撤单。
- 模拟盘公司行为支持 `cash_dividend`、`bonus_share` 和 `split`。现金分红按当前持仓数量增加账户现金；送股的 `shareRatio` 表示每股新增比例；拆股的 `shareRatio` 表示拆股后倍数，例如 `2` 表示 1 拆 2。`GET /paper/accounts/{accountId}/replay` 返回由订单、成交和公司行为组成的复盘时间线。

服务器发送事件（SSE）和静态前端资源保持各自的原生协议。

## 公开与认证

- `GET /health`
- `GET /auth/bootstrap-required`
- `POST /auth/bootstrap`
- `POST /auth/login`

## 受保护领域

- `POST /auth/logout`
- `GET /dashboard`
- `GET /setup/readiness`
- `POST /setup/actions/{key}`
- `GET /admin/users`
- `POST /admin/users`
- `PUT /admin/users/{userId}`
- `POST /admin/users/{userId}/password-reset`
- `GET /admin/sessions`
- `POST /admin/sessions/{sessionId}/revoke`
- `GET /audit-events`
- `GET /ops/provider-health`
- `GET /ops/jobs`
- `POST /ops/jobs/retry-failed`
- `GET /ops/metrics`
- `GET /ops/backups`
- `POST /ops/backups`
- `POST /ops/backups/{backupName}/restore-dry-run`
- Redis worker task `run_backup_restore_drill` performs the same non-destructive latest-backup restore dry-run on the scheduler interval configured by `BACKUP_RESTORE_DRILL_INTERVAL_HOURS`.
- `GET /logs`
- `GET /logs/files`
- `GET /settings/runtime-env`
- `GET /settings/secret-definitions`
- `GET /settings/secrets`
- `POST /settings/secrets`
- `GET /settings/app-settings`
- `PUT /settings/app-settings/{key}`
- `GET /settings/proxy`
- `PUT /settings/proxy`
- `POST /settings/proxy/test`
- `GET /ai/providers`
- `POST /ai/providers`
- `PUT /ai/providers/{providerId}`
- `GET /ai/providers/{providerId}/models`
- `POST /ai/providers/{providerId}/models/sync`
- `GET /ai/tool-definitions`
- `GET /ai/skill-definitions`
- `GET /ai/roles`
- `PUT /ai/roles/{roleKey}`
- `POST /ai/roles/apply-default-capabilities`
- `GET /research-teams`
- `POST /research-teams`
- `PUT /research-teams/{teamId}`
- `DELETE /research-teams/{teamId}`
- `GET /research-teams/{teamId}/roles`
- `POST /research-teams/{teamId}/roles/apply-defaults`
- `PUT /research-teams/{teamId}/roles/{roleKey}`
- `DELETE /research-teams/{teamId}/roles/{roleKey}`
- `GET /message-subscription-filters`
- `POST /message-subscription-filters`
- `PUT /message-subscription-filters/{filterId}`
- `GET /market/symbols`
- `POST /market/symbols`
- `PUT /market/symbols/{code}`
- `DELETE /market/symbols/{code}`
- `GET /market/quotes/{code}`
- `GET /market/series/{code}`
- `POST /market/symbols/sync`
- `POST /market/tasks`
- `GET /market/daily/{code}`
- `GET /market/watchlist`
- `POST /market/watchlist`
- `PUT /market/watchlist/{itemId}`
- `DELETE /market/watchlist/{itemId}`
- `POST /market/tools/query`
- `GET /prediction-markets/search`
- `POST /prediction-markets/sync`
- `GET /prediction-events/{eventId}`
- `GET /prediction-markets/{marketId}`
- `GET /prediction-matches`
- `POST /prediction-matches/{matchId}/review`
- `GET /prediction-watchlist`
- `POST /prediction-watchlist`

`POST /market/tasks` accepts JSON:API `market-tasks` resources with `action` = `sync_symbols`, `refresh_quote`, or `refresh_daily_bars`; Redis/asynq workers consume `market_run_task` and return provider errors to the queue so retry/archive handling and `/ops/jobs` failure samples can govern market data failures.

Tracing and queue propagation:

- `OTEL_TRACES_EXPORTER` defaults to `none`; set it to `otlp` to enable trace export.
- `OTEL_EXPORTER_OTLP_PROTOCOL` supports `grpc` and `http/protobuf`; `OTEL_EXPORTER_OTLP_ENDPOINT` points to the collector endpoint.
- `OTEL_TRACES_SAMPLER` supports `always_on`, `always_off`, `traceidratio`, `parentbased_traceidratio`, `parentbased_always_on`, and `parentbased_always_off`; `OTEL_TRACES_SAMPLER_ARG` is used for ratio samplers and must be between 0 and 1.
- Redis/asynq task payloads may be wrapped as `{ traceEnvelopeVersion, traceparent, tracestate, payload }`; consumers must continue to accept legacy unwrapped payloads.
- `GET /message-subscriptions/app-config`
- `POST /message-subscriptions/app-config`
- `GET /message-subscriptions`
- `GET /message-subscriptions/diagnostics`
- `POST /message-subscriptions`
- `PUT /message-subscriptions/{subscriptionId}`
- `DELETE /message-subscriptions/{subscriptionId}`
- `POST /message-subscriptions/test`
- `POST /message-subscriptions/{subscriptionId}/test`
- `POST /message-subscriptions/collect`
- `POST /message-subscriptions/maintenance`
- `POST /message-subscriptions/telegram/login/start`
- `POST /message-subscriptions/telegram/login/verify`
- `GET /platform-adapters`
- `POST /platform-adapters`
- `PUT /platform-adapters/{adapterId}`
- `DELETE /platform-adapters/{adapterId}`
- `POST /platform-adapters/{adapterId}/test`
- `GET /ingested-messages`
- `POST /ingested-messages`
- `PUT /ingested-messages/{messageId}`
- `DELETE /ingested-messages/{messageId}`
- `POST /ingested-messages/refilter`
- `POST /ingested-messages/feedback/batch`
- `POST /ingested-messages/{messageId}/feedback`
- `POST /ingested-messages/{messageId}/filter`
- `GET /message-feedback/training-samples`
- `GET /message-feedback/evaluation`
- `GET /message-feedback/training-snapshots`
- `POST /message-feedback/training-snapshots`
- `GET /message-feedback/training-exports`
- `POST /message-feedback/training-exports`
- `GET /message-feedback/training-exports/{exportVersion}`
- `GET /message-feedback/source-trust`
- `POST /message-feedback/source-trust/recompute`
- `GET /meetings`
- `POST /meetings`
- `GET /meetings/{meetingId}`
- `PUT /meetings/{meetingId}`
- `POST /meetings/{meetingId}/recap`
- `POST /meetings/{meetingId}/trust-reviews`
- `POST /meetings/{meetingId}/recap-action-reviews`
- `DELETE /meetings/{meetingId}`
- `POST /meetings/{meetingId}/cancel`
- `POST /meetings/{meetingId}/restart`
- `GET /meetings/{meetingId}/events`
- `GET /meetings/{meetingId}/references`
- `POST /meetings/{meetingId}/references`
- `DELETE /meetings/{meetingId}/references/{referenceId}`
- `GET /meetings/{meetingId}/stream`
- `GET /wake-plans`
- `POST /wake-plans`
- `PUT /wake-plans/{planId}`
- `POST /wake-plans/{planId}/fire`
- `POST /wake-plans/{planId}/pause`
- `POST /wake-plans/{planId}/resume`
- `POST /wake-plans/{planId}/cancel`
- `DELETE /wake-plans/{planId}`
- `GET /paper/overview`
- `GET /paper/risk-configs`
- `POST /paper/risk-configs`
- `PUT /paper/risk-configs/{configId}`
- `DELETE /paper/risk-configs/{configId}`
- `GET /paper/accounts`
- `POST /paper/accounts`
- `PUT /paper/accounts/{accountId}`
- `POST /paper/accounts/{accountId}/activate`
- `POST /paper/accounts/{accountId}/deactivate`
- `DELETE /paper/accounts/{accountId}`
- `GET /paper/accounts/{accountId}/positions`
- `GET /paper/accounts/{accountId}/orders`
- `GET /paper/accounts/{accountId}/fills`
- `GET /paper/accounts/{accountId}/corporate-actions`
- `POST /paper/accounts/{accountId}/corporate-actions`
- `GET /paper/accounts/{accountId}/performance`
- `GET /paper/accounts/{accountId}/replay`
- `POST /paper/accounts/{accountId}/backtests`
- `GET /paper/orders`
- `POST /paper/orders`
- `POST /paper/orders/{orderId}/approve`
- `POST /paper/orders/{orderId}/reject`
- `POST /paper/orders/{orderId}/cancel`
- `POST /paper/orders/{orderId}/fill`
- `DELETE /paper/orders/{orderId}`

`POST /paper/accounts/{accountId}/backtests` 执行只读历史回测，不会创建真实模拟盘订单、成交、持仓或权益快照。请求资源类型为 `paper-backtests`，attributes 支持 `code`、`startDate`、`endDate`、`initialCash`、`buyThresholdPct`、`sellThresholdPct`、`orderPct` 和 `slippageBps`。响应资源类型同为 `paper-backtests`，返回 `input`、`policy`、`summary`、`series` 和 `orders`：当前基础引擎使用日线收盘价、100 股一手、T+1、零成交量停牌、默认 10% 涨跌停带、滑点和模拟盘风控/手续费快照；`orders` 会记录成交或拒绝原因，便于复盘和后续替换为更完整撮合模型。

模拟交易风险配置资源暴露 `accountIds` 和 `accountCount`。创建或更新时接受 `accountIds`，用于替换通过 `paper_accounts.risk_config_id` 绑定的一组模拟交易账户；空列表是有效值，表示该配置不作用于任何账户。删除风险配置会解绑受影响账户，并在删除资源中返回 `unboundAccountIds` / `unboundAccountCount`。
模拟交易订单资源暴露 `approvalRequired`、`approvalStatus`、`approvalRiskLevel`、`approvalRiskReasons`、`approvalRiskMetrics`、`approvalReviewRequired`、`approvalConfirmRequired`、`filledQuantity`、`remainingQuantity` 和 `partialFillCount`。`suggested` 表示待审批；只有审批通过后才进入 `pending`，随后由模拟盘维护任务按交易时段和价格可用性执行。高风险审批必须在 `POST /paper/orders/{orderId}/approve` 中传 `confirmHighRisk=true`。拒绝后的订单状态为 `rejected`，可以删除；部分成交后的 `pending` 订单可以继续成交剩余数量或撤销未成交部分。
模拟交易绩效资源暴露 `attribution`、`riskAlerts` 和 `riskSummary`。`attribution` 按标的聚合当前持仓浮盈亏与成交已实现盈亏，返回仓位占比、总盈亏、收益率和贡献度；`riskAlerts` 覆盖缺失风控、仓位集中、低现金、回撤、待处理订单、持仓浮亏和估值过期，`riskSummary` 给出账户级最高风险状态。
模拟交易公司行为资源暴露 `actionType`、`exDate`、`cashPerShare`、`shareRatio`、`affectedShares`、`cashAmount`、`status` 和 `appliedAt`。创建公司行为是受保护写操作，会立即调整当前账户现金或持仓，并进入审计与复盘时间线；当前基础版不尝试替代完整历史回测引擎。

研究团队资源暴露 `paperAccountId`；API 会拒绝将同一个模拟交易账户绑定到多个团队。消息订阅资源暴露 `teamIds`；启用状态的订阅必须有非空列表，禁用状态的订阅可以保持为空。RSS/Atom subscriptions require http/https feed URLs，reject URL-embedded credentials，并通过 `rssAuthType`、`rssUsername`、`rssPassword` 支持 RSS Basic/Bearer feed authentication；密码或 token 走 `SecretKindMessageSubscription` 加密保存，响应只回显 `rssAuthType`、`rssUsername` 与 `hasRssPassword`。`POST /message-subscriptions/maintenance` 支持 `repair_defaults`、`clear_collect_error`、`queue_collect`、`rotate_rss_auth`、`audit_telegram_access` 和 `apply_source_trust_governance`，可按 `subscriptionIds`、`provider`、`onlyBlocked`、`onlyWarnings` 限定范围；响应返回 `matched`、`checked`、`passed`、`failed`、`updated`、`queued`、`paused` 和 `skipped`。RSS 批量轮换仍不返回密码或 token。手动创建会议需要 `researchTeamId`。
`POST /research-teams/{teamId}/roles/apply-defaults` 是针对该团队的破坏性重置：服务端会删除当前所有团队角色，并重新创建内置默认研究角色。
