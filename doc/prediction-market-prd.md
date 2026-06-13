# 预测市场支持 PRD（Polymarket v1）

## 背景

TradingCopilot 已具备消息订阅、AI 过滤、投研会议、A 股行情、自选池、唤醒计划和模拟盘能力。预测市场 v1 的目标是在不混淆 A 股资产域、不引入钱包或下单风险的前提下，把 Polymarket 公共市场数据接入消息分诊和会议证据链。

## 范围

v1 支持：

- Polymarket 公共事件和 binary market 发现。
- 市场搜索、活跃市场同步、事件详情、市场详情、匹配候选、人工确认/驳回和预测市场 watchlist。
- 新闻到预测市场的候选召回、打分、阈值分层和证据保存。
- 默认预测市场投研团队和默认预测市场消息过滤器。
- 会议中引用预测市场，并通过工具拉取市场快照、盘口和价格历史。
- 使用现有 Market 代理模块访问 Polymarket HTTP API。

v1 不支持：

- 钱包、API key、真实下单、充值、提现或仓位管理。
- 预测市场模拟盘。
- 把预测市场强行写入 A 股 `market_symbols`。
- 自动生成预测市场交易动作。会议 recap 只能输出观察、关注、证据缺口和后续唤醒建议。

## 数据源

- Gamma API：`https://gamma-api.polymarket.com`，用于事件、市场发现和搜索。
- CLOB API：`https://clob.polymarket.com`，用于盘口、价格和历史价格。
- Market WebSocket 目标地址：`wss://ws-subscriptions-clob.polymarket.com/ws/market`。

当前实现中，会议运行开始时会对关联预测市场的 CLOB token 进行 Market WebSocket 短采样，并把实时快照写入 meeting event；如果 WebSocket 连接、订阅或采样超时失败，则降级通过 CLOB REST 拉取盘口快照。会议工具仍可按需通过 CLOB REST 拉取盘口和价格历史。

## 领域模型

新增预测市场资产域，不复用 A 股证券表：

- `prediction_events`：保存 Polymarket event 级信息，包括 provider、externalEventId、slug、title、description、category、tags、active、closed、endDate、volume、liquidity、openInterest 和 raw。
- `prediction_markets`：保存 binary market，包括 externalMarketId、conditionId、question、slug、outcomes、outcomePrices、clobTokenIds、enableOrderBook、bestBid、bestAsk、lastTradePrice、spread、active、closed、restricted 和 raw。
- `prediction_market_quotes`：保存按 outcome token 抓取的报价快照。
- `prediction_market_matches`：保存新闻到市场的匹配证据、分数、状态和人工复核信息。
- `prediction_watchlist_items`：保存团队关注的预测市场。

`research_teams.asset_class` 支持：

- `a_share`
- `prediction_market`
- `mixed`

`prediction_market` 团队允许 `paper_account_id` 为空；`a_share` 和 `mixed` 团队仍要求有效模拟盘账户。

## 默认团队与过滤器

默认预测市场投研团队名为“默认预测市场投研团队”，包含：

- 主持人
- 事件核验
- 市场结构
- 赔率/盘口
- 反方风险
- 结论整理

默认角色只使用 `web.search`、`meeting.*`、`prediction.*` 等观察/证据工具，不启用 `paper.*` 或其他交易/执行类工具。主持人具备重新搜索候选市场的工具，反方风险官具备盘口和价格历史工具，用于复核市场错配、流动性噪声和新闻是否已被赔率提前反映。后续唤醒计划可以作为 recap 建议输出，但不在讨论阶段通过工具执行。

预测市场团队的自定义角色同样受能力白名单约束：保存角色、从其他团队复制角色、或把已有团队切换为 `prediction_market` 时，服务端只保留 `web.search`、`meeting.*`、`prediction.*` 工具，以及预测市场研究、证据核验、盘口/结算风险、会议协作和观察型唤醒相关技能；不保留带仓位/交易风控语义的通用技能。前端角色编辑器使用同一白名单，避免把 A 股行情、模拟盘、自选池或交易执行能力配置进预测市场团队。同时会给自定义/复制角色 prompt 加入“预测市场角色边界”，使旧 A 股角色说明只能作为不违反边界的补充。默认预测市场团队如果来自旧版本数据，启动播种或设置向导动作会修正资产类型、清空模拟盘账户、补齐缺失默认角色，并刷新默认角色的预测市场职责、prompt、工具和技能，同时保留已配置的模型服务商、模型和启停状态。

