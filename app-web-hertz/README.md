# app-web-hertz

`app-web-hertz` 把应用级 Controller/Filter 转换为 `web-hertz` 原生扩展，并提供可直接注册到 IOC 的托管 `web.Server`。

## 设计范围与目标

### In Scope

1. 定义 Controller、可选 RouteRegistrar 和 Filter 应用合同。
2. 支持方法名自动路由、Controller 独立 RouterGroup 和 Filter 稳定排序。
3. 通过 `New` 使用显式值构造 `web.Server`。
4. 通过 `NewConfigured` 或 `web.hertz.middleware` 配置化装配 Recovery、Request ID、CORS 和 Gzip。
5. 通过 `NewModule` 在 IOC Build 阶段自动注入 Controller、Filter 和 `config.Manager`，并发布唯一 `web.Server`。
6. 让 IOC 托管 Server 的初始化和停止，同时保留底层错误 cause。
7. 通过不含 OTel 类型的 `InstrumentationBundle` 原子安装 Hertz Option/Middleware，并桥接 Trace ID。

### Out of Scope

1. 不重新定义 `web.Server`、`webhertz.Config`、Hertz Handler、Router 或 RequestContext。
2. 不做包扫描或隐式 bean 发现；Controller/Filter 必须显式声明对应接口。
3. 不自动调用阻塞型 `Run`，不安装进程信号处理器。
4. 不创建或关闭配置 Manager、日志 Provider、Controller 或 Filter。
5. 不管理 Hertz 全局 hlog；日志桥接由 composition root 在 IOC Build 前完成。
6. 不默认启用 Recovery、Request ID、CORS、Gzip、access-log 或 pprof；标准能力必须逐项显式开启。
7. 不创建 Prometheus Registry、OTel Provider、Exporter、诊断 Server 或配置订阅。

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `Controller` | 声明 GroupPath，并承载方法名自动路由 |
| `RouteRegistrar` | 为动态路径、通配符和复杂 Middleware 提供原生 Hertz 注册入口 |
| `Filter` | 直接实现 Hertz Middleware，并声明全局稳定顺序 |
| `Config` / `MiddlewareConfig` | 保留扁平 Server 配置，并定义标准 Middleware 装配意图 |
| `Option` | 声明路径风格、标准 typed policy 和 `web-hertz` 原生扩展 |
| `InstrumentationBundle` | 成对声明 Hertz 原生插桩 Option/Middleware，不拥有 Provider |
| `Module` | 在 IOC Build 前注册托管 Server bean |
| `New` | 从显式 Config、Controller、Filter 构造 `web.Server` |
| `NewConfigured` | 从完整 Server/Middleware Config 构造 `web.Server` |
| `NewModule` | 从 IOC 自动聚合依赖并发布 `web.Server` |

## 最小使用

显式构造：

```go
webServer, err := appwebhertz.New(
	webhertz.Config{Address: ":8080"},
	controllers,
	filters,
)
if err != nil {
	return err
}
return webServer.Run(ctx)
```

配置化显式构造：

```go
webServer, err := appwebhertz.NewConfigured(appwebhertz.Config{
	Server: webhertz.Config{Address: ":8080"},
	Middleware: appwebhertz.MiddlewareConfig{
		Recovery:  appwebhertz.RecoveryConfig{Enabled: true},
		RequestID: appwebhertz.RequestIDConfig{Enabled: true},
	},
}, controllers, filters)
```

IOC 自动装配：

```go
container.Register(manager,
	ioc.WithDeclare[config.Manager](),
	ioc.WithBorrowed(),
)
container.Register(controller, ioc.WithDeclare[appwebhertz.Controller]())
container.Register(filter, ioc.WithDeclare[appwebhertz.Filter]())

if err := appwebhertz.NewModule("").Register(container); err != nil {
	return err
}
if err := container.Build(ctx); err != nil {
	return err
}

webServer, err := ioc.ResolveAs[web.Server](container)
if err != nil {
	return err
}
return webServer.Run(ctx)
```

