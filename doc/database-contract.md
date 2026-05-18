# 数据库契约

TradingCopilot 根据 GORM 持久化模型初始化当前逻辑 schema。不支持既有旧版数据库结构；启动此版本前请重建数据库。

## 数据表

- `admin_users`
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

## 重要枚举值

- 会议状态：`queued`, `running`, `completed`, `failed`, `cancelled`.
- 会议事件类型：`system`, `role_message`, `tool_call`, `tool_result`, `conclusion`, `error`.
- Telegram 过滤决策：`ignore`, `observe`, `meeting`.
- 摄取消息过滤决策：`ignore`, `observe`, `meeting`.
- 唤醒触发类型：`time`, `indicator`, `event`.
- 唤醒计划状态：`active`, `paused`, `fired`, `cancelled`.
- 订单方向：`buy`, `sell`.
- 订单状态：`suggested`, `pending`, `filled`, `rejected`, `cancelled`, `expired`.

## 存储说明

- PostgreSQL/TimescaleDB 是生产目标。
- SQLite 仍支持本地开发和测试。
- 密钥保存在 `secrets.encrypted_value`，并以加密形式存储。
- 灵活载荷保持为 JSON 列：角色能力、应用设置、行情、会议载荷、唤醒触发配置、Telegram 原始载荷。
- 消息订阅和平台适配器使用 JSON 配置列；摄取消息保留提供方原始载荷和相关标的数组，均为 JSON。
- 消息相关表名与提供方无关（`message_subscriptions`、`message_subscription_filters`、`ingested_messages`、`platform_adapters`），即使运行时提供方是 Telegram 和 RSS/Atom。
- `ingested_messages.source_message_id` 是与提供方无关的字符串。Telegram 将十进制消息 ID 存为字符串；RSS/Atom 存储 GUID、链接或哈希身份。唯一键保持为 `(subscription_id, source_message_id)`。
- `message_subscriptions` 存储与提供方无关的采集状态：`poll_interval_seconds`、`last_collected_at`、`next_collect_at` 和 `last_collect_error`。
- 消息过滤器不是 AI 角色。每条 `message_subscriptions` 记录绑定一条 `message_subscription_filters` 记录；默认过滤器只为新订阅提供前端或应用默认值。内置默认消息过滤器只在过滤器表为空时创建；启动种子数据不会重写已有过滤器记录。
- `research_teams` 是会议、自选列表、唤醒计划和消息触发研究的业务边界。每个团队通过 `research_teams.paper_account_id` 只绑定一条 `paper_accounts` 记录；该列上的唯一索引确保一个 paper account 最多只能属于一个团队。
- `research_team_roles` 存储每个团队的会议角色。会议运行器必须按 `meetings.research_team_id` 加载角色，而不是从全局 `agent_roles` 加载。启动种子数据只会在没有任何研究团队时创建一个默认研究团队及其内置角色。显式的 `roles/apply-defaults` 操作会对选中团队执行破坏性重置：删除当前所有团队角色，并基于内置默认角色种子重建。
- `message_subscription_research_teams` 将每个消息源绑定到一个或多个研究团队。启用状态的订阅必须至少有一个绑定；`meeting` 过滤决策对每个 message/team 组合最多创建一个会议。
- 模拟交易账户通过可空的 `paper_accounts.risk_config_id` 绑定到风险配置。空值是允许的，表示该账户没有启用中的风险配置；订单执行会拒绝这类账户，而不是静默分配默认配置。
- 删除模拟交易风险配置前会先清空匹配的 `paper_accounts.risk_config_id` 值，然后再移除配置。
