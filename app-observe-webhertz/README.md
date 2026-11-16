# app-observe-webhertz

`app-observe-webhertz` 把 Hertz 请求按需适配为 Prometheus HTTP 指标和 OTel HTTP server Span。包名为 `webhertzobserve`。

## 设计范围与目标

### In Scope

1. 创建实现完成态/开始态观察合同的 Prometheus Collector。
2. 支持 owned-private 或 borrowed-shared Registry，并由 Runtime/managed bean 注销自身 Collector。
3. 提供不拥有 listener/Server 的 Prometheus Handler。
4. 通过可选 IOC Module 把适配结果声明给 `app-web-hertz`。
5. 借用显式 `TracerProvider`/`TextMapPropagator` 创建原子 Hertz Instrumentation Bundle，不访问 OTel global。
6. 按需读取独立 Metrics Server 配置，并由应用 Runner 监督业务与抓取两个工作负载。

### Out of Scope

1. Metrics/Tracing adapter 本身不读取配置；只有显式 `WithMetricsServer` 才读取独立 scope。
2. 不处理 Access Log，不创建/关闭 Prometheus shared Registry、OTel Provider 或 Exporter。Access Log 是 `app-web-hertz` 的可选基础组件；它可以消费 Trace ID 做信号关联，但不归本模块安装。
3. 不隐式启动 HTTP Server、listener 或 goroutine；Metrics Server 只由显式 Runner 启动，也不注册 pprof/健康检查路由。
4. 不访问 `prometheus.DefaultRegistry`，不修改 Hertz 全局 hlog。
5. 不替应用决定 SDK Sampler/Exporter；采样策略引用只做启动期一致性校验。

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `PrometheusObserver` | 实现完成态、开始态和 `prometheus.Collector` 合同 |
| `PrometheusRuntime` | 原子持有 Collector registration 与 Handler |
| `PrometheusRegistry` | shared 模式要求的 Registerer/Gatherer 组合合同 |
| `PrometheusHandler` | 供诊断 Router 注入的窄 HTTP Handler 合同 |
| `MetricsServerConfig` | 独立抓取端口的地址、路径与超时快照 |
| `TracerProvider` | Provider owner 发布给 adapter 的 borrowed 语义引用 |
| `InstrumentationBundle` | 成对提供 Hertz internal tracer 与 Server Middleware |
| `ServerSpanAttributeEnricher` | 基于安全完成态快照追加低风险 Attribute |
| `Module` | 按需注册单个原子 managed adapter bean |
| `Runner` | 解析业务 Server，并按声明选择单工作负载或业务/Metrics 双工作负载监督 |

## 公共入口与合同

`app-web-hertz` 从 IOC 聚合 Metrics 和 Trace 组件，推荐通过本模块声明所需适配器：

```go
observeModule := webhertzobserve.NewModule(
    webhertzobserve.WithPrivatePrometheus(),
    webhertzobserve.WithMetricsServer(),
    webhertzobserve.WithOpenTelemetry(
        webhertzobserve.WithSamplingPolicyReference("parentbased_ratio_10"),
        webhertzobserve.WithTracingIgnoredPaths("/healthz", "/readyz"),
    ),
)

err := app.Run(
    app.WithModules(observeModule, appwebhertz.NewModule()),
    app.WithRunner(webhertzobserve.Runner()),
)
```

shared Prometheus 模式要求 IOC 中已声明 borrowed `PrometheusRegistry`。OTel 模式要求应用 owned managed bean 分别声明 `TracerProvider` 和 `propagation.TextMapPropagator`；前者创建 SDK Provider/Exporter/Sampler 并在关闭时负责 `ForceFlush`/`Shutdown`。Module 只注册 adapter bean。

`NewPrivatePrometheusRuntime` 可用于自定义 IOC Module；创建结果必须声明为对应语义接口，再由 `app-web-hertz` 注入。

## 配置与默认值

Prometheus Registry、namespace/subsystem/bucket，以及 Trace span name、ignore path、sampling policy reference、Attribute Enricher 均通过对象和 typed Option 声明，不进入 `web.hertz`。Access Log 由 `app-web-hertz` 直接借用根 `LoggerFactory` 实现，不属于本模块。

`WithMetricsServer()` 是显式拓扑开关；未声明时不读取下列配置，也不启动额外工作负载。声明后默认读取 `observability.metrics.server`，该默认 scope 缺失时使用安全默认值：

```yaml
observability:
  metrics:
    server:
      address: 127.0.0.1:9099
      path: /metrics
      readHeaderTimeout: 3s
      shutdownTimeout: 5s
```

`WithMetricsServerScope("services.api.metrics")` 可选择自定义 scope；自定义 scope 缺失时启动失败，避免部署拼写错误静默回退。地址和路径只在 IOC Init 读取一次，不支持运行期切换。`/` 路径、非 `host:port` 地址和非正超时会 fail-fast。

Trace 默认只记录 `http.request.method`、稳定 `http.route`、`http.response.status_code` 和受控 `error.type`；不记录完整 URL、query、Header、Cookie、Body、panic 值或 Stack。未匹配路由固定为 `_unmatched`。Request ID 默认与 Trace ID 独立；需要对齐时显式使用 `NewTraceAlignedRequestIDGenerator(fallback)`，无有效 Span 时一定调用 fallback。

