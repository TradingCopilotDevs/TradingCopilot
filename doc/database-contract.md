# 数据库契约

TradingCopilot 根据 GORM 持久化模型初始化和升级当前逻辑 schema。本轮 schema 变更保持直接升级兼容：从上一公开版本数据库启动当前版本时，会通过 `AutoMigrate` 增量创建新增表和新增列，并执行幂等数据修正，不要求重建数据库。生产升级前仍必须先完成备份和恢复 dry-run。

## 数据表

- `admin_users`
- `auth_sessions`
- `audit_events`
- `secrets`
- `app_settings`
- `ai_providers`
- `ai_provider_models`
- `agent_roles`
- `prompt_templates`
- `research_teams`
- `research_team_roles`
- `market_symbols`
- `daily_bars`
- `realtime_quotes`
- `watchlist_items`
- `prediction_events`
- `prediction_markets`
- `prediction_market_quotes`
- `prediction_market_matches`
- `prediction_watchlist_items`
- `message_subscriptions`
- `message_subscription_research_teams`
- `message_subscription_filters`
- `ingested_messages`
- `platform_adapters`
- `telegram_channels`
- `telegram_messages`
- `meetings`
- `meeting_events`
- `meeting_references`
- `tool_call_logs`
- `wake_plans`
- `risk_configs`
- `paper_accounts`
- `paper_orders`
- `paper_positions`
- `paper_fills`
- `paper_equity_snapshots`
- `paper_corporate_actions`

## 升级兼容性

- 本轮新增表：`prediction_events`、`prediction_markets`、`prediction_market_quotes`、`prediction_market_matches` 和 `prediction_watchlist_items`。
- 本轮新增列：`research_teams.asset_class`；`research_teams.paper_account_id` 允许为空，以支持无模拟盘绑定的预测市场团队。
- 新增列均允许空值或带默认值；既有 `admin_users` 会得到安全默认角色 `admin` 与启用状态，不要求人工回填。
- 迁移会继续执行既有幂等修正：空 `ingested_messages.filter_status` 归一化、无历史消息订阅的 `collect_from` 清理，以及无效 active 唤醒计划取消。
- 不做破坏性表删除或列删除；历史 Telegram 表仍保留，以兼容既有数据和迁移期间的旧引用。

## 重要枚举值

- 会议状态：`queued`, `running`, `completed`, `failed`, `cancelled`.
- 会议事件类型：`system`, `role_message`, `tool_call`, `tool_result`, `conclusion`, `error`.
- Telegram 过滤决策：`ignore`, `observe`, `meeting`.
- 摄取消息过滤决策：`ignore`, `observe`, `meeting`.
- 研究团队资产类型：`a_share`, `prediction_market`, `mixed`.
- 预测市场匹配状态：`candidate`, `linked`, `review_required`, `rejected`, `confirmed`.
- 唤醒触发类型：`time`, `indicator`, `event`.
- 唤醒计划状态：`active`, `paused`, `fired`, `cancelled`.
- 订单方向：`buy`, `sell`.
- 订单状态：`suggested`, `pending`, `filled`, `rejected`, `cancelled`, `expired`。其中 `suggested` 表示会议生成后等待人工审批，审批通过后才进入 `pending`。

## 存储说明

