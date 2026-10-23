# App Web Hertz 使用指南

本文说明如何让 IOC 自动聚合 Controller、Filter、fallback handler 和观测组件，并托管 Hertz Server 生命周期。

## 1. 能力与入口

| 能力 | 解决的问题 | 启用入口 | 默认状态 | 业务入口 | 不适用场景 |
|---|---|---|---|---|---|
| IOC 自动装配 | 从依赖图聚合组件并统一关闭 | `NewModule` + `Register` | 需显式声明 | `ioc.ResolveAs[web.Server]` | Build 后动态加组件 |
| App 生命周期整合 | 解析并运行唯一 Server，由 app 负责取消与 Runtime Close | `appwebhertz.Runner()` | 需显式启用 | `app.WithRunner` | 多 Server 监督 |
| 自动路由 | 简单静态方法到 URL 映射 | Controller 导出方法 | Kebab | Hertz Handler | 动态路径或复杂链 |
| 原生路由 | 保留 Hertz RouterGroup 完整能力 | `RouteRegistrar` | 可选 | `RegisterRoutes` | 框架无关路由 DSL |
| 全局 Filter | 提供稳定 Middleware 顺序 | `Filter` 接口声明 | 无默认 Filter | `DoFilter` | 分组策略 |
| 分组 Filter | 同一 GroupPath 的 Controller 共享策略 | `GroupedFilter` 接口声明 | 无默认 Filter | `GroupPath` + `DoFilter` | 全局策略 |
| 标准 Middleware | Recovery、Request ID、Access Log、CORS、Gzip | `web.hertz.middleware` | 全部关闭 | Config + typed policy | 任意动态执行顺序 |
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

Module 默认读取独立 scope：

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
```

Server 字段仍处于原有扁平 `web.hertz` scope；Router 和标准能力分别位于 `router`、`middleware` 子段。Router 布尔值支持未配置、显式 true 和显式 false 三态。

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

不声明 `WithScope` 时使用 `web.hertz`；自定义路径通过 Option 声明：

```go
appwebhertz.NewModule(appwebhertz.WithScope("services.api"))
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

使用 `app` 时，常规单 Server 应用只需要声明组件 Definitions、Server Module 和 Runner：

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
	app.WithModules(appwebhertz.NewModule()),
	app.WithRunner(appwebhertz.Runner()),
)
```

Runtime 已注册 borrowed `config.Manager`。`appwebhertz.Runner()` 解析唯一 `web.Server` 并调用 Run；context 取消后 Server 自行优雅停止，随后 app 关闭 IOC Runtime。多 Server 场景不要使用该 Runner，由 composition root 统一监督。

### 2.3 Option 归并

| 入口 | 默认值 | 重复规则 | 非法值 | 生效阶段 |
|---|---|---|---|---|
| `WithScope` | `web.hertz` | 后声明覆盖前声明 | 空白回退默认 | IOC Init |
| `WithRoutePathStyle` | Kebab | 后者覆盖前者 | 不支持的枚举 fail-fast | 构造计划 |
| `WithServerOptions` | 无 | 每次追加 | 任一 nil fail-fast | provider 构造 |
| `WithRecoveryHandler` | 内建安全响应 | 后者覆盖 | nil fail-fast | Recovery 启用后 |
| `WithRequestIDGenerator` | crypto/rand 128-bit | 后者覆盖 | nil fail-fast；运行期空值/错误返回 500 | Request ID 启用后 |
| `WithCORSOriginValidator` | 无 | 后者覆盖并扩展静态列表 | nil 或启用后无任何 Origin 策略 fail-fast | CORS 构造/请求期 |
| `WithGzipPredicate` | 基础协议规则 | 后者覆盖 | nil fail-fast | 最终响应完成后 |
| nil `Option` | 不适用 | 不适用 | `declaration_invalid` | Registry 修改前 |

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
-> completion Observation（Access Log + Metrics fan-out）
-> Recovery
-> CORS
-> Gzip
-> global application Filters
-> grouped application Filters（仅匹配的逻辑分组）
-> Controller-local Middleware
-> Controller Routes / NoRoute / NoMethod
```

