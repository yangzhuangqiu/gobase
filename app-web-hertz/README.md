# app-web-hertz

`app-web-hertz` 把应用级 Controller、Filter 和 fallback handler 转换为 `web-hertz` 原生扩展，并提供可直接注册到 IOC 的托管 `web.Server`。

## 设计范围与目标

### In Scope

1. 定义 Controller、可选 RouteRegistrar、Filter、GroupedFilter、NoRouteHandler 和 NoMethodHandler 应用合同。
2. 支持方法名自动路由、逻辑分组共享 Filter、Controller 局部 Middleware 隔离和稳定排序。
3. 通过 `web.hertz.middleware` 配置化装配 Recovery、Request ID、Access Log、CORS 和 Gzip。
4. 通过 Definition 工厂声明组件角色，`NewModule` 在 IOC Build 阶段自动注入完整组件集合和 `config.Manager`。
5. 让 IOC 构造、发布并托管唯一 `web.Server`，同时保留底层错误 cause。
6. 通过不含 OTel 类型的 `InstrumentationBundle` 原子安装 Hertz Option/Middleware，并桥接 Trace ID。
7. 从 IOC `LoggerFactory` 派生命名 Logger，记录组装摘要、内建 Middleware、Access Log，并向 `web-hertz` 注入实例 Logger。

### Out of Scope

1. 不重新定义 `web.Server`、`webhertz.Config`、Hertz Handler、Router 或 RequestContext。
2. 不做包扫描或隐式 bean 发现；应用组件必须通过 Definition 工厂显式声明角色。
3. 不自动调用阻塞型 `Run`，不安装进程信号处理器。
4. 不创建或关闭配置 Manager、日志 Provider、Controller 或 Filter。
5. 不管理 Hertz 全局 hlog，也不创建或关闭日志 Provider；全部自有日志只借用 IOC `LoggerFactory`。
6. 不默认启用 Request ID、Access Log、CORS、Gzip 或 pprof；Recovery 作为安全基线默认启用，可显式关闭。
7. 不创建 Prometheus Registry、OTel Provider、Exporter、诊断 Server 或配置订阅。
8. 不提供脱离 IOC 的 Controller/Filter 显式构造入口；原生 Hertz 场景直接使用 `web-hertz`。

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `Controller` | 声明 GroupPath，并承载方法名自动路由 |
| `RouteRegistrar` | 为动态路径、通配符和复杂 Middleware 提供原生 Hertz 注册入口 |
| `Filter` | 直接实现 Hertz Middleware，并声明全局稳定顺序 |
| `GroupedFilter` | 通过 GroupPath 绑定共享逻辑分组，不进入全局链 |
| `NoRouteHandler` / `NoMethodHandler` | 声明全局 404/405 fallback 链和稳定顺序 |
| `Define*` | 把应用组件与 IOC 接口声明绑定为原子 Definition |
| `Config` / `MiddlewareConfig` | 保留扁平 Server 配置，并定义标准 Middleware 装配意图 |
| Logger 装配 | 从必需的 IOC `LoggerFactory` 一次派生 assembly、middleware、provider 和 request Logger |
| 内建 Access Log | 使用 `web.hertz.request` Logger，把完成态快照映射为安全结构化请求事件 |
| `Option` | 声明路径风格、标准 typed policy 和 `web-hertz` 原生扩展 |
| `InstrumentationBundle` | 成对声明 Hertz 原生插桩 Option/Middleware，不拥有 Provider |
| `Module` | 在 IOC Build 前注册托管 Server bean |
| `NewModule` | 从 IOC 自动聚合依赖并发布 `web.Server` |
| `Workload` | 从 app Runtime 解析唯一 `web.Server`，声明 Run/Shutdown 语义 |

## 最小使用

`app` 自动装配：

```go
app.Main(
	app.WithBeanDefinitions(
		appwebhertz.DefineController(&OrdersController{}),
		appwebhertz.DefineFilter(&AuthFilter{}),
		appwebhertz.DefineFilter(&APIAuditFilter{}),
		appwebhertz.DefineNoRouteHandler(&JSONNotFound{}),
		appwebhertz.DefineNoMethodHandler(&JSONMethodNotAllowed{}),
	),
	app.WithModules(appwebhertz.NewModule()),
	app.WithWorkloads(appwebhertz.Workload()),
)
```

