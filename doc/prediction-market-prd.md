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

默认角色只使用 `web.search`、`meeting.*`、`prediction.*` 等观察/证据工具，不启用 `paper.*` 或其他交易/执行类工具。后续唤醒计划可以作为 recap 建议输出，但不在讨论阶段通过工具执行。

默认预测市场消息过滤器名为“默认预测市场消息过滤器”，输出仍保持：

- `ignore`
- `observe`
- `meeting`

提示词要求额外输出 `related_prediction_markets`、`match_confidence` 和 `match_reason`。系统级匹配不会只依赖 LLM 输出，而是在过滤成功后通过 Polymarket 候选召回和本地打分保存可复核证据。

## 新闻匹配机制

流程：

1. 从新闻文本构造搜索 query。
2. 使用 Gamma `/public-search` 召回事件和市场。
3. 按 active、closed、restricted、endDate、liquidity、关键词重合、问题文本重合和市场活跃度打分。
4. 保存新闻片段、query、候选市场快照、分数构成和最终处置。
5. 对高置信匹配自动写入会议上下文和引用；中置信匹配进入人工确认。

阈值：

- `>= 0.75`：自动关联，可随消息触发会议。
- `0.45-0.75`：进入人工确认/观察。
- `< 0.45`：不保存关联。

人工复核通过 `POST /prediction-matches/{matchId}/review` 完成，支持确认、驳回和状态修正。

## 会议接入

会议创建可携带 `predictionMarketIds`，应用层会把它写入会议 context event。消息触发会议时，会把自动关联的预测市场 ID 和匹配证据写入首条业务 system event，并创建 `prediction_market` 引用。

新增会议工具：

- `prediction.search_markets`
- `prediction.market_snapshot`
- `prediction.orderbook`
- `prediction.price_history`
- `prediction.related_matches`

预测市场会议不得创建模拟盘订单；预测市场 recap 的动作边界是关注、观察、证据缺口和唤醒计划。

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

设置向导新增“预测市场”步骤，幂等创建默认预测团队和默认预测过滤器。

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