### Recovery

- 默认关闭；启用后捕获 CORS、Gzip、Filter 和 Handler panic。
- 内建 `response` 支持 `json`（默认）、`text`、`empty`，均不向客户端暴露 panic 内容。
- 自定义行为使用 `WithRecoveryHandler`；Handler 自行负责最终响应。

### Request ID

- 默认 Header 为 `X-Request-ID`，默认不信任入站值，使用 `crypto/rand` 生成 128-bit 标识。
- 只有可信代理已经校验标识来源时才设置 `trustIncoming: true`。
- Handler/Filter 使用 `appwebhertz.GetRequestID(request)` 读取；生成失败返回稳定 500，不让无标识请求继续。

### 实例日志、Access Log 与完成态 Metrics

- `LoggerFactory` 是 Server bean 的必需 IOC 依赖；app Runtime 总会注册默认或 `WithLogging` 提供的 Factory，裸 IOC 必须显式注册。
- Init 一次派生 `app.web.hertz.assembly`、`app.web.hertz.middleware`、`web.hertz` 和 `web.hertz.request`；运行期不重复访问 Factory。
- `app-web-hertz` 成功组装后记录一条 Info 摘要，`web-hertz` 记录 Debug provider 摘要；构造失败只返回错误，由 IOC Build 边界统一记录。
- `accessLog.enabled=true` 启用内建结构化请求日志。
- `skipPaths` 使用无 query 的精确路径；`slowThreshold=0` 表示不区分慢请求。
- Prometheus 等通用 Observer 只通过 IOC `webhertz.RequestObserver` 声明，不需要虚假的 YAML 总开关。
- Access Log、Metrics 共享同一次完成态计时和快照；Recovery 后的 panic 记录最终 500 与 `outcome=panic`。
- `app-web-hertz` 不创建或关闭日志 Provider；sink、encoder 和基础 level 仍由根日志配置决定。可选 [`app-observe-webhertz`](../../app-observe-webhertz/README.md) 只负责 Prometheus/OTel 适配。

### OpenTelemetry Trace

- Trace 不进入 `web.hertz` YAML；应用显式声明 Provider owner 和 `app-observe-webhertz.WithOpenTelemetry(...)` 才启用。
- Provider owner 创建 SDK Provider/Exporter/Sampler，并在 IOC 关闭阶段执行 `ForceFlush -> Shutdown`；Web adapter 只借用 Provider/Propagator。
- Bundle 的 Hertz tracer Option 与 Server Middleware 在 Engine 创建前原子校验，任一半非法都不会发布 Server。
- 默认 Span 名称为 `METHOD route`，未匹配路由固定为 `_unmatched`；只记录 method、route、status、受控 error.type 和安全 Enricher 字段。
- 完整 URL、query、Header、Cookie、Body、panic 值和 Stack 默认不进入 Span。ignore path 使用无 query 的精确绝对路径。
- Request ID 默认独立生成。需要对齐时显式把 `NewTraceAlignedRequestIDGenerator(fallback)` 交给 `WithRequestIDGenerator`；无有效 Span 时 fallback 保持响应 Header 合同。

```go
observe := webhertzobserve.NewModule(
	webhertzobserve.WithOpenTelemetry(
		webhertzobserve.WithSamplingPolicyReference("parentbased_ratio_10"),
		webhertzobserve.WithTracingIgnoredPaths("/healthz", "/readyz"),
	),
)
```

采样比例不是 Web Middleware 配置：它必须在 Provider owner 创建 SDK Sampler 时固定。`sampling policy reference` 只验证 adapter 是否接到了预期 Provider，避免看似配置成功、实际采样策略错配。

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
		applogzap.WithScope("log.zap"),
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
6. 首次 Container Close context 必须覆盖完整优雅停止窗口；IOC 等待超时后会继续关闭后续 bean，不能把短 deadline 当作后台安全停止机制。

