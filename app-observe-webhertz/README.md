# app-observe-webhertz

`app-observe-webhertz` 把 Hertz 请求适配为 Prometheus HTTP 指标和可选 OTel HTTP server Span，并提供独立 Metrics HTTP Server。包名为 `webhertzobserve`。

## 设计范围与目标

### In Scope

1. 创建实现请求开始态/完成态观察合同的 Prometheus Collector。
2. 默认使用 owned-private Registry，也支持 borrowed-shared Registry，并只注销自身 Collector registration。
3. 发布不拥有 listener 的 `PrometheusHandler`。
4. 通过 IOC Module 把 Observer 和可选 Instrumentation Bundle 声明给 `app-web-hertz`。
5. 在 IOC Init 读取统一 Config 启动快照并预绑定独立 listener，使非法策略或端口冲突阻止全部 Workload 启动。
6. 通过独立 Workload 只运行和关闭 Metrics Server，不接管业务 Hertz Server。

### Out of Scope

1. 不处理 Access Log、Request ID、Recovery、CORS 或 Gzip。
2. 不创建或关闭 shared Registry、OTel Provider、Exporter 或 Sampler。
3. Module Init 不启动 HTTP Server 或 goroutine；主动运行只发生在显式 Workload。
4. 不访问 `prometheus.DefaultRegistry`，不修改 Hertz 全局 hlog。
5. Metrics Server 不隐式开放 health、ready、pprof 或业务路由。

## 核心对象与职责

| 对象 | 职责 |
|---|---|
| `PrometheusObserver` | 实现请求开始态、完成态和 `prometheus.Collector` 合同 |
| `PrometheusRuntime` | 持有 Collector registration 与 Gather Handler |
| `PrometheusRegistry` | shared 模式要求的 Registerer/Gatherer 组合合同 |
| `PrometheusHandler` | 供 Metrics Server 或应用诊断 Router 使用的窄 Handler 合同 |
| `Config` | 指标、分桶、Trace 策略和抓取端口的统一启动快照 |
| `MetricsServerConfig` | 统一快照中的独立抓取端口策略 |
| `TracerProvider` | Provider owner 发布给 adapter 的 borrowed 引用 |
| `InstrumentationBundle` | 成对提供 Hertz internal tracer 与 Server Middleware |
| `Module` | 原子注册 adapter，并预绑定 Metrics Server Resource |
| `Workload` | 只运行和关闭独立 Metrics Server Resource |

## 公共入口与合同

零参数 `NewModule()` 默认安装 private Prometheus 和独立 Metrics Server Resource：

```go
err := app.Run(
    app.WithModules(
        appwebhertz.NewModule(),
        webhertzobserve.NewModule(),
    ),
    app.WithWorkloads(
        appwebhertz.Workload(),
        webhertzobserve.Workload(),
    ),
)
```

两个 Workload 对应两个主动 Server：

| Workload | 稳定名称 | 所有权 |
|---|---|---|
| `appwebhertz.Workload()` | `web.hertz` | 业务 Hertz Server |
| `webhertzobserve.Workload()` | `web.hertz.metrics` | 独立 Metrics HTTP Server |

任一 Workload 返回都会触发 App 的统一关闭。App 并发调用两个 `Shutdown`，等待两个 `Run` 真实返回后才释放 IOC Resource。

需要 shared Registry 时使用 `WithSharedPrometheus()`，IOC 中必须声明 borrowed `PrometheusRegistry`。需要 OTel 时使用 `WithOpenTelemetry(...)`，并由应用 owned managed bean 分别声明 `TracerProvider` 和 `propagation.TextMapPropagator`。Provider owner 负责最终 `ForceFlush`/`Shutdown`。

## 配置模型与默认值

默认配置路径是 `DefaultConfigScope`：

```text
observability.webHertz
```

```yaml
observability:
  webHertz:
    prometheus:
      namespace: gobase
      subsystem: http_server
      buckets:
        duration: [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]
        requestBytes: [128, 512, 2048, 8192, 32768, 131072, 524288, 2097152]
        responseBytes: [128, 512, 2048, 8192, 32768, 131072, 524288, 2097152]
      server:
        address: 127.0.0.1:9099
        path: /metrics
        readHeaderTimeout: 3s
    tracing:
      ignoredPaths: []
      samplingPolicyReference: ""
```

语义：

1. 默认 scope 缺失时使用完整 `DefaultConfig()`。
2. `WithConfigScope("services.api.observability")` 可选择自定义路径；自定义路径缺失时 fail-fast。
3. ConfigManager 以 `DefaultConfig()` 为基底做字段级覆盖，并在 IOC Init 形成一次统一快照；不支持运行期切换指标合同、Trace 策略、地址或路由。
4. listener 在 IOC Init 预绑定；端口冲突时 Runtime Build 失败，两个 Workload 均不会启动。
5. `/`、非绝对路径、非法 `host:port` 和非正超时都会 fail-fast。

