# TradingCopilot 产品与架构需求文档

## 产品结论

TradingCopilot 当前定位为自托管个人或小团队投研工作台，优先服务 A 股投研、消息驱动会议、自选池、唤醒计划和模拟盘闭环。现有架构保持 Go 模块化单体、JSON:API/OpenAPI 契约、Vue 控制台和 Docker 部署，不引入微服务拆分。

主要产品缺口集中在首次配置闭环、团队安全、数据可信、交易风控、运维可用性和工程一致性。本轮已补齐设置向导基础接口与前端入口、唤醒计划手动创建/编辑、团队安全/审计基础、运维状态基础报告、模拟盘审批与执行约束、绩效归因/风险预警、公司行为/复盘时间线、基础日线历史回测、Prometheus 指标、备份恢复预检与周期性恢复演练调度、AI 成本配置/预算治理、会议可信度报告级证据门禁、recap action 人工复核记录、消息反馈训练样本、快照/导出归档与来源可信治理，以及工程文档和 Docker 策略一致性；更深的运行时证据阻断、高级撮合/完整历史回测引擎和生产级外部恢复编排仍按本文件拆分推进。

## P0 首次上手闭环

### 已落地基础

- `GET /setup/readiness` 汇总管理员、AI 服务商、投研团队、模拟盘风控、行情、消息源、代理、日志和队列状态。
- `POST /setup/actions/{key}` 支持同步 AI 模型、应用默认 AI 能力、创建默认团队、检查模拟盘默认配置、创建默认消息过滤器、同步证券列表和测试代理。
- `GET /setup/readiness` 已调整为只读状态聚合：模拟盘和消息过滤器状态读取不再借用会隐式创建默认数据的列表用例，初始化数据只由 `POST /setup/actions/{key}` 显式触发。
- 新增 HTTP 回归测试覆盖管理员初始化后 readiness、`sync-ai-models` 无 provider warning、代理测试、默认模拟盘、默认团队和默认消息过滤器动作的重复执行，确保不会重复创建账户、风控、团队、角色或过滤器。
- 新增首次配置闭环烟测覆盖管理员初始化 -> 配置 AI provider -> 显式执行默认模拟盘/团队/消息过滤器动作 -> 绑定过滤器模型 -> 创建 RSS 订阅 -> 写入行情证券 -> 创建会议 -> 由带证据的主持人 recap action 生成自选、唤醒计划和会议来源模拟盘订单，并回查 readiness 中 AI、团队、模拟盘、行情和消息源均为 ready。
- 前端新增“设置向导”一级入口，展示完成度、状态、动作按钮和领域页面跳转。
- 唤醒计划页面支持手动创建和编辑 `time`、`indicator`、`event` 三类条件。
- `docker-compose.integration.yml` 与集成测试文档对齐。
- `frontend/Dockerfile` 与主 Dockerfile 对齐 Node 24 + `npm ci`。
- `.editorconfig` 为 Go、Shell、YAML 显式指定 LF，避免 Windows 工作区触发大面积格式化漂移。

### 后续验收

- 新管理员初始化后，用户应能在设置向导中完成 AI provider、默认团队、默认风控、行情同步、消息过滤器和代理测试。
- 设置向导动作必须幂等，重复执行不得破坏用户已有配置。
- OpenAPI、Go server adapter、前端生成类型和人工 API 索引必须保持一致。

## P1 产品可信与团队安全

### 团队安全

