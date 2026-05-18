# API 契约

所有路由都以 `/api` 为根路径。除健康检查、是否需要初始化、登录和初始化接口外，受保护路由都需要 `Authorization: Bearer <jwt>`。

`api/openapi.yaml` 是当前 HTTP 契约的事实来源。下面的路由列表是面向前端 API 表面的人工索引；如果本文档与 `api/openapi.yaml` 冲突，应更新本文档以匹配 OpenAPI。

- `data.type` 使用 kebab-case 的复数资源名。
- `id` 始终是字符串。
- 业务字段放在 `attributes` 下，并使用 camelCase。
- 关联关系放在 `relationships` 下。
- 错误使用 `errors: [{ code, message, detail?, field? }]`。
- 列表使用 `page[limit]` 和 `page[cursor]`，并返回 `links.next`、`meta.pageSize`、`meta.nextCursor` 和 `meta.hasMore`。游标是不透明的 API 值；客户端必须原样透传，不得解析、递增或自行构造。
- 消息摄取 API 使用与提供方无关的资源类型：`message-subscriptions`、`message-subscription-filters`、`message-subscription-app-configs`、`platform-adapters` 和 `ingested-messages`。Telegram 和 RSS/Atom 仍然是属性或 provider 值，不是这些路由的 JSON:API 资源类型。
- 消息订阅支持 `telegram_channel` 和 `rss_feed` 提供方。所有提供方的 `sourceMessageId` 都是字符串；Telegram 数字消息 ID 序列化为十进制字符串，RSS/Atom 条目 ID 使用 GUID、链接或哈希身份。订阅暴露 `pollIntervalSeconds`、`lastCollectedAt`、`nextCollectAt` 和 `lastCollectError`，用于表示与提供方无关的采集状态。
- 消息订阅过滤器是由消息订阅域拥有的可复用配置。每个订阅只绑定一个过滤器；多个订阅可以共享一个过滤器，默认过滤器只用于新建订阅时的预选。启动种子数据只会在没有任何过滤器记录时创建内置默认过滤器。
- 摄取消息通过 `filterStatus` 暴露 `unfiltered`、`filtering`、`filtered` 或 `failed` 状态。采集和重新过滤 API 会把工作加入队列，并在任务被接受后返回；AI 过滤和会议创建通过消息任务队列异步执行。
- 研究团队 API 使用 `research-teams` 和 `research-team-roles`。会议、自选列表、唤醒计划、摄取消息过滤器和消息订阅使用显式的 `researchTeamId` 或 `teamIds` 字段，而不是隐式全局团队。
- 行情运行时设置暴露一个历史数据提供方、一个实时数据提供方、`MARKET_REALTIME_COMPAT_PROVIDER` 和实时缓存 TTL。`MARKET_REALTIME_COMPAT_PROVIDER` 是一个范围很窄的实时兼容来源，用于 ETF/LOF 或主数据源缺失报价（`tencent`、`sina` 或 `disabled`），不是通用兜底提供方列表。实时兜底数据源列表不是当前契约的一部分。
- `MEETING_DAILY_TOKEN_BUDGET` 使用 `-1` 表示会议 token 使用量不受限制；正整数表示每日上限，`0` 或小于 `-1` 的值无效。
- 日志是基于文件的 JSON:API 资源。日志条目暴露 UTC 时间戳，以及必需的通用字段 `event`、`group`、`method`、`status` 和可空的 `durationMs`；不适用的字符串字段使用 `not_applicable`。日志配置通过运行时环境设置编辑，并在重启后生效。`GET /logs` 默认隐藏噪声较大的静态资源、前端和日志查看器请求；传入 `includeNoise=true` 可返回这些记录。

服务器发送事件（SSE）和静态前端资源保持各自的原生协议。

## 公开与认证

- `GET /health`
- `GET /auth/bootstrap-required`
- `POST /auth/bootstrap`
- `POST /auth/login`

## 受保护领域

- `GET /dashboard`
- `GET /logs`
- `GET /logs/files`
- `GET /settings/runtime-env`
- `GET /settings/secret-definitions`
- `GET /settings/secrets`
- `POST /settings/secrets`
- `GET /settings/app-settings`
- `PUT /settings/app-settings/{key}`
- `GET /ai/providers`
- `POST /ai/providers`
- `PUT /ai/providers/{providerId}`
- `GET /ai/providers/{providerId}/models`
- `POST /ai/providers/{providerId}/models/sync`
- `GET /ai/tool-definitions`
- `GET /ai/skill-definitions`
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
- `GET /market/daily/{code}`
- `GET /market/watchlist`
- `POST /market/watchlist`
- `PUT /market/watchlist/{itemId}`
- `DELETE /market/watchlist/{itemId}`
- `POST /market/tools/query`
- `GET /message-subscriptions/app-config`
- `POST /message-subscriptions/app-config`
- `GET /message-subscriptions`
- `POST /message-subscriptions`
- `PUT /message-subscriptions/{subscriptionId}`
- `DELETE /message-subscriptions/{subscriptionId}`
- `POST /message-subscriptions/test`
- `POST /message-subscriptions/{subscriptionId}/test`
- `POST /message-subscriptions/collect`
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
- `POST /ingested-messages/{messageId}/filter`
- `GET /meetings`
- `POST /meetings`
- `GET /meetings/{meetingId}`
- `PUT /meetings/{meetingId}`
- `POST /meetings/{meetingId}/recap`
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
- `GET /paper/accounts/{accountId}/performance`
- `GET /paper/orders`
- `POST /paper/orders`
- `POST /paper/orders/{orderId}/cancel`
- `POST /paper/orders/{orderId}/fill`
- `DELETE /paper/orders/{orderId}`

模拟交易风险配置资源暴露 `accountIds` 和 `accountCount`。创建或更新时接受 `accountIds`，用于替换通过 `paper_accounts.risk_config_id` 绑定的一组模拟交易账户；空列表是有效值，表示该配置不作用于任何账户。删除风险配置会解绑受影响账户，并在删除资源中返回 `unboundAccountIds` / `unboundAccountCount`。

研究团队资源暴露 `paperAccountId`；API 会拒绝将同一个模拟交易账户绑定到多个团队。消息订阅资源暴露 `teamIds`；启用状态的订阅必须有非空列表，禁用状态的订阅可以保持为空。RSS/Atom 订阅需要公开的 `http` 或 `https` feed URL，拒绝 URL 内嵌凭据，并且 v1 不支持 feed 认证。手动创建会议需要 `researchTeamId`。
`POST /research-teams/{teamId}/roles/apply-defaults` 是针对该团队的破坏性重置：服务端会删除当前所有团队角色，并重新创建内置默认研究角色。
