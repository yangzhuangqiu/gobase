# app-web-hertz

`app-web-hertz` 把应用级 Controller、Filter 和 fallback handler 转换为 `web-hertz` 原生扩展，并提供可直接注册到 IOC 的托管 `web.Server`。

## 设计范围与目标

### In Scope

1. 定义 Controller、可选 RouteRegistrar、Filter、GroupedFilter、NoRouteHandler 和 NoMethodHandler 应用合同。
2. 支持方法名自动路由、逻辑分组共享 Filter、Controller 局部 Middleware 隔离和稳定排序。
3. 通过 `web.hertz.middleware` 配置化装配 Recovery、Request ID、CORS 和 Gzip。
4. 通过 Definition 工厂声明组件角色，`NewModule` 在 IOC Build 阶段自动注入完整组件集合和 `config.Manager`。
5. 让 IOC 构造、发布并托管唯一 `web.Server`，同时保留底层错误 cause。
6. 通过不含 OTel 类型的 `InstrumentationBundle` 原子安装 Hertz Option/Middleware，并桥接 Trace ID。

### Out of Scope

1. 不重新定义 `web.Server`、`webhertz.Config`、Hertz Handler、Router 或 RequestContext。
2. 不做包扫描或隐式 bean 发现；应用组件必须通过 Definition 工厂显式声明角色。
3. 不自动调用阻塞型 `Run`，不安装进程信号处理器。
4. 不创建或关闭配置 Manager、日志 Provider、Controller 或 Filter。
5. 不管理 Hertz 全局 hlog；日志桥接由 composition root 在 IOC Build 前完成。
6. 不默认启用 Recovery、Request ID、CORS、Gzip、access-log 或 pprof；标准能力必须逐项显式开启。
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
| `Option` | 声明路径风格、标准 typed policy 和 `web-hertz` 原生扩展 |
| `InstrumentationBundle` | 成对声明 Hertz 原生插桩 Option/Middleware，不拥有 Provider |
| `Module` | 在 IOC Build 前注册托管 Server bean |
| `NewModule` | 从 IOC 自动聚合依赖并发布 `web.Server` |
| `Runner` | 从 app Runtime 解析唯一 `web.Server` 并进入阻塞 Run |

## 最小使用

`app` 自动装配：

```go
app.Main(
	app.WithConfig(config.Defaults(defaultConfig())),
	app.WithBeanDefinitions(
		appwebhertz.DefineController(&OrdersController{}),
		appwebhertz.DefineFilter(&AuthFilter{}),
		appwebhertz.DefineFilter(&APIAuditFilter{}),
		appwebhertz.DefineNoRouteHandler(&JSONNotFound{}),
		appwebhertz.DefineNoMethodHandler(&JSONMethodNotAllowed{}),
	),
	app.WithModules(appwebhertz.NewModule("")),
	app.WithRunner(appwebhertz.Runner()),
)
```

`NewModule("")` 默认读取 `web.hertz`。`app` 拥有信号、取消和 Runtime Close；`Runner()` 只解析并运行唯一 Server，因此无需额外 Shutdown Hook。多 Server 监督仍由应用 composition root 负责。

## 公共入口与合同

```go
const DefaultScope = "web.hertz"

func NewModule(scope string, options ...Option) Module

func DefineController(controller Controller, options ...ioc.RegisterOption) ioc.Definition
func DefineFilter(filter Filter, options ...ioc.RegisterOption) ioc.Definition
func DefineNoRouteHandler(handler NoRouteHandler, options ...ioc.RegisterOption) ioc.Definition
func DefineNoMethodHandler(handler NoMethodHandler, options ...ioc.RegisterOption) ioc.Definition
func Runner() app.Runner

func WithServerOptions(options ...webhertz.Option) Option
func WithRoutePathStyle(style RoutePathStyle) Option
func WithRecoveryHandler(handler RecoveryHandler) Option
func WithRequestIDGenerator(generator RequestIDGenerator) Option
func WithCORSOriginValidator(validator CORSOriginValidator) Option
func WithGzipPredicate(predicate GzipPredicate) Option
func GetRequestID(request *app.RequestContext) string
func RequestPanicked(request *app.RequestContext) bool
```