默认预测市场消息过滤器名为“默认预测市场消息过滤器”，输出仍保持：

- `ignore`
- `observe`
- `meeting`

提示词要求额外输出 `related_prediction_markets`、`match_confidence` 和 `match_reason`。系统级匹配不会只依赖 LLM 输出，而是在过滤成功后通过 Polymarket 候选召回和本地打分保存可复核证据。

## 新闻匹配机制

流程：

1. 从新闻文本构造搜索 query。
2. 使用 Gamma 召回事件和市场：Polymarket event/events/market/markets URL（含无协议裸域名 URL）或 slug（含分享 fragment，大小写不敏感）优先归一化为小写 slug 后走 `/events/slug/{slug}`、`/markets/slug/{slug}` 精确定位；普通关键词走 `/public-search` 且必须使用完整 payload，以保留 market id、condition id 和 CLOB token ids。
3. 按 active、closed、restricted、endDate、liquidity、关键词重合、问题文本重合和市场活跃度打分。
4. 保存新闻片段、query、候选市场快照、分数构成和最终处置。
5. 对高置信匹配自动写入会议上下文和引用；中置信匹配进入人工确认。

本地缓存兜底排序必须先满足“用户显式搜的是什么”：exact market slug/external id/condition id 优先，其次是 exact event slug/external event id，再按部分 slug/title/question 匹配、活跃/关闭状态和 volume 排序。这样在 provider 不可用时，事件 URL 已缓存的子市场不会被高成交量但仅部分匹配的市场挤到前面。

自动匹配的 `scoreBreakdown` 必须包含 `overlap_terms`、overlap/news/market term 计数以及 event 身份字段，`candidateSnapshot` 使用稳定 snake_case 字段保存 market/event 身份、状态和流动性。预测市场 query 构造和 overlap 计分会剔除 A 股 6 位代码、A 股/证券/模拟盘/仓位/买卖等交易噪声；在识别到 A 股语境时，也会丢弃 `paper`、`order`、`position`、`share/stock/security` 等易被旧 A 股摘要带入的英文伪证据。`linked` 自动关联要求分数达到阈值且至少有 2 个不同关键词重合；只有 1 个重合词的高活跃市场最多进入 `review_required`，避免短新闻或泛词把错误市场直接带进预测会议。

阈值：

- `>= 0.75`：自动关联，可随消息触发会议。
- `0.45-0.75`：进入人工确认/观察。
- `< 0.45`：不保存关联。

预测市场候选匹配只会在订阅绑定 `prediction_market` 或 `mixed` 团队且消息未被过滤为 ignore 时运行；纯 A 股团队订阅不会保存预测市场候选。预测市场团队的过滤结果会在服务端强制清空 `related_symbols`，即使模型错误返回 A 股代码，消息摘要和后续会议触发也不会继承这些证券标的。

人工复核通过 `POST /prediction-matches/{matchId}/review` 完成，支持确认、驳回和状态修正。

预测市场关注列表必须始终绑定明确的 `prediction_market` 或 `mixed` 投研团队；读取和写入都会拒绝空团队、纯 A 股团队或不存在的团队，不提供跨团队全局关注列表。

## 会议接入

会议创建可携带 `predictionMarketIds`，应用层会把它写入会议 context event。`prediction_market` 团队固定按预测市场会议运行；`mixed` 团队只有当本次会议上下文或引用中实际存在预测市场 ID 时，才按预测市场会议运行并启用同样的 A 股隔离边界。消息触发预测市场会议时，会把自动关联的预测市场 ID 和匹配证据写入首条业务 system event，并创建 `prediction_market` 引用；该链路不会从过滤结果、消息聚合字段或正文兜底继承 A 股 `related_symbols`。

