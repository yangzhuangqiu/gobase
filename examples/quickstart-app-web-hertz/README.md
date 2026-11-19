# quickstart-app-web-hertz

本示例展示 `app-web-hertz` 当前推荐的单 Server 装配路径：

1. `app` 创建 Config、Logging 和 IOC Runtime，`apphlogzap.Wrap(applogzap.New())` 让业务 Logger 与 Hertz hlog 共享 Zap Provider；
2. `WithBeanDefinitions` 注册 Controller、Filter、NoRouteHandler 和 NoMethodHandler；
3. `appwebhertz.NewModule()` 在 IOC Init 阶段聚合全部组件并构建唯一 `web.Server`；
4. `appwebhertz.Workload()` 声明该 Server 的 Run/Shutdown 语义；
5. context 取消后，app 先等待 Workload 收敛，再由 IOC 逆序释放 Resource。

## 核心装配

```go
app.Run(
	app.WithConfig(
		config.File(config.FileDisabled()),
		config.Defaults(defaultConfig()),
	),
	app.WithLogging(apphlogzap.Wrap(applogzap.New())),
	app.WithBeanDefinitions(
		appwebhertz.DefineController(&GreetingController{}),
		appwebhertz.DefineFilter(&globalHeaderFilter{}),
		appwebhertz.DefineFilter(&apiHeaderFilter{}),
		appwebhertz.DefineNoRouteHandler(&jsonNoRouteHandler{}),
		appwebhertz.DefineNoMethodHandler(&jsonNoMethodHandler{}),
	),
	app.WithModules(appwebhertz.NewModule()),
	app.WithWorkloads(appwebhertz.Workload()),
)
```

`apiHeaderFilter` 同时实现 `Filter` 和 `GroupedFilter`，但仍统一通过 `DefineFilter` 声明。是否全局由实现类型决定：

- 普通 `Filter` 注册到 Hertz `engine.Use`；
- `GroupedFilter` 注册到 `GroupPath` 对应的共享基础分组；
- Controller 从基础分组获得 `Group("")` 子分组，因此继承分组 Filter，Controller 自身的 `Use` 不会泄漏。

关闭预算来自 ConfigManager 启动快照；代码显式 `WithShutdownPolicy` 时按字段覆盖：

```yaml
app:
  shutdown:
    workloadGracePeriod: 15s
    runtimeCloseGracePeriod: 10s
```

## 运行

在仓库根目录执行：

```powershell
./scripts/go.ps1 -C examples run ./quickstart-app-web-hertz
```

默认监听 `127.0.0.1:18081`。可通过环境变量覆盖：

```powershell
$env:APP_WEB_HERTZ_ADDRESS = "127.0.0.1:28081"
./scripts/go.ps1 -C examples run ./quickstart-app-web-hertz
```

## 请求

```powershell
# 自动注册的 GET/POST 路由
Invoke-RestMethod http://127.0.0.1:18081/api/hello
Invoke-RestMethod -Method Post http://127.0.0.1:18081/api/hello

# RouteRegistrar 注册的动态路由
Invoke-RestMethod http://127.0.0.1:18081/api/users/gobase

# NoRouteHandler
Invoke-WebRequest http://127.0.0.1:18081/missing

# NoMethodHandler；配置已启用 handleMethodNotAllowed
Invoke-WebRequest -Method Delete http://127.0.0.1:18081/api/hello
```

`/api/*` 响应同时包含：

- `X-Quickstart-Global: true`：来自全局 Filter；
- `X-Quickstart-Group: api`：来自 `/api` 分组 Filter；
- `X-Request-ID`：来自配置启用的标准 Middleware。

## 装配语义

1. `NewModule` 只注册 managed Server bean，不在 Register 阶段读取配置或启动监听。
2. IOC Build 时，`managedServer.Init` 读取一次 `web.hertz`，聚合全部语义组件并调用私有 builder。
3. Controller、全局/分组 Filter、404/405 fallback 在 provider 创建前统一校验；任一声明非法都不会发布半初始化 Server。
4. `Workload` 解析同一个 `web.Server`，分别委托 `Run` 与 `Shutdown`，不复制 Hertz 生命周期状态机。
5. app 负责信号、Workload 收敛和 Runtime Close，因此单 Server 场景不需要自定义 Shutdown Hook。
6. `hlog-zap/app` 在 IOC Build 前安装 hlog，在 IOC/Server 关闭后恢复 hlog，再关闭 Zap Provider。
7. 本 Module 明确限定单 Runtime 单业务 Server；Metrics 诊断端口使用专用 Workload，pprof 或 Trace 属于 composition root 的高级能力。

## File Map

1. `main.go`：组件合同、Definition、配置、Module 和 Workload 装配。
2. `main_test.go`：路由语义、环境覆盖和完整 IOC Server 依赖图验证。

更多路由、错误和装配规则见 [app-web-hertz 使用指南](../../docs/guides/app-web-hertz-usage.md)。
