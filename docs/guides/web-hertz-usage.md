# web-hertz 使用指南

本文按实际任务说明如何直接使用 `web-hertz` 构建 Hertz 服务。模块范围、默认值和公共入口见 [web-hertz README](../../web-hertz/README.md)，精确状态机与设计取舍见 [web-hertz 架构设计](../architecture/web-hertz-design.md)。

## 1. 选择入口

| 场景 | 依赖入口 | 说明 |
|---|---|---|
| 应用宿主只运行一个已构造的 Web Server | `web.Server` | 只依赖 `Run/Shutdown` 稳定合同 |
| 直接构造 Hertz、注册原生 Middleware/Route | `web-hertz` | 当前可用的 Hertz provider |
| Controller/Filter/IOC/config scope 自动装配 | [`app-web-hertz`](app-web-hertz-usage.md) | 已提供独立应用装配模块 |
| 需要另一套 Web 框架 | 对应 provider | 不应要求 `web-hertz` 模拟其他框架概念 |

业务 Handler、Middleware 和路由直接依赖 Hertz 原生类型。根 `web` 只用于应用运行期抽象，不提供 Router 或公共配置。

## 2. 运行 Quickstart

从仓库根目录启动：

```powershell
./scripts/go.ps1 -C examples run ./quickstart-web-hertz
```

请求示例端点：

```powershell
Invoke-RestMethod http://127.0.0.1:18080/healthz
Invoke-RestMethod http://127.0.0.1:18080/api/v1/hello/gobase
```

按 `Ctrl+C` 触发优雅停止。完整代码位于 [quickstart-web-hertz](../../examples/quickstart-web-hertz/README.md)。

## 3. 构造 Server

推荐在 composition root 中一次性完成配置和原生扩展：

```go
server, err := webhertz.New(
	webhertz.Config{Address: "127.0.0.1:8080"},
	webhertz.WithMiddleware(accessLog(), recovery.Recovery()),
	webhertz.WithNoRoute(notFoundHandler),
	webhertz.WithNoMethod(methodNotAllowedHandler),
	webhertz.WithRoutes(func(router *route.RouterGroup) error {
		router.GET("/healthz", healthHandler)
		return nil
	}),
)
if err != nil {
	return err
}
```

`New` 只构造 Hertz Engine 并注册 Middleware/Routes，不监听端口，也不启动 goroutine。构造成功后，配置和路由声明视为冻结。

构造顺序固定为：

```text
ResolveConfig
  -> typed Config 转 Hertz Options
  -> WithHertzOptions（最终优先级）
  -> 校验 Registry
  -> 注册全部 Middleware
  -> WithNoMethod 最终启用 405 扫描（仅在已声明时）
  -> 注册 NoRoute / NoMethod Handler
  -> 顺序执行全部 RouteFunc
```

## 4. 配置与覆盖

`webhertz.Config` 是 provider 独立配置，不来自根 `web`：

| 字段 | 默认值 | 输入规则 |
|---|---:|---|
| `Address` | `:8888` | trim 后为空使用默认值 |
| `ReadTimeout` | `3m` | 零值使用默认值，负值失败 |
| `IdleTimeout` | `3m` | 零值使用默认值，负值失败 |
| `MaxRequestBodySize` | `4194304` | 零值使用默认值，负值失败 |
| `MaxHeaderBytes` | `1048576` | 零值使用默认值，负值失败 |
| `ExitWaitTime` | `5s` | 零值使用默认值，负值失败 |
| `Router.RedirectTrailingSlash` | `true` | nil 使用默认值；false 显式关闭 |
| `Router.RedirectFixedPath` | `false` | nil 使用默认值；true 显式开启 |
| `Router.HandleMethodNotAllowed` | `false` | nil 使用默认值；true 显式开启 |
| `Router.DisablePrintRoute` | `false` | nil 使用默认值；true 显式开启 |

优先级：

```text
DefaultConfig < Config 显式字段 < WithHertzOptions 声明顺序
```

使用原生 Option 设置 typed Config 未覆盖的 Hertz 能力：

```go
webhertz.WithHertzOptions(
	server.WithWriteTimeout(5*time.Second),
	server.WithDisablePrintRoute(true),
)
```

`WithHertzOptions` 是高级入口。Option 可以覆盖 typed Config 的值，调用方必须理解最终 Hertz 语义。唯一例外是已声明的 `WithNoMethod` 会在原生 Option 后再次开启 `HandleMethodNotAllowed`，保证自定义 405 Handler 可达。当前禁止 `server.WithRegistry`：直接 `Run` 不会执行 `Spin` 的私有注册 hook，接受 Registry 会造成启动不注册、关闭却注销的不对称生命周期。