`NewModule` 只创建可重放声明；`Register` 不读取配置或构造 Hertz，真实配置读取、完整组件聚合和 provider 构造发生在 IOC `Init`。Observer 与 Instrumentation Bundle 只作为 IOC 组件注入，Option 仅承载策略和 provider 扩展。

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
    router:
      redirectTrailingSlash: true
      redirectFixedPath: false
      handleMethodNotAllowed: false
      disablePrintRoute: false
    middleware:
      recovery:
        enabled: true
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

1. `NewModule` 的 scope trim 后为空时使用 `web.hertz`。
2. Module 在 IOC Init 阶段读取当前 Snapshot 一次；启动配置不热更新。
3. scope 缺失、解码失败、provider 或 Middleware 配置非法会让 IOC Build 失败；Access Log 启用但缺少 Request Log Observer 也会 fail-fast。
4. `WithServerOptions` 在 typed Config 之后应用，最终优先级由 `web-hertz` 保证。
5. CORS 不默认放开 Origin；静态列表为空时必须提供 `WithCORSOriginValidator`。
6. `allowOrigins: ["*"]` 与 `allowCredentials: true` 是非法安全组合。

Router 布尔值采用 optional bool：未配置时使用 `web-hertz` 固化默认值，显式 `false` 不会被默认回填覆盖。

标准链固定为：

```text
advanced outer Middleware
-> instrumentation Middleware（例如 OTel Server Span；按 Bundle 存在性启用）
-> Request ID
-> completion Observation（Access Log + Metrics fan-out）
-> Recovery
-> CORS
-> Gzip
-> global application Filters
-> grouped application Filters（仅命中逻辑分组时）
-> Controller-local Middleware
-> Routes / NoRoute / NoMethod
```

所有 feature 默认关闭。Instrumentation Bundle 只通过 IOC 对象存在性启用，不进入 YAML；Bundle 的 Hertz Option 与 Middleware 在 provider 构造前一起校验。typed policy 只替换启用后的策略，不隐式启用能力；需要改变固定顺序时，应关闭对应配置并通过 `WithServerOptions(webhertz.WithMiddleware(...))` 完整显式声明。

## 错误语义

| ErrorCode | 语义 |
|---|---|
| `app_web_hertz.config_invalid` | Server/Middleware 字段或安全组合非法 |
| `app_web_hertz.config_load_failed` | Manager、Snapshot、scope 解码或 provider 配置非法 |
| `app_web_hertz.declaration_invalid` | Option、Controller、Filter、Module 或 Registry 声明非法 |
| `app_web_hertz.server_build_failed` | Controller 计划或 `web-hertz` 构造失败 |
| `app_web_hertz.module_register_failed` | 托管 Server bean 无法注册到 IOC |
| `app_web_hertz.server_state_invalid` | 托管代理在 IOC Init 完成前被调用 |
| `app_web_hertz.server_resolve_failed` | App Runner 无法从 Runtime 解析唯一 `web.Server` |

IOC Build 会在外层追加自身的依赖、循环或 Init 错误码；app-web-hertz、config、IOC 和 web-hertz cause 均保留在错误链中。

## 运行时语义