- 已落地基础：`admin_users` 扩展角色、启停、显示名和最近登录时间；`auth_sessions` 支持 access token 会话记录、自助登出与撤销；`audit_events` 记录初始化、登录、登出、用户治理、密码重置、会话撤销和所有受保护 HTTP 写操作；前端新增“团队安全”页面。
- 已落地基础：用户管理、`owner/admin/operator/viewer` HTTP RBAC、会话列表、会话撤销、密码重置和敏感操作二次确认；`viewer` 保持只读与自助登出，`operator` 可执行投研/行情/消息/模拟盘/队列重试等业务动作，但不能修改安全、密钥、AI、平台适配器、研究团队、设置向导和备份治理。
- 增加操作审计模型，覆盖登录、配置变更、密钥写入、删除、状态切换、订单相关动作和会议重启/取消。
- 已落地基础：前端 token 默认改存 `sessionStorage`，读取时迁移并清理旧版 `localStorage` token，侧边栏和移动抽屉提供“退出”动作并调用 `POST /auth/logout` 撤销当前会话；如后续要进一步降低 XSS 后果，可评估 httpOnly cookie/CSRF 方案。

### 投研可信度

- 已落地基础：会议详情返回 `trustReport`，从最近会议事件派生工具证据、引用、模型快照、提示词哈希快照、声明到证据/引用的绑定、结论历史、逐句结论变更摘要、事实/假设/推断、证据缺口和缺口提示；前端会议详情新增“可信度”面板并展示声明证据绑定和逐句结论变化。
- 已落地基础：托管会议角色输出 schema 强制要求 `facts`、`assumptions`、`inferences` 和 `evidence_gaps`，主持人最终 recap schema 同样提示输出 `facts`、`assumptions`、`inferences`、`evidence_gaps` 和 `citations`，会议事件会持久化这些字段，避免只能从自由文本中事后猜测。
- 稳定提示词版本、模型配置快照、声明到证据/引用的基础绑定、逐句结论 diff 和结论句人工复核工作流已落地；`POST /meetings/{meetingId}/trust-reviews` 会把复核判定、引用 ID、证据事件 ID、复核人和备注写入会议事件，`trustReport.sentenceReviews` 聚合最近复核记录，前端会议详情可逐句确认、标记待补证据或驳回。
- 已落地基础：`trustReport.evidenceGate` 对 `fact` 和 `inference` 声明执行报告级门禁，要求至少绑定结构化证据事件或引用；缺证声明会返回 `blocked` 状态、缺证数量和声明明细，前端会议详情展示“证据门禁”和“缺证声明”。`assumption` 与 `evidence_gap` 仍作为上下文，不作为阻断项。
- 已落地基础：`trustReport` 会聚合 `recap_actions_blocked` 与 `recap_actions_review_required` 事件中的 `suggested_actions`，暴露 `recapActionReviewStatus`、`recapActionSuggestionCount` 和 `recapActionSuggestions`；前端会议详情“可信度”面板展示待复核动作、原始 spec、处置状态、阻断/复核原因和证据摘要，形成从证据门禁到人工复核队列的可见闭环。
- 已落地基础：新增 `POST /meetings/{meetingId}/recap-action-reviews`，将待复核 recap action 的人工判定、证据/引用 ID、复核人、备注、原始 spec 和执行处置写入 `recap_action_review` 会议事件；`trustReport` 聚合 `recapActionReviewCount`、`recapActionReviews`，并为每条 `recapActionSuggestions` 挂载 `latestReview`。`approved` 判定必须二次确认，且只记录为 `manual_execution_required`，不会绕过证据门禁自动创建自选、唤醒计划或模拟盘订单。
- 已落地基础：主持人 recap 若包含 `watchlist_actions`、`wake_plans` 或 `orders`，运行时语义校验会要求同时提供结构化 `facts`/`inferences` 和 `citations`；缺失时触发模型重试，要求补证据或移除可执行动作，避免缺证结论直接落成自选、唤醒计划或模拟盘订单。
- 已落地基础：会议 recap action 落库前增加二次门禁，手工 recap、历史事件重放或恢复路径若包含缺证据、无引用或语义无效的自选、唤醒或订单动作，会写入 `recap_actions_blocked` 系统事件并跳过所有可执行动作创建；事件 payload 保留 `suggested_actions`、`disposition`、`evidence_summary` 和动作计数，后续可直接升级为人工复核或审批队列。
- 已落地基础：会议来源模拟盘订单会派生高风险复核提示，覆盖缺少会议事件来源、账户/风控不可用、价格数量异常、单笔金额接近或超过上限、现金占用过高、买后仓位接近或超过上限、卖出数量超过当前持仓等场景；前端审批时展示原因，高风险订单必须二次确认后才会提交后续风控。
- 已落地基础：带可执行动作的 recap 若只有 `@role` 形式的角色引用、没有工具/来源引用或显式证据事件 ID，会写入 `recap_actions_review_required` 系统事件并跳过自动创建自选、唤醒计划和模拟盘订单；这类弱引用动作会以 `manual_review_required` 处置状态保留结构化建议，需要人工复核或补充来源证据后再执行。
- 后续继续对 `blocked` 证据门禁结果引入更细粒度的人工复核、降级为建议或审批队列策略。