## 5. 注册 Middleware

`WithMiddleware` 只接收 Hertz `app.HandlerFunc`，不提供 Filter 转换：

```go
webhertz.WithMiddleware(
	accessLog(),
	recovery.Recovery(),
)
```

规则：

1. Middleware 按声明顺序追加。
2. 全部 Middleware 在任一 RouteFunc 前注册。
3. nil Handler 在 `New` 阶段返回声明错误。
4. group 级 Middleware 在 RouteFunc 内通过 Hertz `router.Group(...handlers)` 或 `group.Use` 声明。

当 access log 需要记录 panic 恢复后的 500 状态时，把 access log 放在 recovery 外层。Quickstart 展示了该顺序。

## 6. 注册路由

`RouteFunc` 直接获得生命周期隔离的 Hertz 根路径 `*route.RouterGroup`：

```go
func registerRoutes(router *route.RouterGroup) error {
	router.GET("/healthz", healthHandler)
	api := router.Group("/api/v1")
	api.GET("/users/:id", userHandler)
	return nil
}
```

`New` 在全局 Middleware 注册后创建一个 `Group("/")`，多个 RouteFunc 按声明顺序共享该根 group。URL 不增加额外前缀，且回调无法取得 `*server.Hertz` 的 Run/Shutdown 入口。某个函数返回 error 时，后续函数不再执行，`New` 不返回半初始化 Server。

重复路由等 Hertz 编程错误仍以 panic fail-fast。provider 会添加 `stage=register_routes`，但不会把编程错误伪装成可恢复的启动 error。需要 Controller/Filter、方法名自动路由或 IOC 聚合时使用 `app-web-hertz`；这些应用约定不属于 provider。

### 6.1 自定义 404 与 405

未匹配路由和方法不匹配属于 Engine 级 fallback，使用窄 Option 声明：

```go
webhertz.WithNoRoute(notFoundHandler)
webhertz.WithNoMethod(methodNotAllowedHandler)
```

两者都按声明顺序追加 Handler，并继承全局 Middleware。`WithNoMethod` 自动开启 Hertz 的 405 路由扫描；即使后声明的原生 Option 关闭该开关，provider 也会在构造末段重新开启。未声明时保持 Hertz 默认行为，nil Handler 在构造期失败。

pprof、健康检查等普通端点仍通过 `RouteFunc` 注册；Hertz 的 pprof 注册函数可以接收 `*route.RouterGroup`，不需要暴露 Engine。生产环境若需要与业务流量隔离，应由应用装配独立诊断监听器。

## 7. 运行与停止

### 信号驱动

应用拥有 OS signal，并把它转换为 context：

```go
ctx, stop := signal.NotifyContext(
	context.Background(),
	os.Interrupt,
	syscall.SIGTERM,
)
defer stop()

return server.Run(ctx)
```

`Run` 阻塞到监听失败或完整停止。context 取消会触发共享 Hertz Shutdown；停止成功时返回 nil。不要调用 `Hertz.Spin()`，也不要再安装第二套 signal waiter。

### 显式 Shutdown

应用宿主也可以从自己的关闭阶段调用：

```go
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

if err := server.Shutdown(shutdownCtx); err != nil {
	return err
}
```

一个 Server 只有一个运行周期：

1. 并发或重复 `Run` 返回状态错误。
2. `Shutdown` 可以并发调用，调用者共享同一次底层停止。
3. 每个 Shutdown context 只限制当前调用者等待，不会取消共享停止事务。
4. 在 `Run` 前调用 Shutdown 会终结该对象，后续 Run 返回状态错误。
5. 需要重启时重新调用 `New`，不要复用已停止实例。

caller context 先结束时，错误码仍为 `web_hertz.shutdown.failed`，并同时保留 context cause 与 `ErrShutdownWaitInterrupted`：

```go
if errors.Is(err, webhertz.ErrShutdownWaitInterrupted) {
	// 仅本次等待结束；共享停止仍在后台继续。
}
```

不要只用 `errors.Is(err, context.DeadlineExceeded)` 判断共享停止是否完成；底层 Hertz 停止失败也可能携带相同 cause。

## 8. 集成 hlog-zap

`web-hertz` 与 Hertz 内部日志统一写入当前 `hlog.SystemLogger()`，自身不创建 Zap，也不修改全局 Logger。

选择方式：