新增会议工具：

- `prediction.search_markets`
- `prediction.market_snapshot`
- `prediction.orderbook`
- `prediction.price_history`
- `prediction.related_matches`
- `prediction.watchlist`
- `prediction.upsert_watchlist`

`prediction.search_markets` 与 HTTP 搜索使用同一套 URL/slug 归一化和持久化流程，工具返回的是已落库的本地 `market_id`，并在已归属 event 的候选上暴露本地 `event_id`、`external_event_id`、`event_slug` 和 `event_title`；工具参数中记录 `original_query` 与 `normalized_query`。角色可以直接用本地 market id 继续调用 `prediction.market_snapshot`、`prediction.orderbook` 或 `prediction.price_history`，并用 event 身份字段审计 event URL 与子 market 的归属关系，避免只拿到外部候选而无法串联后续证据工具。

Gamma `/markets/slug/{slug}` 的真实返回可能把父 event 放在 market payload 的 `events` 数组里，同时 `eventId` 为空；解析器必须保存 embedded event，并把 market raw 写入 `event_id`，让后续本地持久化能把 direct market URL 搜索结果挂到正确 event。

`prediction.watchlist` 是只读工具，用于读取当前 prediction research team 已关注的 Polymarket market、本地 `market_id`、event slug/title、note 和 active 状态。默认只给主持人和结论整理员使用，便于判断是新增关注、更新 note、重新启用还是停用，避免重复关注或覆盖已有跟踪理由。

`prediction.upsert_watchlist` 是讨论阶段的延迟动作工具：角色只能提出把本地 `market_id` 加入/更新预测市场关注列表，真正写入发生在 moderator recap 的 `prediction_watchlist_actions`。它与 A 股 `watchlist_actions` 完全分离；预测市场会议中 `watchlist_actions` 仍视为 A 股自选池动作并被阻断。

`prediction.market_snapshot`、`prediction.orderbook` 和 `prediction.price_history` 都会暴露本地 event 身份字段与 `outcome_tokens`，把 outcome label、outcome price 和 CLOB token id 按 index 配对；盘口和价格历史结果还会标注当前查询 token 对应的 `outcome`、`outcome_index` 和 `outcome_price`，避免角色只看到裸 token id 后把 Yes/No 盘口方向读反。

HTTP `prediction-markets` 资源和前端预测市场页同样展示 `externalEventId`、`eventSlug`、`eventTitle`，搜索结果、关注列表和匹配治理表都应能直接看出子 market 属于哪个 Polymarket event，避免用户只看到本地 event 数字 ID 后无法判断 URL 搜索是否命中正确事件。

预测市场会议不得创建模拟盘订单；预测市场 recap 的动作边界是 `prediction_watchlist_actions`、观察、证据缺口和唤醒计划。`prediction_watchlist_actions` 必须使用本地 `market_id`，并需要 facts/inferences/citations 证据约束；A 股 `watchlist_actions` 和 `orders` 在预测市场会议中始终为空。唤醒计划只允许观察型 `time` 或 `event` 触发，event 触发必须使用可监控的 keywords、regex/pattern、decisions 或 channel filters；`prediction_market_ids` 只能作为上下文 ID，不能单独作为触发条件。系统会拒绝 indicator wake plan，以及 `code`、`symbol`、`ticker`、`related_symbols`、`symbols`、`codes` 等 A 股触发字段。

预测市场 recap 还有文本边界门禁：`topic`、`tags`、`summary`、`conclusion`、`facts`、`assumptions`、`inferences`、`evidence_gaps` 和 `citations` 不得包含 A 股、股票/证券、6 位 A 股/ETF 代码、模拟盘、买卖/仓位/下单或 A 股行情/模拟盘工具引用。托管会议会要求模型重写；旧式 recap 事件会写入 `prediction_market_recap_text_blocked` 审计事件并拒绝应用为最终结论。