### 模拟盘真实度

- 已落地基础：会议来源的模拟盘订单默认进入 `suggested` 审批队列，前端支持通过/拒绝；通过时先展示服务端派生的复核等级和原因，高风险订单必须带 `confirmHighRisk=true` 二次确认后才会复用既有风控提交到待执行队列，拒绝原因保留用于复盘和审计；手动/自动成交复用成交明细，支持按数量部分成交、累计费用与均价、剩余数量继续挂单或撤销；自动维护批次按 `executeAfter, id` 队列顺序消耗同标的实时成交量预算，预算不足时形成自动部分成交并保留未成交数量。
- 已落地基础：自动执行会检查 A 股 T+1 可卖数量、涨跌停价格区间、实时报价零成交量疑似停牌和过期单；自动成交应用基础滑点，并按当日实时成交量参与率叠加价格冲击，`executionNote` 会解释未成交、滑点、价格冲击和撮合报告。
- 已落地基础：账户绩效资源返回组合归因、持仓/已实现盈亏贡献、单标的仓位占比，以及缺失风控、仓位集中、低现金、回撤、待处理订单、浮亏和估值过期等风险预警；前端模拟盘详情展示预警和归因排行。
- 已落地基础：新增 `paper_corporate_actions`，支持现金分红、送股和拆股的手动应用；现金分红调整账户现金，送股/拆股调整持仓数量和平均成本；前端模拟盘支持创建公司行为、查看公司行为列表和订单/成交/公司行为复盘时间线。
- 已落地基础：新增只读日线历史回测 API `POST /paper/accounts/{accountId}/backtests` 与前端“历史回测”面板，支持按标的、时间区间、初始资金、买卖阈值、单笔比例和滑点运行基础策略回测；报告返回策略输入、执行/风控策略快照、权益曲线、成交/拒绝订单和回撤收益摘要，不写入真实模拟盘账户。
- 继续增加盘口档位价格冲击、公司行为复权、盘口级/分钟级回测和完整历史回测引擎。
- 回测/复盘应复用模拟盘风控、成交模型和公司行为语义，不另建不一致规则。

### 数据可靠性

