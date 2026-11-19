# App Web Hertz 使用指南

本文说明如何让 IOC 自动聚合 Controller、Filter、fallback handler 和观测组件，并托管 Hertz Server 生命周期。

## 1. 能力与入口

| 能力 | 解决的问题 | 启用入口 | 默认状态 | 业务入口 | 不适用场景 |
|---|---|---|---|---|---|
| IOC 自动装配 | 从依赖图聚合组件并统一关闭 | `NewModule` + `Register` | 需显式声明 | `ioc.ResolveAs[web.Server]` | Build 后动态加组件 |
| App 生命周期整合 | 声明唯一 Server 的 Run/Shutdown，由 app 负责收敛与 Runtime Close | `appwebhertz.Workload()` | 需显式启用 | `app.WithWorkloads` | 无 IOC 的原生 Server |
| 自动路由 | 简单静态方法到 URL 映射 | Controller 导出方法 | Kebab | Hertz Handler | 动态路径或复杂链 |
| 原生路由 | 保留 Hertz RouterGroup 完整能力 | `RouteRegistrar` | 可选 | `RegisterRoutes` | 框架无关路由 DSL |
| 全局 Filter | 提供稳定 Middleware 顺序 | `Filter` 接口声明 | 无默认 Filter | `DoFilter` | 分组策略 |
| 分组 Filter | 同一 GroupPath 的 Controller 共享策略 | `GroupedFilter` 接口声明 | 无默认 Filter | `GroupPath` + `DoFilter` | 全局策略 |
| 标准 Middleware | Recovery、Request ID、Access Log、CORS、Gzip | `web.hertz.middleware` | Recovery 默认启用，其余关闭 | Config + typed policy | 任意动态执行顺序 |
| 完成态 Metrics | 有限基数请求指标 | IOC `webhertz.RequestObserver` | 未注入即关闭 | Observer bean | 在 Config 中创建 Registry |
| 原生扩展 | 透传 Hertz/server/provider 能力 | `WithServerOptions` | 无额外 Option | `web-hertz.Option` | Registry 等 provider 明确拒绝能力 |

只需要 Hertz 生命周期和原生路由函数时直接使用 `web-hertz`。需要 Controller/Filter 应用模型时使用本模块。

### 1.1 运行 Quickstart

```powershell
./scripts/go.ps1 -C examples run ./quickstart-app-web-hertz
```

启动后访问 `GET /api/hello`、`POST /api/hello` 和 `GET /api/users/gobase`。完整关闭顺序见 [Quickstart](../../examples/quickstart-app-web-hertz/README.md)。

`app-web-hertz` 不提供脱离 IOC 的 Controller/Filter 构造入口。测试、工具或无 IOC 程序若只需要 Hertz 生命周期和原生路由，应直接使用 `web-hertz.New`。

## 2. IOC 自动装配

### 2.1 配置和组件声明

Module 默认读取独立 config scope：

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
```

Server 字段仍处于原有扁平 `web.hertz` config scope；关闭合同、Router 和标准能力分别位于 `shutdown`、`router`、`middleware` 子段。`handlerCompletion` 默认 `bestEffort`，可显式选择 `required`；Router 布尔值支持未配置、显式 true 和显式 false 三态。

普通 IOC Container 需要显式注册 borrowed Manager、组件 Definitions 和 Module：

```go
if err := container.Register(
	manager,
	ioc.WithDeclare[config.Manager](),
	ioc.WithBorrowed(),
); err != nil {
	return err
}
if err := container.RegisterDefinition(
	appwebhertz.DefineController(ordersController),
); err != nil {
	return err
}
if err := container.RegisterDefinition(
	appwebhertz.DefineFilter(requestIDFilter),
); err != nil {
	return err
}
if err := container.Register(
	logProvider.Factory(),
	ioc.WithDeclare[baselog.LoggerFactory](),
	ioc.WithBorrowed(),
); err != nil {
	return err
}
if err := appwebhertz.NewModule().Register(container); err != nil {
	return err
}
```

不声明 `WithConfigScope` 时使用 `web.hertz`；该默认 config scope 缺失时直接使用
`appwebhertz.DefaultConfig()`。自定义路径通过 Option 声明，并在缺失时 fail-fast：

```go
appwebhertz.NewModule(appwebhertz.WithConfigScope("services.api"))
```

Module Register 只添加一个托管 bean，不读取 Snapshot、不构造 Hertz、不启动 goroutine。真实配置读取和 Server 构造发生在 Container Build 的 Init 阶段。

### 2.2 Build、Resolve 和 Run

```go
if err := container.Build(ctx); err != nil {
	return err
}