- PostgreSQL/TimescaleDB 是生产目标。
- SQLite 仍支持本地开发和测试。
- `auth_sessions` 存储 access token 的 `jti`、到期时间和撤销信息；认证时会拒绝已撤销或过期的会话。
- `audit_events` 存储登录、用户治理、会话撤销和后续敏感操作的审计轨迹。
- 密钥保存在 `secrets.encrypted_value`，并以加密形式存储。
- 灵活载荷保持为 JSON 列：角色能力、应用设置、行情、会议载荷、唤醒触发配置、Telegram 原始载荷。
- AI 成本治理复用 `app_settings`：`AI_DAILY_COST_BUDGET` 保存每日成本预算，`AI_COST_RATES` 保存 provider/model 价格表 JSON；provider health 和 Prometheus 指标从会议事件 token 用量与这些配置派生成本视图，不新增持久化表。
- 消息订阅和平台适配器使用 JSON 配置列；摄取消息保留提供方原始载荷和相关标的数组，均为 JSON。
- 消息相关表名与提供方无关（`message_subscriptions`、`message_subscription_filters`、`ingested_messages`、`platform_adapters`），即使运行时提供方是 Telegram 和 RSS/Atom。
- `ingested_messages.source_message_id` 是与提供方无关的字符串。Telegram 将十进制消息 ID 存为字符串；RSS/Atom 存储 GUID、链接或哈希身份。唯一键保持为 `(subscription_id, source_message_id)`。
- `ingested_messages` 通过 `feedback_label`、`feedback_comment` 和 `feedback_at` 保存人工反馈；标签只使用 `helpful`、`noise`、`misclassified` 和 `neutral`，用于 provider health 的来源可信评分聚合。
- `message_subscriptions` 存储与提供方无关的采集状态：`poll_interval_seconds`、`last_collected_at`、`next_collect_at` 和 `last_collect_error`。
- 消息过滤器不是 AI 角色。每条 `message_subscriptions` 记录绑定一条 `message_subscription_filters` 记录；默认过滤器只为新订阅提供前端或应用默认值。内置默认消息过滤器只在过滤器表为空时创建；启动种子数据不会重写已有过滤器记录。
- `research_teams` 是会议、自选列表、唤醒计划和消息触发研究的业务边界。`asset_class=a_share|mixed` 的团队通过 `research_teams.paper_account_id` 绑定一条 `paper_accounts` 记录；该列上的唯一索引确保一个 paper account 最多只能属于一个团队。`asset_class=prediction_market` 的团队允许 `paper_account_id` 为空，会议运行和消息触发不得因此尝试创建模拟盘订单。
- `research_team_roles` 存储每个团队的会议角色。会议运行器必须按 `meetings.research_team_id` 加载角色，而不是从全局 `agent_roles` 加载。启动种子数据只会在没有任何研究团队时创建一个默认研究团队及其内置角色。显式的 `roles/apply-defaults` 操作会对选中团队执行破坏性重置：删除当前所有团队角色，并基于内置默认角色种子重建。
- 预测市场使用独立资产域表，不写入 A 股 `market_symbols`。`prediction_events` 保存 Polymarket event 级元数据，`prediction_markets` 保存 binary market 级问题、outcome、CLOB token、盘口摘要和状态，`prediction_market_quotes` 保存按 token 抓取的快照，`prediction_market_matches` 保存新闻片段、搜索 query、候选市场快照、分数构成、阈值处置和人工复核信息，`prediction_watchlist_items` 保存团队关注的预测市场。唯一约束以 provider + external id 为准，当前 provider 为 `polymarket`。
- 预测市场 v1 不保存钱包、Polymarket API key、真实订单、预测市场模拟盘订单、仓位、充值或提现信息。预测市场会议引用通过 `meeting_events.payload.prediction_market_ids` 和 `meeting_references.reference_type=prediction_market` 表达；实时盘口/历史价格工具结果仍写入 `meeting_events` 和 `tool_call_logs`。
- `message_subscription_research_teams` 将每个消息源绑定到一个或多个研究团队。启用状态的订阅必须至少有一个绑定；`meeting` 过滤决策对每个 message/team 组合最多创建一个会议。
- 模拟交易账户通过可空的 `paper_accounts.risk_config_id` 绑定到风险配置。空值是允许的，表示该账户没有启用中的风险配置；订单执行会拒绝这类账户，而不是静默分配默认配置。
- 删除模拟交易风险配置前会先清空匹配的 `paper_accounts.risk_config_id` 值，然后再移除配置。
- 模拟盘执行约束复用现有表：T+1 可卖数量从当日 `paper_fills` 买入成交和当前 `paper_positions.quantity` 推导；涨跌停区间使用最近上一交易日 `daily_bars.close`；疑似停牌/不可成交近似使用当日 `realtime_quotes.volume = 0`；自动撮合成交量预算和价格冲击从当日 `realtime_quotes.volume` 派生并在维护批次内按订单队列消耗，不新增持久化表；过期单使用 `paper_orders.expire_at`。
- 模拟盘组合归因和风险预警是从 `paper_positions`、`paper_fills`、`paper_equity_snapshots`、`paper_accounts` 和 `risk_configs` 派生的 API 视图，不新增持久化表；已实现盈亏以成交记录为准，持仓占比以当前总权益为分母。
- `paper_corporate_actions` 存储已应用的模拟盘公司行为，当前支持 `cash_dividend`、`bonus_share` 和 `split`。现金分红记录 `cash_per_share` 和 `cash_amount`，送股/拆股记录 `share_ratio` 与 `affected_shares`；复盘时间线从该表、`paper_orders` 和 `paper_fills` 派生。
- 会议可信度报告是从 `meeting_events.payload` 派生的 API 视图，不新增持久化表；结构化引用、模型快照、证据门禁和结论历史优先复用事件 payload 中已有的 `citations`、`raw_json`、`model`、`provider_id` 和 `token_usage`。证据门禁只派生 `fact`/`inference` 是否绑定结构化证据事件或引用，缺证明细不单独落表。