namespace、subsystem、buckets、ignoredPaths 和 samplingPolicyReference 属于可部署运行策略，统一进入 Config。private/shared Registry、是否安装 OTel、Span Formatter 与 Attribute Enricher 仍由代码声明；Config 不创建资源或启用能力。Access Log 属于 `app-web-hertz`，不由本模块安装。

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
| `app_observe_webhertz.declaration_invalid` | Option、Registry、Provider/Propagator 或 Handler 声明非法 |
| `app_observe_webhertz.registration_failed` | Collector 注册/注销失败 |
| `app_observe_webhertz.module_register_failed` | 原子 adapter bean 无法注册到 IOC |
| `app_observe_webhertz.config_invalid` | 统一可观测启动快照非法 |
| `app_observe_webhertz.config_load_failed` | ConfigManager、Snapshot 或统一 scope 读取失败 |
| `app_observe_webhertz.metrics_server_listen_failed` | IOC Init 无法预绑定 Metrics 端口 |
| `app_observe_webhertz.metrics_server_run_failed` | Metrics Server 运行或关闭异常 |
| `app_observe_webhertz.workload_resolve_failed` | Workload 无法解析 Metrics Server Resource |

错误携带 `op/result/stage/reason` 并保留底层 cause。

## 运行时与所有权语义

1. Module 是被动装配边界：读取配置、注册 Collector、预绑定 listener，但不启动 goroutine。
2. `appwebhertz.Workload` 只拥有业务 Server；`webhertzobserve.Workload` 只拥有 Metrics Server。
3. private Runtime 拥有私有 Registry；shared Runtime 借用 Registry，但拥有自身 Collector registration。
4. OTel adapter 借用 Provider/Propagator，只创建无资源 Bundle。
5. Metrics Workload 使用 App 传入的 graceful context 执行 `Shutdown`，必要时 force close，并等待 `Run` 返回。
6. IOC Close 会兜底关闭未运行或异常遗留的 Metrics Resource，再注销 Collector。
7. `app-web-hertz` Server 依赖 Observer/Bundle；关闭顺序保证先停止两个 Server，再关闭 adapter，最后由 owner Flush/Shutdown Provider。

## 最佳实践

1. Metrics 端口与业务端口分离，并通过 NetworkPolicy、sidecar、mTLS 或鉴权限制访问。
2. 应用已有聚合 Registry 时使用 shared 模式，并以 borrowed 语义注册。
3. Metrics 只使用 Method、RoutePattern、Status、Outcome；实际请求 Path 不进入默认 Label。
4. Sampler 在 Provider 构造时固定；sampling policy reference 只做启动期一致性校验。
5. Attribute Enricher 只接收安全完成态快照，敏感键会被过滤。

## Open Gaps / Future Improvements

1. 当前内建 Server 只暴露 Metrics；health、ready、pprof 是否共享端口需要先定义安全合同。
2. Histogram 默认 bucket 仍需结合真实流量评估。
3. 当前 Trace 只覆盖 HTTP server；client/RPC instrumentation 应独立定义合同。

## File Map

1. `prometheus.go`：Collector、Runtime、注册所有权和 Handler。
2. `prometheus_registry.go`、`prometheus_handler.go`：Registry/Handler 公共合同。
3. `tracer_provider.go`：borrowed Provider 与 sampling policy reference 合同。
4. `server_span*.go`、`tracing.go`：Span、传播、Hertz Tracer/Middleware 和 Trace ID 对齐策略。
5. `module.go`：IOC 原子 adapter、Metrics Resource 装配与生命周期。
6. `config.go`：统一配置 scope、完整默认快照与加载语义。
7. `metrics_server_config.go`：抓取 Server 子配置默认值与校验。
8. `metrics_server.go`：预绑定 listener、Metrics Server Run/Shutdown。
9. `workload.go`：独立 Metrics Server 的 App Workload 适配器。
10. `errors.go`：稳定错误码和阶段包装。
11. `*_test.go`：配置、指标、Trace、Module、Workload 和资源所有权测试。

## 相关文档

1. [可观测性入门](../docs/guides/observability-primer.md)
2. [Web 可观测性提案](../docs/architecture/web-observability-proposal.md)
3. [app-web-hertz README](../app-web-hertz/README.md)
4. [app-observe-webhertz 使用指南](../docs/guides/app-observe-webhertz-usage.md)
5. [高级可观测性 Quickstart](../examples/quickstart-app-observe-webhertz/README.md)