默认 `web.hertz` config scope 缺失时，`NewModule()` 直接使用 `DefaultConfig()`，因此最小应用不需要配置文件。自定义配置路径使用 `WithConfigScope("services.api")`；该路径缺失时仍 fail-fast，避免部署拼写错误被默认值掩盖。`app` 拥有信号、Workload 收敛和 Runtime Close；`Workload()` 显式桥接唯一 Server 的 Run/Shutdown，因此无需额外 Shutdown Hook。本模块的公共边界是“每个 Runtime 一个业务 Hertz Server”；多 Server 应拆分 Runtime，或使用独立的专用 Workload/Resource，而不是重复安装本 Module。

## 公共入口与合同

```go
const DefaultConfigScope = "web.hertz"

func NewModule(options ...Option) Module

func DefineController(controller Controller, options ...ioc.RegisterOption) ioc.Definition
func DefineFilter(filter Filter, options ...ioc.RegisterOption) ioc.Definition
func DefineNoRouteHandler(handler NoRouteHandler, options ...ioc.RegisterOption) ioc.Definition
func DefineNoMethodHandler(handler NoMethodHandler, options ...ioc.RegisterOption) ioc.Definition
func Workload() app.Workload

func WithConfigScope(configScope string) Option
func WithServerOptions(options ...webhertz.Option) Option
func WithRoutePathStyle(style RoutePathStyle) Option
func WithRecoveryHandler(handler RecoveryHandler) Option
func WithRequestIDGenerator(generator RequestIDGenerator) Option
func WithCORSOriginValidator(validator CORSOriginValidator) Option
func WithGzipPredicate(predicate GzipPredicate) Option
func GetRequestID(request *app.RequestContext) string
func RequestPanicked(request *app.RequestContext) bool
```

`NewModule` 只创建可重放声明；`Register` 不读取配置或构造 Hertz，真实配置读取、Logger 派生、完整组件聚合和 provider 构造发生在 IOC `Init`。`LoggerFactory` 是必需 IOC 依赖；app Runtime 始终注册默认或已配置的 Factory。通用 Observer 与 Instrumentation Bundle 仍作为 IOC 组件注入。

## Controller、Filter 与 fallback handler

Controller 的最小合同：

```go
type Controller interface {
	GroupPath() string
}
```

1. GroupPath 标识逻辑分组；空白和 `/` 统一为根逻辑分组 `/`，相同路径共享分组 Filter。
2. 自动方法支持 `Get/Post/Put/Delete/Patch/Options/Head/Any`；`GetOrPostSearch` 为同一 Handler 注册多个 method。
3. 自动签名固定为 `func(context.Context, *app.RequestContext)`，默认把方法后缀映射为 kebab-case。
4. 动态路径使用可选 `RegisterRoutes(*route.RouterGroup) error`。

Filter 直接采用 Hertz Handler 签名：

```go
type Filter interface {
	Order() int
	DoFilter(context.Context, *app.RequestContext)
}

type GroupedFilter interface {
	Filter
	GroupPath() string
}
```

是否全局由接口类型决定：普通 `Filter` 注册到 `engine.Use`；`GroupedFilter` 注册到 GroupPath 对应的缓存基础分组，即使 GroupPath 是 `/` 也不会升级为全局 Middleware。每个 Controller 从基础分组派生 `Group("")`，因此继承共享 Filter，但 `RegisterRoutes` 中的 `router.Use` 只影响自身。Order 越小越先执行；相同 Order 保持 IOC 注册顺序。

`NoRouteHandler` 与 `NoMethodHandler` 同样通过 `Order()` 定义全局 fallback 顺序，并分别实现 `HandleNoRoute` / `HandleNoMethod`。相同 Order 保持 Definition 注册顺序；未声明时完全保留 Hertz 默认行为。

## 配置模型

`Config.Server` 通过 YAML inline 保留原有扁平字段，新增标准能力只进入 `middleware` 子段：

```yaml
web:
  hertz:
    address: ":8080"
    readTimeout: 3m
    idleTimeout: 3m
    maxRequestBodySize: 4194304
    maxHeaderBytes: 1048576
    exitWaitTime: 5s
    shutdown:
      handlerCompletion: bestEffort
    router:
      redirectTrailingSlash: true
      redirectFixedPath: false
      handleMethodNotAllowed: false
      disablePrintRoute: false
    middleware:
      recovery:
        disabled: false
        response: json
      requestId:
        enabled: true
        header: X-Request-ID
        trustIncoming: false
      accessLog:
        enabled: true
        skipPaths: [/healthz, /readyz]
        slowThreshold: 500ms
      cors:
        enabled: true
        allowOrigins: ["https://example.com"]
        allowMethods: [GET, POST, OPTIONS]
        allowHeaders: [Authorization, Content-Type]
        allowCredentials: true
        maxAge: 12h
      gzip:
        enabled: true
        level: -1
        minLength: 1024
        excludedContentTypes: [image/, application/zip]
```