- 已落地基础：provider health 诊断暴露行情最新报价年龄、日线年龄、简易健康分、消息过滤状态分布、采集错误、到期订阅、去重策略、私有源就绪状态和按订阅源聚合的人工反馈可信评分。
- 已落地基础：`GET /message-subscriptions/diagnostics` 按订阅源返回 Telegram 私有/公开引用、RSS 公共 URL/内嵌凭据、RSS Basic/Bearer 私有源认证、代理路由、过滤器 AI provider、团队绑定和最近采集错误检查项；`POST /message-subscriptions/maintenance` 支持按订阅源批量执行默认过滤器修复、采集错误清理、重新入队采集、RSS Basic/Bearer 凭据轮换和 Telegram 账号权限巡检；巡检会复用订阅测试能力，失败写入 `lastCollectError`，成功清理旧错误，并返回 checked/passed/failed 统计。前端消息订阅器提供“修复阻塞源”“Telegram 权限巡检”和“RSS 凭据轮换”入口，展示状态、路径、推荐动作和检查项。
- 已落地基础：消息库支持对每条摄取消息记录 `helpful`、`noise`、`misclassified` 或 `neutral` 人工反馈；`GET /message-feedback/training-samples` 导出带用途、权重、训练/验证切分和去重 key 的训练样本；`GET /message-feedback/evaluation` 返回样本数、验证集数、一致率、噪声率、误判率和治理建议；`GET/POST /message-feedback/training-snapshots` 列出或创建带版本、指纹、样本 manifest、来源可信摘要和评估摘要的训练集快照；`GET/POST /message-feedback/training-exports` 与 `GET /message-feedback/training-exports/{exportVersion}` 支持创建、列出和按版本读取 JSONL 训练集导出归档，归档记录样本数、训练/验证切分、来源摘要、评估摘要、`contentSha256`、字节数和行数，前端消息库支持创建导出和下载 `.jsonl`；`GET /message-feedback/source-trust` 与 `POST /message-feedback/source-trust/recompute` 按订阅源聚合反馈样本数、0-100 来源可信评分、状态、解释、推荐治理动作和自动降权动作，前端消息库新增反馈治理面板。
- 已落地基础：来源级自动治理剧本已接入 `POST /message-subscriptions/maintenance` 的 `apply_source_trust_governance` 动作；严重低可信来源会被暂停采集并写入 `lastCollectError`，中等风险来源会保留启用但写入治理观察状态，治理快照保存到订阅 `config.sourceTrustGovernance`，前端反馈治理面板提供“应用治理”入口。
- 已落地基础：消息库支持多选后通过 `POST /ingested-messages/feedback/batch` 批量记录人工反馈，批处理返回请求数量、更新数量和缺失 ID，方便复核历史消息并快速积累训练/验证样本。
- 已落地基础：消息过滤默认启用来源可信自动降权。至少 3 条同源反馈后，低可信来源会把模型输出的 `meeting` 降为 `observe`，极低可信或严重负反馈来源会把 `observe` 降为 `ignore`；降权原因会写入消息 `filterReason`，策略可通过 `MESSAGE_SOURCE_TRUST_POLICY` app setting 覆盖。
- 已落地基础：消息过滤 worker 在 provider/config 调用失败时会先把消息持久化为 `failed` 并保留失败原因，再向队列返回可重试错误，Redis/asynq 可按 retry/archive 路径形成死信治理，运维页的失败任务筛选、批量重试和单任务重试可用于人工处置；手动过滤接口仍返回已失败消息状态，便于页面直接展示原因。
- 已落地基础：新增 `POST /market/tasks` 提交 `sync_symbols`、`refresh_quote`、`refresh_daily_bars` 后台任务，Redis/asynq worker 消费 `market_run_task`；provider/config 失败会返回给队列，由 retry/archive、`GET /ops/jobs` 失败样本、批量重试和单任务重试统一治理。
- 后续可继续增加模型离线对比评测、更细粒度的来源分层策略和导出归档的外部对象存储适配。
- 后续可继续增加更细粒度的来源级自动治理剧本，例如按来源可信评分自动触发巡检、要求人工复核、分层采样或按不同业务团队套用不同治理策略。