Prometheus 默认名称：

| 名称 | 类型 | Labels |
|---|---|---|
| `gobase_http_server_requests_total` | Counter | method, route, status, outcome |
| `gobase_http_server_request_duration_seconds` | Histogram | method, route, outcome |
| `gobase_http_server_request_bytes` | Histogram | method, route |
| `gobase_http_server_response_bytes` | Histogram | method, route, status |
| `gobase_http_server_in_flight_requests` | Gauge | method |

未知路由固定为 `_unmatched`，非标准 Method 固定为 `_other`；Request ID、Trace ID 和原始 path 不作为默认 Label。

## 错误语义

| ErrorCode | 语义 |
|---|---|
| `app_observe_webhertz.declaration_invalid` | Option、Registry、Provider/Propagator、采样引用或 bucket 声明非法 |
| `app_observe_webhertz.registration_failed` | Collector 注册/注销失败 |
| `app_observe_webhertz.module_register_failed` | 原子 adapter bean 无法注册到 IOC |
| `app_observe_webhertz.metrics_server_config_invalid` | Metrics Server 配置值非法 |
| `app_observe_webhertz.metrics_server_config_load_failed` | Config Manager、Snapshot 或 scope 读取失败 |
| `app_observe_webhertz.metrics_server_listen_failed` | 业务 Server 启动前无法预绑定 Metrics 端口 |
| `app_observe_webhertz.metrics_server_run_failed` | 业务/Metrics 工作负载运行或关闭异常 |
| `app_observe_webhertz.runner_resolve_failed` | Runner 无法解析唯一业务 Server 或 Metrics 计划 |

错误携带 `op/result/stage/reason` 并保留底层 cause。

## 运行时与所有权语义

1. private Runtime 拥有私有 Registry；shared Runtime 借用 Registry，但拥有并注销自身 Collector registration。
2. Module 使用单个 managed bean 原子发布所选接口；Tracing/Prometheus 初始化失败时不会发布半初始化委托。
3. OTel adapter 借用 Provider/Propagator，只创建无资源 Bundle；Provider owner 必须覆盖 Web Server/adapter 生命周期。
4. `app-web-hertz` Server 依赖 Observer/Bundle，IOC 逆序关闭保证先停止请求，再关闭 adapter，最后由 owner Flush/Shutdown Provider。
5. Handler 只执行 Gather；未启用 Metrics Server 时不拥有 listener 或主动工作负载。
6. 启用 Metrics Server 后，Module Init 只冻结配置；Runner 先预绑定端口，再并发运行两个 Server，任一退出或 context 取消都会触发统一关闭并等待收敛。
7. Metrics Server 只挂载配置的 Prometheus path，不隐式开放 health、pprof 或业务路由。
8. Trace 批处理/远程导出 goroutine 只由显式 Provider owner 创建和停止。

## 最佳实践

1. 应用已有聚合 Registry 时使用 shared 模式，并以 borrowed 语义注册。
2. Metrics 只使用 Method、RoutePattern、Status、Outcome；原始动态路径不进入默认 Label。
3. `/metrics` 应挂到独立诊断端口并配置网络访问控制。
4. Sampler 在 Provider 构造时固定；adapter 的 sampling policy reference 用于阻止接错 Provider，不做请求期动态采样。
5. Attribute Enricher 只接收 Method/Route/Status/Panicked 安全快照；保留键及 authorization/cookie/token/body/query 等键会被过滤。

## Open Gaps / Future Improvements

1. 当前内建 Server 只暴露 Metrics；health、ready、pprof 是否共享端口需要先定义安全和生命周期合同。
2. Histogram 默认 bucket 仍需结合真实流量评估，但发布后的名称/Label 视为稳定查询合同。
3. 当前 Trace 只覆盖 HTTP server；client/RPC instrumentation 应按各协议独立定义 Bundle 与所有权。

## File Map

1. `prometheus.go`：Collector、Runtime、注册所有权和 Handler。
2. `prometheus_registry.go`、`prometheus_handler.go`：Registry/Handler 公共语义合同。
3. `tracer_provider.go`：borrowed Provider 与 sampling policy reference 语义合同。
4. `server_span*.go`、`tracing.go`：安全 Span 快照、Enricher、传播、Hertz Tracer/Middleware 和 Trace ID 对齐策略。
5. `module.go`：IOC 原子 managed adapter 声明与生命周期。
6. `metrics_server_config.go`：独立抓取端口的配置 scope、默认值与校验。
7. `runner.go`：业务 Server 与可选 Metrics Server 的启动、监督和统一关闭。
8. `value.go`：跨适配能力共用的 typed-nil 依赖校验。
9. `errors.go`：稳定错误码和阶段包装。
10. `*_test.go`：指标合同、Trace 传播/安全/panic、Module、Runner 和资源所有权测试。

## 相关文档

1. [可观测性入门](../docs/guides/observability-primer.md)
2. [Web 可观测性提案](../docs/architecture/web-observability-proposal.md)
3. [app-web-hertz README](../app-web-hertz/README.md)
4. [Web 可观测性使用指南](../docs/guides/web-observability.md)
5. [高级可观测性 Quickstart](../examples/quickstart-app-observe-webhertz/README.md)