1. 未声明 `WithConfigScope` 时使用 `web.hertz`；显式 config scope trim 后为空同样回退该默认值；该默认 config scope 缺失时直接采用 `DefaultConfig()`。
2. Module 在 IOC Init 阶段读取当前 Snapshot 一次；启动配置不热更新。
3. 自定义 config scope 缺失、已存在 config scope 解码失败、provider、Middleware 配置非法或缺少 `LoggerFactory` 会让 IOC Build fail-fast。
4. `WithServerOptions` 在 typed Config 之后应用，最终优先级由 `web-hertz` 保证。
5. CORS 不默认放开 Origin；静态列表为空时必须提供 `WithCORSOriginValidator`。
6. `allowOrigins: ["*"]` 与 `allowCredentials: true` 是非法安全组合。

Router 布尔值采用 optional bool：未配置时使用 `web-hertz` 固化默认值，显式 `false` 不会被默认回填覆盖。

Recovery 是默认启用的 panic 隔离安全基线，只有 `disabled: true` 才关闭；Request ID、
Access Log、CORS 和 Gzip 默认关闭。其中 Access Log 直接借用 IOC 根 `LoggerFactory`，不依赖 Prometheus、OTel
或 `app-observe-webhertz`。它仍可作为运行诊断信号，并在 OTel Bundle 存在时通过
完成事件中的 Trace ID 与 Span 关联。

标准链固定为：

```text
advanced outer Middleware
-> instrumentation Middleware（例如 OTel Server Span；按 Bundle 存在性启用）
-> Request ID
-> 请求完成事件（Access Log + IOC Observer fan-out）
-> Recovery
-> CORS
-> Gzip
-> global application Filters
-> grouped application Filters（仅命中逻辑分组时）
-> Controller-local Middleware
-> Routes / NoRoute / NoMethod
```

除 Recovery 安全基线外，所有 feature 默认关闭。Instrumentation Bundle 只通过 IOC 对象存在性启用，不进入 YAML；Bundle 的 Hertz Option 与 Middleware 在 provider 构造前一起校验。typed policy 只替换对应策略，不隐式启用其他能力；需要改变固定顺序时，应显式关闭对应配置并通过 `WithServerOptions(webhertz.WithMiddleware(...))` 完整声明。

## 错误语义

| ErrorCode | 语义 |
|---|---|
| `app_web_hertz.config.invalid` | Server/Middleware 字段或安全组合非法 |
| `app_web_hertz.config.load_failed` | Manager、Snapshot、config scope 解码或 provider 配置非法 |
| `app_web_hertz.declaration.invalid` | Option、Controller、Filter、Module 或 Registry 声明非法 |
| `app_web_hertz.route.register_failed` | Controller 手工路由注册失败，并保留 Registrar cause |
| `app_web_hertz.middleware.execution_failed` | Request ID、Gzip 等内建 Middleware 运行失败；通过结构化日志观测 |
| `app_web_hertz.server.build_failed` | Controller 计划或 `web-hertz` 构造失败 |
| `app_web_hertz.module.register_failed` | 托管 Server bean 无法注册到 IOC；同一 Runtime 重复 Module 也在此失败 |
| `app_web_hertz.server.state_invalid` | 托管代理在 IOC Init 完成前被调用 |
| `app_web_hertz.server.resolve_failed` | App Workload 无法从 Runtime 解析唯一 `web.Server` |

IOC Build 会在外层追加自身的依赖、循环或 Init 错误码；app-web-hertz、config、IOC 和 web-hertz cause 均保留在错误链中。

## 运行时语义

1. IOC 依赖图保证 Controller/Filter/fallback handler 先初始化，托管 Server 后初始化。
2. Instrumentation Bundle 由 IOC 先初始化，Server bean 再一次性冻结其原生 Option、Middleware 和可选 Trace ID 元数据桥接。
3. Server bean 的 `Init` 只读取一次完整 Config，冻结标准链和路由计划后构造底层单周期 Server。
4. Build 后由 `Workload()` Resolve 唯一 `web.Server` 并声明 `Run/Shutdown`；Module 不启动隐藏 goroutine。
5. 同一 bean 同时发布 `web.StartupServer`；高级组合层可调用 `WaitStarted(ctx)` 等待首次启动成功或稳定失败。
6. app 先等待 Workload Run 收敛，Container Close 再逆序释放 Server Resource 与 adapter/Provider owner；默认 `bestEffort` 保持有界尽力关闭，需要在 Hertz `exitWaitTime` 后继续等待 Handler 时显式选择 `required`。
7. `config.Manager` 为 borrowed 依赖，Server bean 不关闭它。
8. Controller/Filter 不得反向依赖 `web.Server`，否则 IOC 会报告循环依赖。