## P2 运维产品化
- 已落地基础：新增 `GET /ops/provider-health`、`GET /ops/jobs`、`GET /ops/backups` 和 `POST /ops/backups`，前端新增“运维状态”页面，展示 provider、后台心跳、Redis/asynq 队列快照、备份状态报告、归档提供方，并可创建本地备份 zip 归档。
- 已落地基础：新增 `GET /ops/metrics` Prometheus text 指标，覆盖业务计数、AI model call/token 用量、行情新鲜度、消息过滤失败、队列 totals 和后台依赖状态。
- 已落地基础：新增 `POST /ops/backups/{backupName}/restore-dry-run`，前端可对备份包执行非破坏性沙箱恢复演练，展示 manifest/条目/数据库/日志检查结果、沙箱目录、解压文件数和检查项；dry-run 会把归档安全解压到备份目录下的 `restore-drills/`，并持久化最近一次恢复演练记录，备份报告显示已演练、警告或过期状态。
- 已落地基础：备份创建后会自动执行保留策略，默认至少保留最新 7 份和最近 30 天归档，清理过旧 zip 与过旧 `restore-drills/` 沙箱目录；前端备份概览展示保留窗口和最近一次清理结果，`.env.example` 暴露 `BACKUP_RETENTION_COPIES`、`BACKUP_RETENTION_DAYS` 与 `BACKUP_RESTORE_DRILL_INTERVAL_HOURS`。
- 已落地基础：新增 `doc/upgrade-migration.md`，覆盖 Docker Compose 和本地二进制升级、迁移约束、回滚流程、升级后验收和版本记录模板。
- 已落地基础：provider health 的 AI 诊断从会议事件聚合近 24 小时和累计 token 用量，按 provider/model 展示调用、token、成本、成本来源、缺价格调用和估算调用分布；成本优先读取 provider payload，缺失时按 `AI_COST_RATES` 配置估算，不硬编码会随时间漂移的模型价格。
- 已落地基础：设置页支持维护 `AI_DAILY_COST_BUDGET` 和 `AI_COST_RATES`，运维页展示 24h 成本、预算状态和预算用量；`GET /ops/metrics` 暴露 AI 成本金额、预算使用率和缺价格调用数。
- 已落地基础：运维面板展示队列积压风险、失败任务样本和 retry/archive 任务统计，新增 `POST /ops/jobs/retry-failed` 将失败队列任务重新提交处理，并通过受保护写操作审计记录；`GET /ops/jobs` 和前端失败任务表支持按队列、任务类型、retry/archive 状态和样本数量筛选，`POST /ops/jobs/{queue}/tasks/{taskId}/run` 支持对单个失败任务重新提交。
- 已落地基础：`GET /ops/jobs` 增加 `recentErrors`，复用结构化日志查询近 24 小时 error 级别日志摘要；运维面板直接展示近期错误数量和样本，Prometheus 指标暴露 `tradingcopilot_recent_errors_24h`；近期错误表支持跳转到日志页并带入 level/event/group/path/q 等筛选参数，日志页支持 query 初始化筛选。
- 已落地基础：provider health 增加 meeting runtime 诊断，聚合近 24 小时会议完成/失败/运行数量、排队等待、运行耗时均值/P95 和事件类型计数；AI 诊断增加模型调用延迟观测数、平均延迟、P95 延迟、启用 provider/model 价格覆盖率和缺价格模型样本，并按 provider/model 分组；Prometheus 增加会议排队等待、会议运行耗时、AI latency 和 AI 价格覆盖指标，运维页展示“会议运行”状态卡。

