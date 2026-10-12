# web-hertz

`web-hertz` 是 Hertz 的 Web Server provider：内部封装 `*server.Hertz`，只负责 Hertz 生命周期适配、独立配置，以及原生 Middleware/Route 扩展。

## 设计范围与目标

### In Scope

1. 生命周期适配：封装 `*server.Hertz`，归一化 Hertz `Run/Shutdown` 与 `web.Server` 的状态、错误和资源释放语义。
2. 独立配置：提供 Hertz 专属 typed Config、确定默认值、解析优先级和 fail-fast 校验。
3. 原生扩展：按声明顺序应用 Hertz `config.Option`、全局 Middleware、NoRoute/NoMethod 和 RouteFunc。
4. 完成态观测：复制有限基数请求快照并同步 fan-out 窄 Observer，不依赖具体日志、Metrics 或 Trace 后端。

### Out of Scope

1. 不定义框架无关的 Router、Controller、Filter、Handler、Context 或 Middleware。
2. 不调用 `Hertz.Spin()`，不安装或替换进程信号处理器。
3. 不定义配置 scope，不读取 `config.Manager`、环境变量或 YAML 文件；配置来源与绑定由应用装配层负责。
4. 不创建 Nacos、Consul 等服务注册客户端，不持有厂商凭据。
5. 不内置 pprof、gzip、CORS、request-id、access-log 等 contrib 依赖。
6. 不扫描 Controller 方法名，不通过反射推导 HTTP method/path。
7. 不修改 Hertz 全局 JSON marshal、全局 hlog Logger 或其他进程级状态。
8. 不依赖 IOC，也不提供 Controller/Filter 聚合、字段注入或自动 bean 发现。
9. 不读取日志配置、不创建 Zap Logger，也不调用 `hlog.SetLogger`；日志 provider 与 Hertz hlog 的安装由上层装配负责。

## 依赖方向

```text
application --构造参数/原生扩展--> web-hertz --> Hertz
application --运行期依赖-------> web.Server
                                  ^
                                  |
                              web-hertz
```

`web-hertz` 实现 `web.Server`，但 `web` 不依赖 Hertz。`web-hertz` 不依赖根 `log`、`log-zap`、`hlog-zap`、app、app-runtime、IOC、config provider 或具体服务注册实现。这里的 `app.HandlerFunc` 来自 Hertz，而不是 gobase 的 `app` 模块。

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `Config` | Hertz 启动期 typed 配置；字段、默认值和校验全部由本模块拥有 |
| `Option` | 声明 Hertz 原生高级 Option、全局 Middleware 与 RouteFunc |
| `Server` | 持有 `*server.Hertz`、生命周期状态、运行结果和幂等 Shutdown 协调状态 |
| `RouteFunc` | 在构造阶段接收 Hertz 根路径 `*route.RouterGroup`，以函数形式注册原生路由 |
| `RouterConfig` | 使用三态布尔值定义路由修正、405 和路由打印行为 |
| `HTTPRequestObservation` | 不持有池化对象的请求完成态值快照 |
| `RequestObserver` | 同步消费完成态；可选扩展开始通知和受控 Metadata |

## 公共入口

推荐构造入口：

```go
server, err := webhertz.New(webhertz.Config{
	Address: ":8080",
},
	webhertz.WithMiddleware(recovery.Recovery()),
	webhertz.WithNoRoute(notFoundHandler),
	webhertz.WithNoMethod(methodNotAllowedHandler),
	webhertz.WithRoutes(func(router *route.RouterGroup) error {
		router.GET("/ping", pingHandler)
		return nil
	}),
)
if err != nil {
	return err
}

return server.Run(ctx)
```

稳定签名：

```go
func New(config Config, options ...Option) (*Server, error)
func DefaultConfig() Config
func ResolveConfig(input Config) (Config, error)
func WithNoRoute(handlers ...app.HandlerFunc) Option
func WithNoMethod(handlers ...app.HandlerFunc) Option
func WithRequestObservers(observers ...RequestObserver) Option
var ErrShutdownWaitInterrupted error
```