1. IOC 依赖图保证 Controller/Filter/fallback handler 先初始化，托管 Server 后初始化。
2. Instrumentation Bundle 由 IOC 先初始化，Server bean 再一次性冻结其原生 Option、Middleware 和可选 Trace ID 元数据桥接。
3. Server bean 的 `Init` 只读取一次完整 Config，冻结标准链和路由计划后构造底层单周期 Server。
4. Build 后由 `Runner()` 或应用入口 Resolve `web.Server` 并调用 `Run`；Module 不启动隐藏 goroutine。
5. Container Close 逆序先停止 Server，再关闭 adapter/Provider owner；`app-web-hertz` 不直接 ForceFlush/Shutdown Provider。
6. `config.Manager` 为 borrowed 依赖，Server bean 不关闭它。
7. Controller/Filter 不得反向依赖 `web.Server`，否则 IOC 会报告循环依赖。

## 最佳实践

1. 使用 Controller/Filter 应用模型时通过 `NewModule` 进入 IOC；不使用 IOC 时直接使用 `web-hertz` 原生组装。
2. 使用 `DefineController`、`DefineFilter`、`DefineNoRouteHandler` 和 `DefineNoMethodHandler`，不要在 composition root 重复 `WithDeclare`；分组 Filter 同样使用 `DefineFilter`。
3. 单 Server 应用使用 `appwebhertz.Runner()`；多 Server 交给 composition root 监督。首次 Close context 必须覆盖完整优雅停止窗口。
4. hlog 在 Container Build 前安装，在 Container Close 后恢复，日志 Provider 最后关闭。
5. 自动方法名只承载稳定静态 URL；动态和兼容性敏感路由使用 `RegisterRoutes`。
6. Request ID 默认不信任入站值；只有代理链已经验证来源时才开启 `trustIncoming`。
7. CORS 使用精确 Origin；凭据模式禁止通配符。Gzip 仅处理完整内存响应，不用于流式/SSE/升级连接。

## Open Gaps / Future Improvements

1. `Runner()` 只支持按类型唯一解析的单 Server；多 Server 仍需由应用 composition root 显式 Resolve、监督和关闭。
2. 应用组件仍需显式 Definition，不做包扫描。
3. 启动配置不热更新，地址或 timeout 变化需要重建 Runtime。
4. 当前按类型只适合发布一个 `web.Server`；多 Server 应先定义具名运行策略。
5. Access Log/Metrics 通过完成态 Observation 接入，Trace 通过原子 Bundle 接入；本模块仍不创建 Logger、Registry、Provider、Exporter 或 Handler。

## File Map

1. `controller.go`、`route_registrar.go`、`filter.go`、`grouped_filter.go`、`no_*_handler.go`：应用扩展合同和 fallback 排序。
2. `route.go`、`group_path.go`、`route_path.go`、`route_path_style.go`、`filter_plan.go`：逻辑分组、Filter/路由计划和路径映射。
3. `configured_config.go`、`middleware_config.go`：完整配置模型、默认值和校验。
4. `middleware*.go`：固定标准链、typed policy 和四类私有 Middleware 实现。
5. `request_log_observer.go`、`request_observation.go`：Access Log 语义合同、过滤策略和完成态元数据桥接。
6. `instrumentation_bundle.go`：后端无关、原子 Option/Middleware 插桩合同。
7. `model.go`、`builder.go`、`options.go`：完整 IOC 组件快照、私有构造核心和策略 Option。
8. `definition.go`：组件角色到 IOC Definition 的原子工厂。
9. `module.go`：IOC Module 声明入口。
10. `managed_server.go`：IOC 注入、Init、Run/Shutdown 和 Close 适配。
11. `runner.go`：app 单 Server Runner 适配。
12. `config.go`：独立 scope 单次配置读取。
13. `errors.go`：稳定错误码和 cause 包装。
14. `*_test.go`：配置、Middleware、Observation、原子 Bundle、路由、IOC 依赖图和真实生命周期测试。

## 相关文档

1. [使用指南](../docs/guides/app-web-hertz-usage.md)
2. [架构设计](../docs/architecture/app-web-hertz-design.md)
3. [Quickstart](../examples/quickstart-app-web-hertz/README.md)
4. [web-hertz provider](../web-hertz/README.md)