预测市场会议可使用 `web.search` 进行公共来源核验，但默认使用 global/US 新闻搜索参数，并且不会把历史 context 中的 A 股 `related_symbols` 拼入搜索 query；A 股会议仍使用中国区新闻搜索参数和 related symbol query 扩展。这样避免 Polymarket 事件核验被 A 股区域新闻语境带偏。

预测市场团队的会议工具边界只允许 `meeting.references`、`meeting.transcript`、`web.search` 和 `prediction.*` 工具；不再按 `meeting.*` 前缀放行未来新增的通用会议工具。`meeting.references` 在 prediction meeting 中会保留 prediction-market 引用和带 prediction context 的历史会议引用，但会把非 prediction 历史会议的 note/topic/summary 快照标记为 `redacted=true` 并移除文本，避免手动链接的 A 股会议摘要污染后续角色发言和 moderator recap。

会议详情页的 trust report 会把 `prediction_watchlist` recap action 显示为“预测关注”，并优先展示本地 `market_id`、关注/停用状态和 note/reason；A 股自选仍显示为“自选”和证券代码。这样人工复核时不会把预测市场关注动作误读成 A 股自选或模拟盘动作。

成功执行的 `prediction_watchlist_actions` 会写入 `prediction_watchlist_updated` 工具结果事件，payload 带 `tool=prediction.upsert_watchlist`、本地 `market_id`、market question/slug、event slug/title、`source_meeting_id`、`source_meeting_event_id` 和 `source_role_key` 等身份字段；会议 trust report 把该事件计入结构化证据，会议详情页展示该证据并可跳到 `/prediction-markets?marketId=...&sourceMeetingId=...`。`prediction_watchlist_items` 同步保存 `sourceMeetingId`、`sourceMeetingEventId` 和 `sourceRoleKey`；预测市场关注列表展示来源会议并可跳回会议结论，手工更新关注不会清空已有来源，删除会议/团队时会清理失效来源，避免坏链接。

托管 prediction meeting 的角色发言在写入讨论上下文前也会执行文本边界门禁：如果模型把 A 股、证券代码、模拟盘、买卖/仓位/下单、`market.*` 或 `paper.*` 内容放进 analysis 或 tool_request，会先触发重写；若重写后仍越界，只记录 `prediction_market_role_analysis_blocked` 的干净占位发言，避免污染后续主持人总结和最终 recap。

## API

新增受保护端点：

- `GET /prediction-markets/search`
- `POST /prediction-markets/sync`
- `GET /prediction-events/{eventId}`
- `GET /prediction-markets/{marketId}`
- `GET /prediction-matches`
- `POST /prediction-matches/{matchId}/review`
- `GET /prediction-watchlist`
- `POST /prediction-watchlist`

已有契约扩展：

- `ResearchTeamAttributes.assetClass`
- nullable `ResearchTeamAttributes.paperAccountId`
- `MeetingStartDocument.attributes.predictionMarketIds`
- `IngestedMessageAttributes.relatedPredictionMarkets`
- `IngestedMessageAttributes.predictionMarketMatchStatus`

## 设置与运维

设置向导新增“预测市场”步骤，幂等创建默认预测团队和默认预测过滤器；通用“投研团队与角色”步骤的说明需要按资产类型区分，避免暗示预测市场团队也必须绑定模拟盘账户。

代理设置不新增独立开关，Polymarket Gamma/CLOB 请求复用 Market 模块代理；设置页语义为行情/预测市场。

`GET /ops/provider-health` 新增 `predictionMarket` 区块，展示 Polymarket endpoints、代理模块、会议 WebSocket 快照模式、本地同步样本状态和 WebSocket runner 状态。

## 验收

必须通过：

- `go generate ./...`
- `go test ./...`
- `go vet ./...`
- `golangci-lint run`
- `npm ci --prefix frontend`
- `npm run typegen:api --prefix frontend`
- `npm run build --prefix frontend`
- `docker compose config`（需要本机安装 Docker）
- 前端 `/prediction-markets` 视觉与交互验证

外部 Polymarket 实时连通性依赖部署网络，可通过预测市场同步 API 或 Market 代理测试验证。