`New` 只解析配置、创建 Hertz Engine 并注册 Middleware/Routes，不建立监听器、不启动 goroutine。返回的具体 `*Server` 实现 `web.Server`；调用方需要统一生命周期时依赖根接口，需要 Hertz 专属构造能力时使用本模块 Option。

本模块只在 Hertz 的构造与生命周期边界做适配，不重复封装 `GET/POST/Group/Use` 等原生路由 API。扩展能力应通过 Hertz `config.Option`、`app.HandlerFunc` 和 `*route.RouterGroup` 表达，使 Hertz 官方文档和生态组件可以直接使用。

## 独立配置模型与默认值

typed 配置只吸收稳定、高频且直接映射 Hertz Server/Router 的字段：

| 字段 | 单位 | 默认值 | 语义与约束 |
|---|---:|---:|---|
| `address` | string | `:8888` | Hertz 监听地址；trim 后为空使用默认值 |
| `readTimeout` | duration | `3m` | 底层读取超时；输入小于 0 时失败 |
| `idleTimeout` | duration | `3m` | 无请求连接空闲超时；输入小于 0 时失败 |
| `maxRequestBodySize` | byte | `4194304` | 请求体上限；输入小于 0 时失败 |
| `maxHeaderBytes` | byte | `1048576` | 请求头上限；输入小于 0 时失败 |
| `exitWaitTime` | duration | `5s` | Hertz 单次优雅退出最大等待时间；输入零值使用默认值，负值非法 |
| `router.redirectTrailingSlash` | optional bool | `true` | nil 使用默认；false 显式关闭尾斜杠重定向 |
| `router.redirectFixedPath` | optional bool | `false` | nil 使用默认；true 开启大小写和冗余路径修正 |
| `router.handleMethodNotAllowed` | optional bool | `false` | nil 使用默认；true 开启 405 路由扫描 |
| `router.disablePrintRoute` | optional bool | `false` | nil 使用默认；true 禁止 Hertz 构造时打印路由 |

默认值与 Hertz v0.10.5 保持一致，但由 `DefaultConfig` 固化，避免 Hertz 升级静默改变本模块行为。输入零值表示保留本模块默认值；确需设置 Hertz 的特殊零值语义时，使用显式 Hertz 高级 Option。

`Config` 是 `web-hertz` 自有合同：不嵌入根 `web` 配置，不与未来 `web-gin.Config` 共享结构，也不承诺同名字段具有跨框架一致语义。应用可以把任意配置来源绑定为该结构，但配置路径、刷新和订阅生命周期不属于本模块。

配置优先级：

```text
DefaultConfig < Config 显式字段 < WithHertzOptions 声明顺序
```

`WithHertzOptions` 是高级 escape hatch，仅接受构造期 `config.Option`。唯一优先级例外是
`WithNoMethod`：为保证自定义 405 Handler 必达，它会在全部原生 Option 后最终启用
`HandleMethodNotAllowed`。当前对 Hertz Registry 配置 fail-fast：`Hertz.Spin()` 的私有注册 hook 不会被
使用，若直接透传 `server.WithRegistry`，容易形成“停止时注销但启动时未注册”的不完整生命周期。服务注册
应在后续独立合同中设计。

## 原生 Middleware 与路由扩展合同

### Middleware

`WithMiddleware(handlers ...app.HandlerFunc)` 只接收 Hertz 原生 Handler，按 Option 声明顺序追加，并在全部路由注册前调用 `Hertz.Use`。

本模块不默认安装任何 Middleware。参考实现中的能力按需显式声明：

```go
webhertz.WithMiddleware(
	recovery.Recovery(),
	requestid.New(),
)
```

这避免 `web-hertz` 因默认能力被迫依赖全部 hertz-contrib 模块。group 级 Middleware 由 RouteFunc 使用 Hertz `router.Group` 原生能力声明。

