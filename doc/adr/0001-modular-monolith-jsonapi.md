# ADR 0001：模块化单体与 JSON:API 契约

## 状态

已接受。

实现说明：本 ADR 描述的重构已经在当前 Go 代码库中完成。截至 2026-05-11 文档清理，`/api` 路由通过生成的 OpenAPI 包装层注册，请求/响应体使用严格的 JSON:API 文档，架构测试会保护下方包边界。

## 背景

TradingCopilot 已迁移为单个 Go 二进制程序。早期 Go 迁移保留了较宽的兼容层：领域结构体同时带有 GORM 标签，处理器返回兼容旧形状的 JSON，大多数用例仍位于较大的包中。这有助于建立功能对齐，但让后续变更更难推理。

## 决策

Go 系统保持模块化单体，并采用以下边界：

- `internal/domain/{auth,settings,ai,market,telegram,messaging,meeting,wake,paper}` 只包含领域类型和规则。不得依赖 GORM、HTTP 或外部客户端。
- `internal/app/{...}` 包含用例。方法接受 `context.Context`；写入类用例明确拥有事务边界。
- `internal/infra/persistence/gorm/{connect,model,repo,uow,migrate,...}` 包含数据库连接、GORM 模型、映射器、仓储、事务范围的工作单元、数据库支撑的适配器和 `AutoMigrate`。
- `internal/infra/{ai,marketdata,telegram,messaging,queue,proxy,security,config}` 包含基础设施适配器。
- `internal/transport/http` 包含路由、中间件、JSON:API 编码、OpenAPI 生成接口和 DTO 映射。
- `internal/composition` 是组合根。它负责连接应用用例、基础设施适配器、持久化实现、运行时服务和 HTTP transport，但不拥有业务行为。
- `cmd/tradingcopilot` 只负责加载配置、解析 CLI 命令和 flags，并委托给 `internal/composition` 或应用层运行时用例。

HTTP 路由表面保持以 `/api` 为根路径，除 SSE 流和静态前端资源外，请求/响应体都使用 JSON:API 资源文档。`api/openapi.yaml` 是生成 Go 接口和前端 TypeScript 类型的共享契约。

## 影响

运行时行为仍位于一个可部署二进制程序中，不引入微服务，也不破坏数据库数据。数据库访问必须留在 GORM 持久化边界内，外部协议客户端应保留在 `internal/infra/*` 适配器中。API fixtures 和前端调用必须与 JSON:API 文档和 `api/openapi.yaml` 保持一致。

依赖组装属于 `internal/composition`，因此 CLI 入口点应保持轻量。新增运行时命令时，只应在 `cmd/tradingcopilot` 中增加参数解析；依赖连接和跨模块启动编排应加入 `internal/composition`。

消息订阅在应用/API 边界保持与提供方无关。Telegram 频道采集、MTProto 登录/监听、bot 投递和 AI 过滤是 `internal/infra/messaging` 与 `internal/infra/telegram` 下的运行时适配器关注点；GORM 包只保留持久化模型、仓储、工作单元绑定和迁移兼容。消息持久化、过滤结果写入和会议创建继续由 `internal/app/messaging` 事务拥有。

## 测试

常规验证应包括：

- `go fmt ./...`
- `go vet ./...`
- `golangci-lint run`
- `go test ./...`
- `npm run typegen:api`
- `npm run build`
- `docker compose config`，需要在已安装 Docker 的环境中执行