webServer, err := ioc.ResolveAs[web.Server](container)
if err != nil {
	return err
}
return webServer.Run(runCtx)
```

IOC 自动注入 `[]Controller`、`[]Filter`、`[]NoRouteHandler` 和 `[]NoMethodHandler`。切片候选按注册顺序稳定排列；Filter 和 fallback handler 随后分别按 Order 稳定排序。Module 不自动 Run，因为阻塞型工作负载应由应用入口协调 context、退出码和并发任务。

使用 `app` 时，常规单 Server 应用只需要声明组件 Definitions、Server Module 和 Workload：

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

Runtime 已注册 borrowed `config.Manager`；未声明 `web.hertz` 时 Server 使用完整内建默认值。`appwebhertz.Workload()` 解析唯一 `web.Server`：app 并发执行 `Run`，关闭时调用 `Shutdown`，并继续等待 `Run` 真正返回，之后才关闭 IOC Runtime。本模块明确限定每个 Runtime 一个业务 Hertz Server；第二个 Server 应使用独立 Runtime 或专用 Workload/Resource。

同一托管 bean 还发布 `web.StartupServer`，服务注册等高级组合层可以等待首次启动结果：

```go
startupServer, err := ioc.ResolveAs[web.StartupServer](container)
if err != nil {
	return err
}
if err := startupServer.WaitStarted(startupCtx); err != nil {
	return err
}
```

`WaitStarted` 表示当前单周期 Server 已被 Hertz 确认为 running；本模块默认使用的官方 transport 同时确认 listener 已存在。它广播并缓存首次成功或失败，单个等待者超时不会取消 Server，也不会污染其他等待者。启动成功后即使 Server 随后停止，历史启动结果仍保持成功；它不是 readiness 或持续健康检查。若通过高级 Option 替换自定义 transport，调用方必须确认其 `IsRunning` 具备同等语义。

### 2.3 Option 归并

| 入口 | 默认值 | 重复规则 | 非法值 | 生效阶段 |
|---|---|---|---|---|
| `WithConfigScope` | `web.hertz` | 后声明覆盖前声明 | 空白回退默认 | IOC Init |
| `WithRoutePathStyle` | Kebab | 后者覆盖前者 | 不支持的枚举 fail-fast | 构造计划 |
| `WithServerOptions` | 无 | 每次追加 | 任一 nil fail-fast | provider 构造 |
| `WithRecoveryHandler` | 内建安全响应 | 后者覆盖 | nil fail-fast | Recovery 未禁用时 |
| `WithRequestIDGenerator` | crypto/rand 128-bit | 后者覆盖 | nil fail-fast；运行期空值/错误返回 500 | Request ID 启用后 |
| `WithCORSOriginValidator` | 无 | 后者覆盖并扩展静态列表 | nil 或启用后无任何 Origin 策略 fail-fast | CORS 构造/请求期 |
| `WithGzipPredicate` | 基础协议规则 | 后者覆盖 | nil fail-fast | 最终响应完成后 |
| nil `Option` | 不适用 | 不适用 | `app_web_hertz.declaration.invalid` | Registry 修改前 |

## 3. Controller

```go
func (this *OrdersController) GroupPath() string {
	return "/orders"
}

// GetOrPostSearch 同时注册 GET 和 POST /orders/search。
func (this *OrdersController) GetOrPostSearch(
	ctx context.Context,
	request *app.RequestContext,
) {
	// 处理查询
}