### NoRoute 与 NoMethod

`WithNoRoute` 和 `WithNoMethod` 是不暴露 Engine 的窄扩展。两者按声明顺序追加 Handler，并继承全局
Middleware；未声明时完全保留 Hertz 默认行为。`WithNoMethod` 自动开启 405 路由扫描，避免已声明 Handler
因 Config 或原生 Option 关闭 `HandleMethodNotAllowed` 而静默不可达。nil Handler 在构造阶段返回
`web_hertz.declaration.invalid`。

### 请求完成态 Observation

`WithRequestObservers` 在声明位置安装一个完成态 Middleware，并按声明顺序同步 fan-out。快照复制 Method、模板 Route、无 query Path、最终 Status/Duration/Body 大小和有限 Outcome；未匹配路由固定为 `_unmatched`，非标准 Method 固定为 `_other`。

Observer 不得持有 `RequestContext`。可选 `RequestLifecycleObserver` 只接收开始 Method，用于 in-flight Gauge；可选 `RequestObservationMetadataProvider` 只能补充 Request ID、Trace ID 和已由 Recovery 收敛的 panic 标记，不能改写指标维度。单个 Observer/Metadata Provider panic 会被隔离，原始业务 panic 在通知后继续传播。

### RouteFunc

合同：

```go
type RouteFunc func(router *route.RouterGroup) error
```

RouteFunc 只增加构造阶段的顺序、错误和生命周期隔离边界。`New` 在全局 Middleware 注册后创建一个共享的根路径 `Group("/")`，全部 RouteFunc 都直接使用这个 Hertz `RouterGroup`；URL 不增加前缀，函数也无法取得 `*server.Hertz` 的运行入口。

规则：

1. RouteFunc 按声明顺序执行；nil 函数在构造阶段 fail-fast。
2. 任一 RouteFunc 返回 error 时停止后续注册，`New` 不返回半初始化 Server。
3. RouteFunc 只获得共享根 RouterGroup，不能调用 `Run/Spin/Shutdown/Close` 绕过生命周期；前一个 RouteFunc 对根 group 的原生修改会影响后续 RouteFunc。
4. Controller 等应用组织约定由上层转换为 RouteFunc，本模块不识别或扫描 Controller。
5. Hertz 路由注册或 RouteFunc 的 panic 属于编程错误；`New` 在构造事务边界补充 `stage=register_routes` 语义后继续 panic，不把编程错误降级成普通启动失败。

## Hertz 生命周期适配合同

状态机：

```text
new -> starting -> running -> stopping -> stopped
  \-------------------------------------> stopped
```

### Run

1. nil context 返回输入错误，不进入 Hertz。
2. 已取消 context 不启动监听，终结本对象运行周期并返回 nil。
3. 只有 `new` 可以进入 `starting`；并发或重复 Run 返回稳定状态错误。
4. 使用 `Hertz.Run()`，不使用会接管进程信号的 `Hertz.Spin()`。
5. ctx 取消触发与显式 Shutdown 相同的幂等停止事务。
6. 优雅停止导致的预期监听退出结果归一化为 nil；真实监听、OnRun 或 transport 错误保留 cause。
7. Run 返回后状态固定为 `stopped`，不得再次启动。

### Shutdown

1. nil context 返回输入错误，不触发停止。
2. `new` 状态调用时直接转为 `stopped` 并返回 nil，后续 Run 返回状态错误。
3. `starting/running` 状态只触发一次底层 Hertz Shutdown；并发调用共享同一停止结果。
4. 每次调用的 ctx 只约束当前调用者等待；一个短 deadline 不得截断其他调用者共享的停止事务。
5. caller context 先结束时仍返回 `web_hertz.shutdown.failed` 并保留 context cause，同时可用 `errors.Is(err, webhertz.ErrShutdownWaitInterrupted)` 与底层稳定停止失败分支。
6. 底层停止预算使用应用全部原生 Option 后的最终 `ExitWaitTimeout`；Hertz Shutdown hook、连接排空和 transport 停止均受该预算约束。
7. 已完成停止时重复调用返回首次稳定停止结果，不重复注销或释放资源。
8. 公共 Shutdown 不调用 Hertz `Engine.Close()`；当前没有绕过优雅停止的强制关闭入口。