| 场景 | 推荐入口 | 所有权 |
|---|---|---|
| 单 Host、进程全程使用同一 hlog | `hlogzap.Install` | composition root 安装并 Sync，不恢复对象身份 |
| 已有 `log-zap.Provider`，需要业务日志与 Hertz 共享底层 | `hlogzap.NewFromProvider` + `appwebhertz.WithHLog` | Host 安装/恢复 hlog，调用方最后关闭 Provider |
| 库或纯 provider | 不安装 | 使用进程当前 hlog，避免修改全局状态 |

纯 `web-hertz` 应用可以在构造 Server 前显式安装：

```go
logger, err := hlogzap.Install(hlogzap.Config{
	Level:  "info",
	Mode:   hlogzap.ModeConsole,
	Format: hlogzap.FormatJSON,
})
if err != nil {
	return err
}
defer logger.Sync()

server, err := webhertz.New(config, options...)
```

`Install` 修改进程全局 hlog，必须由 composition root 在并发启动前调用。它不负责恢复旧 Logger；库代码、并行测试和 provider 内部不应隐式调用。应用装配场景使用 `app-web-hertz` 管理单 Host hlog 租约，并用 `hlogzap.NewFromProvider(logProvider)` 复用业务日志的同一个底层 Core。

## 9. 错误处理

| 错误码 | 调用方含义 |
|---|---|
| `web_hertz.config.invalid` | typed Config 非法 |
| `web_hertz.declaration.invalid` | Option/Middleware/RouteFunc 声明非法 |
| `web_hertz.registry.unsupported` | 使用了当前不支持的 Hertz Registry |
| `web_hertz.route.register_failed` | RouteFunc 显式返回 error |
| `web_hertz.context.invalid` | Run/Shutdown 收到 nil context |
| `web_hertz.state.invalid` | 重复或并发 Run 等非法转换 |
| `web_hertz.run.failed` | Hertz 初始化、监听或 transport 运行失败 |
| `web_hertz.shutdown.failed` | 底层停止失败或当前调用者等待取消/超时；后者额外匹配 `ErrShutdownWaitInterrupted` |

使用 `errcode.IsCode` 做稳定分支，使用 `errors.Is/As` 检查底层 cause；不要解析错误文本：

```go
if errcode.IsCode[webhertz.ErrorCode](err, webhertz.ErrCodeRunFailed) {
	// 进入应用自己的启动失败处置。
}
```

## 10. 测试建议

1. 测试路由时优先使用预绑定 `127.0.0.1:0` listener，避免固定端口冲突。
2. 显式选择 Hertz standard transport，避免自定义 listener 与 netpoll 能力不匹配。
3. 从外部测试轮询健康端点或监听地址并设置 deadline，不依赖固定长 sleep；provider 没有公共 readiness 状态。
4. 生命周期测试必须给 goroutine/result channel 设置超时，失败时不能永久挂起。
5. 覆盖 context 取消、端口冲突、并发 Shutdown 和短 deadline 不污染共享结果。

## 11. 常见问题

| 现象 | 原因 | 处理 |
|---|---|---|
| 调用 `Run` 后不能继续执行当前 goroutine | Run 是阻塞工作负载入口 | 由应用 Runner 调用，或在受控 goroutine 中运行并收集 error |
| Shutdown 超时后服务仍在停止 | caller context 只限制当前等待 | 用新的 context 查询同一个共享停止结果 |
| 第二次 Run 返回状态错误 | Server 是单运行周期对象 | 重新构造 Server |
| `WithRegistry` 构造失败 | 直接 Run 没有 Spin 注册 hook | 不在当前 provider 中配置 Registry，等待独立注册生命周期合同 |
| Controller 无法直接传入 | provider 不定义 Controller | 使用 `app-web-hertz` 聚合 Controller/Filter |
| Zap 日志没有生效 | hlog Logger 尚未在构造前安装 | 纯 provider 用 `Install`；应用装配用 `NewFromProvider + WithHLog` |

## 12. RouteFunc 迁移

预发布合同从 `RouteFunc(route.IRouter)` 收窄为 `RouteFunc(*route.RouterGroup)`。通常只需修改参数类型，`GET/POST/Group/Use` 调用保持不变。新合同固定传入共享根 `Group("/")`，并从类型层阻止回调断言为 `*server.Hertz` 后绕过生命周期。

## 13. 相关文档

1. [web-hertz README](../../web-hertz/README.md)
2. [web-hertz 架构设计](../architecture/web-hertz-design.md)
3. [根 web 合同](../../web/README.md)
4. [quickstart-web-hertz](../../examples/quickstart-web-hertz/README.md)
5. [hlog-zap](../../hlog-zap/README.md)
6. [测试规范](../testing/testing.md)
7. [app-web-hertz 使用指南](app-web-hertz-usage.md)