`NewModule("")` 默认读取 `web.hertz`。使用 `app-runtime` 时，Runtime 已把 `config.Manager` 注册为 borrowed bean，业务模块只需声明 Controller/Filter 和本 Module。

## 公共入口与合同

```go
const DefaultScope = "web.hertz"

func New(
	config webhertz.Config,
	controllers []Controller,
	filters []Filter,
	options ...Option,
) (web.Server, error)

func NewConfigured(
	config Config,
	controllers []Controller,
	filters []Filter,
	options ...Option,
) (web.Server, error)

func NewModule(scope string, options ...Option) Module

func WithServerOptions(options ...webhertz.Option) Option
func WithRoutePathStyle(style RoutePathStyle) Option
func WithRecoveryHandler(handler RecoveryHandler) Option
func WithRequestIDGenerator(generator RequestIDGenerator) Option
func WithCORSOriginValidator(validator CORSOriginValidator) Option
func WithGzipPredicate(predicate GzipPredicate) Option
func WithRequestLogObservers(observers ...RequestLogObserver) Option
func WithRequestObservers(observers ...webhertz.RequestObserver) Option
func WithInstrumentationBundles(bundles ...InstrumentationBundle) Option
func GetRequestID(request *app.RequestContext) string
func RequestPanicked(request *app.RequestContext) bool
```

`New` 是兼容入口，等价于标准 Middleware 全部关闭。`NewConfigured` 是不读取外部状态的完整显式入口。`NewModule` 只创建可重放声明；`Register` 不读取配置或构造 Hertz，真实构造发生在 IOC `Init`。

## Controller 与 Filter

Controller 的最小合同：

```go
type Controller interface {
	GroupPath() string
}
```

1. 每个 Controller 都使用独立 RouterGroup；空白和 `/` 都表示根 URL，但不共享 group Middleware。
2. 自动方法支持 `Get/Post/Put/Delete/Patch/Options/Head/Any`；`GetOrPostSearch` 为同一 Handler 注册多个 method。
3. 自动签名固定为 `func(context.Context, *app.RequestContext)`，默认把方法后缀映射为 kebab-case。
4. 动态路径使用可选 `RegisterRoutes(*route.RouterGroup) error`。

Filter 直接采用 Hertz Handler 签名：

```go
type Filter interface {
	Order() int
	DoFilter(context.Context, *app.RequestContext)
}
```

