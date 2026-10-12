# quickstart-app-web-hertz

本示例展示 `app-web-hertz` 的完整应用装配路径：配置化标准 Middleware、Request Log、应用 owned 的
Prometheus Registry、独立诊断 HTTP Server，以及业务 Hertz/诊断 Server 的统一生命周期监督。诊断能力属于应用
composition root，不进入 `web.hertz` 或 `app-observe-webhertz` 的资源所有权。
OTel Trace 也由应用 owned Provider bean 管理，默认关闭且不使用 OTel global。

## 运行

在仓库根目录执行：

```powershell
./scripts/go.ps1 -C examples run ./quickstart-app-web-hertz
```

业务端口默认监听 `127.0.0.1:18081`，诊断端口默认监听 `127.0.0.1:18082`。两者都默认只绑定 loopback；
可分别通过 `APP_WEB_HERTZ_ADDRESS` 和 `APP_WEB_HERTZ_DIAGNOSTICS_ADDRESS` 覆盖：

```powershell
$env:APP_WEB_HERTZ_ADDRESS = "127.0.0.1:28081"
$env:APP_WEB_HERTZ_DIAGNOSTICS_ADDRESS = "127.0.0.1:28082"
./scripts/go.ps1 -C examples run ./quickstart-app-web-hertz
```

Trace 默认关闭。显式启用后，quickstart 创建应用独占 SDK Provider、Batch Processor 和安全最小 stdout
Exporter；关闭时先 `ForceFlush` 再 `Shutdown`：

```powershell
$env:APP_WEB_HERTZ_TRACING_ENABLED = "true"
./scripts/go.ps1 -C examples run ./quickstart-app-web-hertz
```

## 请求

```powershell
Invoke-RestMethod http://127.0.0.1:18081/api/hello
Invoke-RestMethod -Method Post http://127.0.0.1:18081/api/hello
Invoke-RestMethod http://127.0.0.1:18081/api/users/gobase
Invoke-WebRequest http://127.0.0.1:18081/api/hello -Headers @{Origin="http://localhost:3000"; "Accept-Encoding"="gzip"}
Invoke-WebRequest http://127.0.0.1:18082/metrics
```

响应头包含 `X-Request-ID`，控制台同时输出不包含 query/Header/Body 的结构化 `http_request` 完成事件。`GetOrPostHello` 自动生成同一路径的 GET/POST 路由；`RegisterRoutes` 在 GroupPath 已生效的 RouterGroup 上声明动态路径。

## 装配要点

1. `app` 构建 config、Zap logging 和 IOC Runtime，并在退出时按 Runtime 所有权逆序关闭。
2. 应用创建独占 Registry；`app-observe-webhertz.WithSharedPrometheus()` 借用它并拥有 Web Collector
   registration，业务或 runtime Collector 只有被应用显式声明后才会进入最终 Handler。
3. `web.hertz.middleware` 显式启用标准能力；Access Log 启用但缺少 Request Log Observer 时 Build fail-fast。
4. BeforeRun 只解析业务 Server、应用私有 `quickstart.diagnostics` 配置和 Metrics Handler，不启动 listener。
5. 诊断 Server 使用独立 `http.Server`/`ServeMux`；`/metrics` 显式注册，pprof 默认关闭且不会进入业务端口。
6. 同一个 Workload Controller 同时接入 app Runner 和 Shutdown Hook；任一 Server 意外退出都会取消另一个，
   Hook 等待两者 Run 完全返回后才允许 IOC/Registry 关闭。
7. `quickstart.diagnostics.enabled=false` 时只创建业务工作负载，不创建诊断路由、listener 或 goroutine。
8. Go/Process Collector 默认不注册；需要时必须在应用 Registry 声明中显式启用。业务 Collector 也通过同一
   声明列表加入，不访问 Prometheus DefaultRegistry/DefaultGatherer。
9. Trace 开关关闭时不声明 Provider/Bundle、不启动 Batch goroutine；开启时 Provider owner 先于 adapter/Web
   初始化，关闭顺序相反。Adapter 只借用 Provider/Propagator，不访问 OTel global。
10. Trace 默认忽略 `/healthz`、`/readyz`，Span 只输出受控名称、Trace/Span ID 和状态，不输出 URL、query、
    Header、Cookie、Body、panic 值或 Stack。Request ID 默认与 Trace ID 独立。

pprof 可通过应用配置覆盖显式开启：

```powershell
./scripts/go.ps1 -C examples run ./quickstart-app-web-hertz --override quickstart.diagnostics.pprofEnabled=true
Invoke-WebRequest http://127.0.0.1:18082/debug/pprof/
```

对 Pod 外开放诊断地址时，应同时配置 NetworkPolicy、sidecar 或显式鉴权；示例不提供不安全的公网默认值。

## File Map

1. `main.go`：app Config/Logging/Module、Runner 与 Shutdown Hook 装配。
2. `metrics_registry.go`：应用 owned Registry，显式聚合 HTTP/Go/Process/业务 Collector。
3. `tracing_provider.go`：默认关闭的应用 owned SDK Provider、Batch Exporter、传播与 Flush/Shutdown。
4. `diagnostics_config.go`：应用私有配置、安全默认值和启动前校验。
5. `diagnostics_server.go`：独立 listener、私有 ServeMux 与 graceful→force 生命周期。
6. `diagnostics_pprof.go`：基于 `runtime/pprof`/`runtime/trace` 的私有 pprof 路由；不导入会修改
   `DefaultServeMux` 的 `net/http/pprof`。
7. `workload_assembly.go`、`workload_controller.go`：同一 Controller 的 Prepare/Run/Shutdown 与多工作负载监督。
8. `*_test.go`：路由、Trace Provider 生命周期、外部抓取、端口冲突、错误 Join、并发关闭、race 和资源所有权验证。

更多路由、错误和迁移规则见 [app-web-hertz 使用指南](../../docs/guides/app-web-hertz-usage.md)。
