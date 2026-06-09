# 前端交互指南

TradingCopilot 前端页面使用 Vue 3 和 Element Plus。工作流页面应保持信息密度、可预期性和面向操作的体验。

## 列表

- 数据列表在网络刷新期间必须展示加载状态（loading state）。
- 空状态应描述当前列表状态，而不是使用说明。
- 宽屏桌面表格必须提供移动端卡片视图（mobile card view），或放入 `.table-scroll` 容器。
- 可点击的选中行或卡片必须展示持久的选中反馈，不能只依赖悬停反馈。
- 高增长列表必须使用基于游标的分页（cursor pagination），包括 `page[limit]`、`page[cursor]`、`links.next` 和 `meta.nextCursor`；客户端必须把游标视为不透明的透传值。

## 表单与对话框

- 业务对话框必须使用响应式宽度，并在移动端全屏显示（fullscreen on mobile）。
- 移动端表单必须使用顶部对齐标签。
- 保存按钮应在请求进行中展示加载状态。

## 操作

- 创建、更新、删除、切换、重置和测试操作必须提供成功或错误反馈。
- 会删除数据、取消工作或重置默认值的危险操作（Dangerous actions）必须要求确认。
- API 错误消息应使用 `apiErrorText`；视图文件不应定义本地 `errorText` 或 `apiErrorMessage` 辅助函数。

## 认证会话

- 前端默认把当前 bearer token 存在 `sessionStorage`，不得新增持久化 `localStorage` token 写入。
- 读取 token 时必须清理旧版 `localStorage` key，避免升级后留下长期有效的旧会话。
- 全局导航必须提供退出入口；退出应调用 `POST /auth/logout` 撤销当前后端会话，再清理本地 token 并返回登录页。

## 易变数据（Volatile Data）

- 记录可能由后台 worker 创建或修改的页面必须使用共享的 `useAutoRefresh` 组合式函数。
- 浏览器标签页隐藏时自动刷新必须暂停，并在标签页再次可见时刷新一次。
- 后台刷新不得清空当前过滤条件、置空当前表格，也不得造成整页加载闪烁。
- 使用游标分页的易变列表，在最新模式下只能刷新最新页。加载更早记录会进入历史模式（history mode），显示可见提示，并暂停自动刷新，直到用户返回最新模式。
- 视图文件不得直接创建原始 `setInterval` 定时器或 `EventSource` 流；生命周期清理由共享组合式函数负责。

## 日志（Logs）

- 日志视图必须使用 `useAutoRefresh`，提供表格和移动端卡片兜底视图，并使用详情抽屉（detail drawer）展示结构化字段。
- 日志视图不得展示请求体；后端日志载荷必须对 token、secret、password、hash、session 和 key 字段做脱敏。
- 在设置页编辑的日志配置必须说明需要重启（restart-required）。
- HTTP 日志消息必须是人类可读的请求摘要，而不是固定的完成短语。
- 日志条目必须暴露 `event`、`group`、`method`、`status`、可空的 `durationMs` 和 `noise`；日志视图必须提供这些字段的过滤器。
- 日志视图必须把 `durationMs: null` 和设置为 `not_applicable` 的字符串字段显示为不适用，而不是零或空值。
- 日志视图默认必须隐藏噪声较大的静态资源、前端和日志查看器请求，并提供显式控制项用于包含这些记录。

## 提供方无关消息

- 消息订阅源列表必须展示提供方、启用状态、过滤器/团队绑定、轮询间隔、上次采集时间、下次采集时间，以及可用时的上次采集错误。
- 提供方专属凭据应放在单独的配置区域；消息源增删改查必须保持与提供方无关。
- 提供方配置区域只展示有可编辑凭据的提供方。对于契约上有意无需凭据的提供方，不要渲染占位凭据标签页（Do not render placeholder credential tabs）。