Order 越小越先执行；相同 Order 保持显式输入或 IOC 注册顺序。Filter 自行调用 `request.Next(ctx)` 继续链。

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
-> application Filters
-> Routes / NoRoute / NoMethod
```

所有 feature 默认关闭。Instrumentation Bundle 只通过 typed Option/IOC 对象存在性启用，不进入 YAML；Bundle 的 Hertz Option 与 Middleware 在 provider 构造前一起校验。typed policy 只替换启用后的策略，不隐式启用能力；需要改变固定顺序时，应关闭对应配置并通过 `WithServerOptions(webhertz.WithMiddleware(...))` 完整显式声明。

## 错误语义

| ErrorCode | 语义 |
|---|---|
| `app_web_hertz.config_invalid` | Server/Middleware 字段或安全组合非法 |
| `app_web_hertz.config_load_failed` | Manager、Snapshot、scope 解码或 provider 配置非法 |
| `app_web_hertz.declaration_invalid` | Option、Controller、Filter、Module 或 Registry 声明非法 |
| `app_web_hertz.server_build_failed` | Controller 计划或 `web-hertz` 构造失败 |
| `app_web_hertz.module_register_failed` | 托管 Server bean 无法注册到 IOC |
| `app_web_hertz.server_state_invalid` | 托管代理在 IOC Init 完成前被调用 |

IOC Build 会在外层追加自身的依赖、循环或 Init 错误码；app-web-hertz、config、IOC 和 web-hertz cause 均保留在错误链中。

## 运行时语义

1. IOC 依赖图保证 Controller/Filter 先初始化，托管 Server 后初始化。
2. Instrumentation Bundle 由 IOC 先初始化，Server bean 再一次性冻结其原生 Option、Middleware 和可选 Trace ID 元数据桥接。
3. Server bean 的 `Init` 只读取一次完整 Config，冻结标准链和路由计划后构造底层单周期 Server。
4. Build 后由应用入口 Resolve `web.Server` 并调用 `Run`；Module 不启动隐藏 goroutine。
5. Container Close 逆序先停止 Server，再关闭 adapter/Provider owner；`app-web-hertz` 不直接 ForceFlush/Shutdown Provider。
6. `config.Manager` 为 borrowed 依赖，Server bean 不关闭它。
7. Controller/Filter 不得反向依赖 `web.Server`，否则 IOC 会报告循环依赖。

## 最佳实践

1. 常规 IOC 应用使用 `NewModule`；单测、工具和不使用 IOC 的程序使用 `New`。
2. 始终用 `ioc.WithDeclare[Controller/Filter]` 显式声明组件接口。
3. 把 `Run` 留给应用 Runner 或 composition root，退出后再关闭 Container；首次 Close context 必须覆盖完整优雅停止窗口。
4. hlog 在 Container Build 前安装，在 Container Close 后恢复，日志 Provider 最后关闭。
5. 自动方法名只承载稳定静态 URL；动态和兼容性敏感路由使用 `RegisterRoutes`。
6. Request ID 默认不信任入站值；只有代理链已经验证来源时才开启 `trustIncoming`。
7. CORS 使用精确 Origin；凭据模式禁止通配符。Gzip 仅处理完整内存响应，不用于流式/SSE/升级连接。

## Open Gaps / Future Improvements

1. `app` 只提供通用 Runner/Shutdown Hook；多 Server 仍需由应用 composition root 显式 Resolve、监督和关闭。
2. Controller/Filter 注册仍需显式接口声明，不做包扫描。
3. 启动配置不热更新，地址或 timeout 变化需要重建 Runtime。
4. 当前按类型只适合发布一个 `web.Server`；多 Server 应先定义具名运行策略。
5. Access Log/Metrics 通过完成态 Observation 接入，Trace 通过原子 Bundle 接入；本模块仍不创建 Logger、Registry、Provider、Exporter 或 Handler。

## File Map

1. `controller.go`、`route_registrar.go`、`filter.go`：应用扩展合同。
2. `route.go`、`route_path.go`、`route_path_style.go`：路由计划和路径映射。
3. `configured_config.go`、`middleware_config.go`：完整配置模型、默认值和校验。
4. `middleware*.go`：固定标准链、typed policy 和四类私有 Middleware 实现。
5. `request_log_observer.go`、`request_observation.go`：Access Log 语义合同、过滤策略和完成态元数据桥接。
6. `instrumentation_bundle.go`：后端无关、原子 Option/Middleware 插桩合同。
7. `builder.go`、`options.go`：显式/IOC 共用构造核心、Observer/Bundle 聚合和 Option。
8. `module.go`：IOC Module 声明入口。
9. `managed_server.go`：IOC 注入、Init、Run/Shutdown 和 Close 适配。
10. `config.go`：独立 scope 单次配置读取。
11. `errors.go`：稳定错误码和 cause 包装。
12. `*_test.go`：配置、Middleware、Observation、原子 Bundle、路由、IOC 依赖图和真实生命周期测试。

## 相关文档

1. [使用指南](../docs/guides/app-web-hertz-usage.md)
2. [架构设计](../docs/architecture/app-web-hertz-design.md)
3. [Quickstart](../examples/quickstart-app-web-hertz/README.md)
4. [web-hertz provider](../web-hertz/README.md)