// RegisterRoutes 在 GroupPath 已生效的 group 上注册动态路由。
func (this *OrdersController) RegisterRoutes(router *route.RouterGroup) error {
	router.GET("/:id", this.get)
	return nil
}
```

规则：

1. GroupPath 是逻辑分组标识；空白或 `/` 统一为 `/`，同路径共享分组 Filter。
2. 自动动词支持 `Get/Post/Put/Delete/Patch/Options/Head/Any`。
3. `GetOrPostSubmit` 使用同一 Handler 注册多个 method；不同 Handler 使用 `GetSubmit` 和 `PostSubmit`。
4. 自动签名固定为 `func(context.Context, *app.RequestContext)`。
5. 每个 Controller 从共享基础分组派生无路径子分组；`RegisterRoutes` 先执行，`router.Use` 作用于自身全部路由但不污染同组其他 Controller。
6. `/:id`、`/*path`、多 Handler 和复杂 method 组合使用原生注册，不扩展方法名 DSL。
7. Controller 按 IOC 注册顺序，自动方法按导出方法名字典序。
8. 同一 method + path 冲突属于编程错误，Hertz panic 会补充 `stage=register_routes` 后继续传播。

### 3.1 自动方法映射

| Controller 方法 | 注册结果 | 说明 |
|---|---|---|
| `GetOrder` | `GET /order` | 单 method |
| `GetOrPostOrder` | `GET` + `POST /order` | 同一 Handler |
| `AnyPing` | Hertz `Any("/ping")` | Any 不能与其他动词组合 |
| `Get` | 构造失败 | 路径后缀不能为空 |
| 签名不匹配的 `GetOrder` | 构造失败 | 不静默忽略 |

### 3.2 路径风格

```go
appwebhertz.WithRoutePathStyle(appwebhertz.RoutePathStyleCamel)
```

| 方法 | Kebab（默认） | Camel | Snake |
|---|---|---|---|
| `GetHealthCheck` | `/health-check` | `/healthCheck` | `/health_check` |
| `GetHTTPStatus` | `/http-status` | `/httpStatus` | `/http_status` |

路径风格只影响自动方法后缀，不修改 GroupPath 或手动 URL。

## 4. Filter

```go
func (this *AuditFilter) Order() int {
	return 100
}

func (this *AuditFilter) DoFilter(ctx context.Context, request *app.RequestContext) {
	request.Next(ctx)
}
```

Order 越小越先进入链；相同 Order 保持注册顺序。Filter 直接是 Hertz Handler，自行选择调用 `Next` 或中断链。Request ID 等通用能力优先使用第 5 节的标准 Middleware；Filter 用于应用特有策略。配置启用的标准 Middleware 固定位于全部应用 Filter 外层。

实现 `GroupedFilter` 后，Filter 按逻辑分组生效：

```go
func (this *APIAuditFilter) GroupPath() string { return "/api" }
func (this *APIAuditFilter) Order() int        { return 100 }
func (this *APIAuditFilter) DoFilter(ctx context.Context, request *app.RequestContext) {
	request.Next(ctx)
}
```

IOC 统一使用 `DefineFilter(&APIAuditFilter{})`；装配阶段根据对象是否实现 `GroupedFilter` 判断作用域。是否全局由接口类型决定，而不是路径文本决定：`GroupPath() == ""` 与 `GroupPath() == "/"` 都属于根逻辑分组 `/`，但不会作用于其他分组或 `NoRoute`/`NoMethod`；需要覆盖所有请求时使用只实现 `Filter` 的类型。

## 5. 标准 Middleware

固定顺序：

```text
WithServerOptions 中的 advanced outer Middleware
-> Instrumentation Bundle Middleware（对象存在性启用）
-> Request ID
-> 请求完成事件（Access Log + IOC Observer fan-out）
-> Recovery
-> CORS
-> Gzip
-> global application Filters
-> grouped application Filters（仅匹配的逻辑分组）
-> Controller-local Middleware
-> Controller Routes / NoRoute / NoMethod
```

### Recovery

- 默认启用并捕获 CORS、Gzip、Filter 和 Handler panic；只有 `disabled: true` 才关闭。
- 内建 `response` 支持 `json`（默认）、`text`、`empty`，均不向客户端暴露 panic 内容。
- 自定义行为使用 `WithRecoveryHandler`；Handler 自行负责最终响应。

### Request ID

- 默认 Header 为 `X-Request-ID`，默认不信任入站值，使用 `crypto/rand` 生成 128-bit 标识。
- 只有可信代理已经校验标识来源时才设置 `trustIncoming: true`。
- Handler/Filter 使用 `appwebhertz.GetRequestID(request)` 读取；生成失败返回稳定 500，不让无标识请求继续。

### 实例日志与 Access Log

- `LoggerFactory` 是 Server bean 的必需 IOC 依赖；app Runtime 总会注册默认或 `WithLogging` 提供的 Factory，裸 IOC 必须显式注册。
- Init 一次派生 `app.web.hertz.assembly`、`app.web.hertz.middleware`、`web.hertz` 和 `web.hertz.request`；运行期不重复访问 Factory。
- `app-web-hertz` 成功组装后记录一条 Info 摘要，`web-hertz` 记录 Debug provider 摘要；构造失败只返回错误，由 IOC Build 边界统一记录。
- `accessLog.enabled=true` 启用内建结构化请求日志。
- `skipPaths` 使用无 query 的精确路径；`slowThreshold=0` 表示不区分慢请求。
- Recovery 后的 panic 记录最终 500 与 `outcome=panic`。
- `app-web-hertz` 不创建或关闭日志 Provider；sink、encoder 和基础 level 仍由根日志配置决定。可选 [`app-observe-webhertz`](../../app-observe-webhertz/README.md) 只负责 Prometheus/OTel 适配。

Access Log 在模块归属和安装方式上是 `app-web-hertz` 的可选基础组件，不属于
`app-observe-webhertz`。它在系统信号层面仍可用于运行诊断；若安装了 OTel Bundle，
完成事件中的 Trace ID 可以进入 Access Log，实现日志与 Span 关联。

### 请求完成事件扩展

`webhertz.HTTPRequestObservation` 是后端无关的通用请求完成事件，不等同于可观测性
适配模块。内建 Access Log 与 IOC 注入的 Observer 共享同一次计时和值快照：

- Prometheus 等 Observer 只通过 IOC `webhertz.RequestObserver` 声明，不设置 YAML 总开关；
- Observer 必须快速、并发安全，不得保留 Hertz 池化对象；
- Request ID、Trace ID 和原始 Path 不得作为默认 Metrics Label；
- 使用 `app-web-hertz` 时不要再安装第二个 `WithRequestObservers`，避免重复通知。

直接使用 provider 时如何声明自定义 Observer，见
[web-hertz 使用指南](web-hertz-usage.md#请求完成事件扩展)。

### Metrics 与 Tracing

Prometheus Metrics 与 OpenTelemetry Tracing 不是标准 Middleware 配置项，而是由
IOC 对象存在性启用的跨模块能力：

- Metrics adapter 发布 `RequestObserver` 与 `PrometheusHandler`；
- OTel adapter 借用应用 owned Provider/Propagator，并发布原子
  `InstrumentationBundle`；
- Registry、Provider、Exporter 与诊断 Server 的所有权始终属于应用或对应
  managed bean。

完整安装、采样策略、独立 `/metrics` 端口与关闭顺序见
[Web 可观测性使用指南](web-observability.md)。

### CORS

- 默认不放开任何 Origin。配置静态 `allowOrigins`，或通过 `WithCORSOriginValidator` 提供动态策略。
- `allowOrigins: ["*"]` 与 `allowCredentials: true` 在 Init 前失败。
- Origin 只接受 `http/https` 精确值；未知 Origin 返回 403，合法预检返回 204。

### Gzip

- 默认 level 为标准库默认值，`minLength=1024`；可配置不压缩媒体类型。
- 尊重 `gzip;q=0`、Range、既有 Content-Encoding、`no-transform`、无 Body 状态和 HEAD。
- 只压缩完整内存响应；流式响应、SSE 和连接升级保持原样。

如需改变固定顺序，关闭对应 feature，再通过 `WithServerOptions(webhertz.WithMiddleware(...))` 完整显式安装；配置不提供任意整数 Order。

## 6. hlog 与日志 Provider

本模块不接管 Hertz 全局 hlog。app Runtime 使用 `hlog-zap/app`，在 IOC Build 前安装、Server 关闭后恢复：

```go
app.WithLogging(apphlogzap.Wrap(
	applogzap.New(
		applogzap.WithConfigScope("log.zap"),
	),
))
```

关闭顺序必须是：

```text
web.Server Run 返回
-> IOC Container Close（再次幂等 Shutdown Server）
-> 恢复 hlog
-> log-zap Provider Close
-> config Manager Close
```

`hlog-zap/app` 不创建或配置应用日志；它装饰显式传入的 Logging，通过 Build 结果的 `ZapProvider()` 窄能力复用同一个 Zap Provider，同时向 IOC 提供 LoggerFactory，并以单 owner Lease 管理 hlog。全局 hlog 是进程级可变状态，同一进程不支持两个并行 Runtime 分别安装不同 Logger。

## 7. 生命周期和依赖约束

1. Server bean 依赖 Manager、全部 Controller 和全部 Filter，因此这些 bean 先 Init。
2. Container Close 使用逆拓扑顺序，先 Shutdown Server，再关闭它依赖的组件。
3. Controller/Filter 不能反向注入 `web.Server`，否则形成 `Controller -> Server -> Controller` 循环并在 Build 时失败。
4. Shutdown context 语义完全由 `web-hertz` 保留；等待中断仍可匹配 `webhertz.ErrShutdownWaitInterrupted`。
5. 地址和 timeout 只在 Init 读取一次，配置刷新不会修改正在运行的 Server。
6. app Workload 先完成主动停止；Container Close 是幂等资源兜底。IOC timeout 只发出协作式取消，并等待 Server Closer 返回后再关闭依赖。
7. 默认 `bestEffort` 在 Hertz `exitWaitTime` 到期后允许 Run 返回，以避免普通请求承担 gate 成本；需要保证 Controller/Filter 依赖不被提前关闭时，显式选择 `required`。

### 7.1 单业务 Server 边界与独立诊断端口

`NewModule()` 和 `Workload()` 共同表达当前 Runtime 的唯一业务 Server。该边界避免 config scope、
Controller 集合和按类型 IOC 解析在多实例下产生隐式选择。需要 Metrics 等独立诊断端口时，
composition root 应使用专用且具有唯一名称的 Workload；不要重复注册 `app-web-hertz` Module。
app 在任一 Workload 意外退出后关闭全组，并等待全部 Run/Shutdown 结果收敛。

Metrics Handler、私有 ServeMux、listener 获取顺序和诊断端口安全策略见
[Web 可观测性使用指南](web-observability.md#独立诊断-server)。当前只有一个真实
复用方，不抽取通用诊断 Server 模块。

## 8. 错误排查

| 错误码 | 常见原因 | 处理 |
|---|---|---|
| `app_web_hertz.config.invalid` | Middleware 字段、CORS 安全组合或 provider Config 非法 | 检查对应 stage/reason 和底层 web-hertz cause |
| `app_web_hertz.config.load_failed` | Manager/Snapshot 不可用、自定义 config scope 缺失、配置非法 | 检查 config scope 和底层 config/web-hertz cause；默认 `web.hertz` 缺失会使用内建值 |
| `app_web_hertz.declaration.invalid` | nil Option/Observer、非法 Controller/Filter/路径风格或 Access Log 缺少 LoggerFactory | 修正启动声明 |
| `app_web_hertz.route.register_failed` | Controller 原生路由注册返回错误 | 检查 Registrar cause |
| `app_web_hertz.middleware.execution_failed` | Request ID 或 Gzip 等请求阶段能力失败 | 结合结构化日志的 op/reason 排查 |
| `app_web_hertz.server.build_failed` | provider 构造或路由注册失败 | 检查 web-hertz code 和 Controller cause |
| `app_web_hertz.module.register_failed` | Server bean 名称或类型重复 | 每个 Runtime 只注册一次 Module |
| `app_web_hertz.server.state_invalid` | 绕过 IOC，在 Init 前调用代理 | 只在 Build 成功后 Resolve Server |
| `app_web_hertz.server.resolve_failed` | Workload 无法解析唯一 Server | 检查 Module 是否缺失或重复 |
| `ioc.build.dependency_not_found` | 未注册 `config.Manager` | 用 borrowed 语义声明 Manager |
| `ioc.build.circular_dependency` | Controller/Filter 反向依赖 Server | 移除反向依赖，改依赖领域服务 |

## 9. 测试建议

1. 自动 Controller 同时断言派生 URL 和 HTTP method；动态路由直接测试 `RegisterRoutes`。
2. Filter 同时测试 Order 和原生 `request.Next` 链。
3. Module 测试使用真实 Container，断言 Manager borrowed、切片顺序和 `web.Server` 可解析。
4. 生命周期测试使用预绑定 listener，让真实请求超过 Hertz `ExitWaitTime`，显式选择 `required` 后确保先完成请求和 Workload、再关闭 IOC 依赖；同时验证 `WaitStarted` 的成功、失败与 caller timeout 广播语义。
5. 错误断言同时检查 app-web-hertz、IOC、config 或 web-hertz 分层错误码。
6. CORS 至少覆盖允许/拒绝 Origin 与通配符凭据冲突；Gzip 覆盖 q=0、媒体类型和解压原文。
7. Observation 覆盖模板路由、未知路由基数、Recovery 最终状态、Request ID、skip path 和 Observer 并发。
8. Recovery 与 Request ID 联合测试最终 500 和关联 Header；Manager 路径断言只调用一次 Current 且不 Subscribe。

## 10. 相关文档

1. [模块 README](../../app-web-hertz/README.md)
2. [架构设计](../architecture/app-web-hertz-design.md)
3. [web-hertz 指南](web-hertz-usage.md)
4. [Quickstart](../../examples/quickstart-app-web-hertz/README.md)
5. [Web 可观测性使用指南](web-observability.md)
6. [高级可观测性 Quickstart](../../examples/quickstart-app-observe-webhertz/README.md)
7. [Web 可观测性提案](../architecture/web-observability-proposal.md)