## 错误与可观测性

错误边界：

| 类别 | 典型阶段 |
|---|---|
| 配置非法 | `resolve_config` |
| Option/RouteFunc 声明非法 | `declare` / `register_routes` |
| 重复或并发运行 | `transition_state` |
| Hertz 初始化或监听失败 | `engine_run` |
| 优雅停止失败 | `engine_shutdown` |
| 当前 Shutdown 调用等待取消/超时 | `wait`；错误链包含 `ErrShutdownWaitInterrupted` |

错误必须保留底层 cause，并携带 `op/result/reason/stage`；地址、路由模式等低风险诊断信息可以记录，TLS 私钥、registry token、请求体和完整凭据不得进入错误或日志。

生命周期日志至少覆盖：

```text
op=web_hertz.run      result=running
op=web_hertz.run      result=failed reason=lifecycle_failed
op=web_hertz.shutdown result=ok     reason=engine_stopped
op=web_hertz.shutdown result=failed reason=engine_shutdown_failed
```

`web-hertz` 自身生命周期日志与 Hertz 内部日志统一使用 `hlog.SystemLogger()`。需要 Zap 时由上层显式安装 `hlog.FullLogger`；`web-hertz` 不依赖 `hlog-zap`，不隐式修改进程级 hlog Logger。它只产生后端无关完成态快照，Request Log/Prometheus 映射由可选适配层承担。

## 验证

配置、构造事务、根 RouterGroup 隔离、真实监听和并发停止合同由 `*_test.go` 覆盖；任务型验证方法见使用指南，设计原因见架构文档。

## Open Gaps / Future Improvements

1. Hertz 只有在 Engine 标记为 running 后才允许 `Shutdown`；如果未来高级扩展在初始化阶段永久阻塞，适配层无法强制终止第三方 goroutine，只能由调用者 context 限制自身等待。
2. pprof 可通过接收 RouterGroup 的 RouteFunc 注册；配置化启用与独立诊断端口由应用装配层继续设计，不暴露 Engine。
3. 服务注册需要独立设计所有权、注册时机、失败回滚和注销语义；当前不支持。
4. 尚不提供 Ready/Address/Status 查询；没有跨 provider 需求前不扩展根 `web.Server`。
5. Controller、Filter、IOC 聚合、配置绑定和 `hlog.FullLogger` 安装属于 `app-web-hertz` 的职责，不进入本 provider 的稳定合同。

## File Map

1. `doc.go`：package 边界摘要。
2. `config.go`：Config、默认值、解析与 Hertz Option 映射。
3. `server.go`：Hertz 构造、Registry 能力拒绝与路由注册事务。
4. `lifecycle.go`：单运行周期状态机、Run/Shutdown 协调和错误归一化。
5. `route.go`：RouteFunc 合同。
6. `no_route.go`、`no_method.go`：404/405 窄 Handler 声明。
7. `options.go`：Hertz Option、Middleware、Routes 与构造计划。
8. `errors.go`：可分支错误码和阶段包装。
9. `request_observation.go`、`request_observer*.go`：完成态 VO、Observer 合同、Option 和私有 Middleware。
10. `*_test.go`：配置、404/405、完成态观测、构造和生命周期合同测试。

## 相关文档

1. [web-hertz 使用指南](../docs/guides/web-hertz-usage.md)
2. [web-hertz 架构设计](../docs/architecture/web-hertz-design.md)
3. [quickstart-web-hertz](../examples/quickstart-web-hertz/README.md)
4. [根 web 生命周期合同](../web/README.md)