### 7.1 独立诊断端口与统一监督

Metrics/pprof 不应注册到业务 Hertz Router。多 Server 应用由 composition root 显式装配：

1. 应用 owned 的 private Registry 显式聚合 IOC/业务 Collector，`app-observe-webhertz.WithSharedPrometheus()`
   再注册 Web Collector 并发布 borrowed `PrometheusHandler`；
2. 应用在 BeforeRun 阶段构造独立 `http.Server` 和私有 `http.ServeMux`，此时不获取 listener；
3. `/metrics` 显式挂载 Handler，pprof 默认关闭且只注册到该私有 mux；
4. 应用 Workload Controller 同时监督业务 `web.Server` 和诊断 Server，并把同一个实例分别接入 Runner 与
   Shutdown Hook；
5. 任一成员意外退出会取消其他成员；Shutdown Hook 在全部成员 Run 返回后才允许 Runtime Close；
6. 诊断地址默认使用 loopback。对 Pod 外开放时，必须显式修改地址并配置 NetworkPolicy、sidecar 或鉴权。

Go/Process Collector 默认也不注册。只有应用明确选择后才加入同一 Registry，禁止用 DefaultGatherer 把未知
全局 Collector 隐式带入诊断端口。

诊断地址、Metrics path、pprof 开关和 Header timeout 属于具体应用配置，不进入 `web.hertz`，也不形成新的
框架固定 `management`/`observability` scope。当前只有一个真实使用方，因此不抽取
`app-diagnostics-http`。

## 8. 错误排查

| 错误码 | 常见原因 | 处理 |
|---|---|---|
| `app_web_hertz.config_invalid` | Middleware 字段、CORS 安全组合或 provider Config 非法 | 检查对应 stage/reason 和底层 web-hertz cause |
| `app_web_hertz.config_load_failed` | Manager/Snapshot 不可用、scope 缺失、配置非法 | 检查 scope 和底层 config/web-hertz cause |
| `app_web_hertz.declaration_invalid` | nil Option/Registry/Observer、非法 Controller/Filter/路径风格或 Access Log 缺少适配器 | 修正启动声明 |
| `app_web_hertz.server_build_failed` | provider 构造或路由注册失败 | 检查 web-hertz code 和 Controller cause |
| `app_web_hertz.module_register_failed` | Server bean 名称或类型重复 | 每个 Container 只注册一次 Module |
| `app_web_hertz.server_state_invalid` | 绕过 IOC，在 Init 前调用代理 | 只在 Build 成功后 Resolve Server |
| `ioc.build.dependency_not_found` | 未注册 `config.Manager` | 用 borrowed 语义声明 Manager |
| `ioc.build.circular_dependency` | Controller/Filter 反向依赖 Server | 移除反向依赖，改依赖领域服务 |

## 9. 测试建议

1. 自动 Controller 同时断言派生 URL 和 HTTP method；动态路由直接测试 `RegisterRoutes`。
2. Filter 同时测试 Order 和原生 `request.Next` 链。
3. Module 测试使用真实 Container，断言 Manager borrowed、切片顺序和 `web.Server` 可解析。
4. 生命周期测试使用预绑定 listener，确保 Container Close 能收敛 Server。
5. 错误断言同时检查 app-web-hertz、IOC、config 或 web-hertz 分层错误码。
6. CORS 至少覆盖允许/拒绝 Origin 与通配符凭据冲突；Gzip 覆盖 q=0、媒体类型和解压原文。
7. Observation 覆盖模板路由、未知路由基数、Recovery 最终状态、Request ID、skip path 和 Observer 并发。
7. Recovery 与 Request ID 联合测试最终 500 和关联 Header；Manager 路径断言只调用一次 Current 且不 Subscribe。

## 10. 相关文档

1. [模块 README](../../app-web-hertz/README.md)
2. [架构设计](../architecture/app-web-hertz-design.md)
3. [web-hertz 指南](web-hertz-usage.md)
4. [Quickstart](../../examples/quickstart-app-web-hertz/README.md)