## 最佳实践

1. 使用 Controller/Filter 应用模型时通过 `NewModule` 进入 IOC；不使用 IOC 时直接使用 `web-hertz` 原生组装。
2. 使用 `DefineController`、`DefineFilter`、`DefineNoRouteHandler` 和 `DefineNoMethodHandler`，不要在 composition root 重复 `WithDeclare`；分组 Filter 同样使用 `DefineFilter`。
3. 当前 Runtime 使用唯一 `NewModule()` + `appwebhertz.Workload()`；第二个业务 Server 使用独立 Runtime 或专用适配模块。
4. Hertz 应用使用 `apphlogzap.Wrap(applogzap.New(...))` 装饰应用 Logging，在 Container Build 前安装 hlog，在 Container Close 后恢复，日志 Provider 最后关闭。
5. 自动方法名只承载稳定静态 URL；动态和兼容性敏感路由使用 `RegisterRoutes`。
6. Request ID 默认不信任入站值；只有代理链已经验证来源时才开启 `trustIncoming`。
7. CORS 使用精确 Origin；凭据模式禁止通配符。Gzip 仅处理完整内存响应，不用于流式/SSE/升级连接。

## Open Gaps / Future Improvements

1. 单 Runtime 单业务 Server 是当前明确边界，不提供具名多 Server 解析；独立诊断 Server 应由专用 Workload/Resource 承载。
2. 应用组件仍需显式 Definition，不做包扫描。
3. 启动配置不热更新，地址或 timeout 变化需要重建 Runtime。
4. Access Log 通过请求完成事件接入；通用 Observer 与原子 Bundle 只由本模块聚合，Prometheus/OTel 后端适配由独立模块负责。本模块只从根 `LoggerFactory` 派生实例 Logger，不创建 Registry、Provider 或 Exporter。

## File Map

1. `controller.go`、`route_registrar.go`、`filter.go`、`grouped_filter.go`、`no_*_handler.go`：应用扩展合同和 fallback 排序。
2. `route.go`、`automatic_route.go`、`group_path.go`、`route_path.go`、`route_path_style.go`、`filter_plan.go`：逻辑分组、Controller/Filter 计划、自动路由协议和路径映射。
3. `config.go`、`middleware_config.go`：完整配置模型、默认值、跨能力解析调度与独立 config scope 单次读取。
4. `middleware*.go`：固定标准链、typed policy，以及各能力的配置、校验和私有 Middleware 实现。
5. `logging.go`：命名 Logger 派生、组装摘要和安全计数字段。
6. `access_log.go`、`request_observation.go`：内建 Access Log 配置、映射、过滤策略和完成态元数据桥接。
7. `instrumentation_bundle.go`：后端无关的原子 Option/Middleware 插桩合同、校验与构造计划。
8. `model.go`、`builder.go`、`options.go`：完整 IOC 组件快照、私有构造核心和策略 Option。
9. `definition.go`：组件角色到 IOC Definition 的原子工厂。
10. `module.go`：IOC Module 声明入口。
11. `managed_server.go`、`managed_server_test.go`：IOC 注入、Init、Run/Shutdown、Close 适配及托管生命周期测试。
12. `workload.go`：唯一 Server 的 Workload Run/Shutdown 适配。
13. `errors.go`：稳定错误码和 cause 包装。
14. `app_lifecycle_integration_test.go`：真实 App + Hertz 请求排空、Workload 收敛和 IOC 资源关闭顺序。
15. 其余 `*_test.go`：配置、Middleware、Observation、日志、原子 Bundle、路由、IOC 依赖图和生命周期合同测试。

## 相关文档

1. [使用指南](../docs/guides/app-web-hertz-usage.md)
2. [架构设计](../docs/architecture/app-web-hertz-design.md)
3. [Quickstart](../examples/quickstart-app-web-hertz/README.md)
4. [web-hertz provider](../web-hertz/README.md)
5. [可观测性入门](../docs/guides/observability-primer.md)
6. [Web 可观测性使用指南](../docs/guides/web-observability.md)
7. [高级可观测性 Quickstart](../examples/quickstart-app-observe-webhertz/README.md)
8. [Web 可观测性提案](../docs/architecture/web-observability-proposal.md)