- 恢复演练已具备本地沙箱解压和 zip-slip 防护，备份创建已自动执行保留策略，Redis scheduler 默认每 168 小时投递 `run_backup_restore_drill` 对最新备份执行非破坏性 dry-run，`BACKUP_RESTORE_DRILL_INTERVAL_HOURS=0` 可禁用；升级/迁移说明已补齐。备份服务已抽象 `BackupArchiveStore`，当前内置 `local_filesystem`、`s3` 和 `oss` provider，列表、创建、读取、删除和保留策略均通过 provider 边界执行；S3/OSS 恢复演练会先下载远端对象到临时文件，再安全解压到本地 `backupMetadataDir/restore-drills/`。
- 已落地基础：`BACKUP_ARCHIVE_PROVIDER=s3|oss` 的外部归档配置就绪度诊断已接入 `/ops/backups`、备份创建结果、恢复 dry-run 结果和前端运维页；报告会暴露 `configuredProvider`、`configuredExternalProvider`、`externalReady`、`missingExternalConfig` 和 `setupHint`。设置页支持维护外部归档变量，S3/OSS access key 与 secret key 只写入、不回显，留空不覆盖已有 `.env` 值。`BACKUP_ARCHIVE_PROVIDER=s3|oss` 且配置完整时，真实上传、远端读取、远端删除和保留策略已走 S3/S3-compatible endpoint 或阿里云 OSS；真实外部端到端仍需部署环境提供 bucket/endpoint/凭据后执行集成验收。
- 已落地基础：HTTP 入口接入 OpenTelemetry trace context 提取与 span 创建，结构化请求日志携带 `traceId`/`spanId`；会议运行阶段、AI provider latency 和 provider/model 价格覆盖基础指标已通过 dashboard/provider health/Prometheus 暴露；OTLP exporter、采样策略和跨 worker 链路传播配置已在可观测性补齐项中落地。
- 增加运维面板：provider health、jobs、backups、近期错误、失败任务筛选、单任务重试和日志跳转联动已覆盖。
- 已落地基础：前端 `api.ts` 增加 OpenAPI 派生的扁平资源模型类型，`Meeting`、设置向导、团队安全、运维队列/日志、消息、研究团队、唤醒计划、模拟盘主要资源、Dashboard、OpsBackups 和 PaperPerformance 曲线点已收敛到生成 schema；金额/比例字段仍在生成类型上做前端数值展示覆盖，少量深层诊断对象保留 `additionalProperties` 以支持运维扩展。

## 架构约束

- OpenAPI 继续作为 HTTP API 事实源。
- README、`doc/api-contract.md`、`doc/database-contract.md` 只做同步说明。
- 会议运行器中业务编排逻辑应逐步从 `internal/infra/persistence/gorm/meeting` 抽到应用层 runtime/orchestrator，GORM 包保留持久化和映射；已先把会议 run 启动策略、终态判断、启动事件 payload、legacy 完成默认文案、角色轮次输出归一化、主持人 fallback 计划策略、主持人 recap 可执行动作证据校验和唤醒计划语义校验收敛到 `internal/app/meeting`，后续继续上移 managed turn/finalize 编排。
- 不接真实券商交易；模拟盘订单不能直接外发到券商。

## 必需验证

- `go test ./...`
- `go vet ./...`
- `go generate ./... && git diff --exit-code`
- `npm ci --prefix frontend && npm run build --prefix frontend`
- `docker compose config`

### 2026-06-09 可观测性补齐

- 已落地基础：新增 `internal/infra/tracing`，主命令在 `serve`、`worker`、`scheduler` 和消息订阅命令启动时统一初始化 OpenTelemetry；默认 `OTEL_TRACES_EXPORTER=none`，配置为 `otlp` 后可通过 `OTEL_EXPORTER_OTLP_PROTOCOL=grpc|http/protobuf` 和 `OTEL_EXPORTER_OTLP_ENDPOINT` 上报到 OTLP collector。
- 已落地基础：新增 `OTEL_TRACES_SAMPLER` 和 `OTEL_TRACES_SAMPLER_ARG`，支持 `always_on`、`always_off`、`traceidratio`、`parentbased_traceidratio` 等采样策略；运行时设置页可维护 OTEL 环境变量，均标记为重启生效。
- 已落地基础：Redis/asynq 与本地消息队列在入队时使用兼容 envelope 注入 `traceparent`/`tracestate`，worker 消费时先解包再创建 consumer span；旧的裸 payload 任务仍可继续消费，不破坏已有 retry/archive 队列。

外部依赖相关检查以 `doc/integration-harness.md` 为准，只有配置对应 `TC_INTEGRATION_*` 环境变量时才运行。
