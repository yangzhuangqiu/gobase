# Web 可观测性使用指南

本文说明如何组合 `app-web-hertz` 与 `app-observe-webhertz`，为业务 Hertz Server
安装 Prometheus Metrics、OpenTelemetry Tracing 和独立诊断端口。

如果还不清楚 Metrics、Logs、Traces 分别解决什么问题，或者不知道 Prometheus、
OpenTelemetry、Collector、Grafana 各自扮演什么角色，请先阅读
[可观测性入门](observability-primer.md)。本文假设这些概念已经理解，只说明装配方法。

Access Log、Request ID、Recovery、CORS、Gzip 属于 `app-web-hertz` 的可选基础
组件，不在本指南中安装。它们的配置和行为见
[app-web-hertz 使用指南](app-web-hertz-usage.md)。

## 1. 使用范围与前置条件

本指南适用于已经使用以下入口的应用：

- `app.WithBeanDefinitions(...)`；
- `app.WithModules(...)`；
- `app-web-hertz.NewModule()`；
- `app-observe-webhertz.Workload()`。

三个模块的职责：

| 模块 | 使用阶段职责 |
|---|---|
| `web-hertz` | 提供请求完成事件和 Hertz 原生扩展 |
| `app-web-hertz` | 从 IOC 聚合 Observer/Bundle 并构造业务 Server |
| `app-observe-webhertz` | 提供 Prometheus/OTel 适配 bean，以及可选 Metrics Server 复合 Workload |

应用必须显式拥有外部资源：shared Registry、TracerProvider 和 Exporter。
adapter 默认不读取配置或启动 listener；只有声明 `WithMetricsServer()` 时才读取独立
scope，并由显式 `Workload()` 创建和监督 Metrics HTTP Server。

## 2. 最小组合

同时启用 private Prometheus 和 OTel：

```go
app.Run(
    app.WithLogging(apphlogzap.Wrap(applogzap.New())),
    app.WithBeanDefinitions(
        appwebhertz.DefineController(&OrderController{}),
        ioc.Define(
            &traceProviderOwner{},
            ioc.WithDeclare[webhertzobserve.TracerProvider](),
        ),
        ioc.Define(
            propagation.TraceContext{},
            ioc.WithDeclare[propagation.TextMapPropagator](),
        ),
    ),
    app.WithModules(
        webhertzobserve.NewModule(
            webhertzobserve.WithPrivatePrometheus(
                webhertzobserve.WithPrometheusNamespace("orders"),
            ),
            webhertzobserve.WithMetricsServer(),
            webhertzobserve.WithOpenTelemetry(
                webhertzobserve.WithSamplingPolicyReference("parentbased_ratio_10"),
                webhertzobserve.WithTracingIgnoredPaths("/healthz", "/readyz"),
            ),
        ),
        appwebhertz.NewModule(),
    ),
    app.WithWorkloads(webhertzobserve.Workload()),
)
```

注册顺序表达语义依赖：

1. 应用 Definition 声明 Provider owner 与 Propagator。
2. observe Module 声明借用这些资源的 adapter。
3. app-web-hertz Module 声明消费 Observer/Bundle 的业务 Server。
4. IOC 根据真实依赖图确定 Init/Close 顺序，不依赖代码书写顺序猜测生命周期。

## 3. Prometheus

### 3.1 Private Registry

应用没有共享 Registry 时：

```go
observeModule := webhertzobserve.NewModule(
    webhertzobserve.WithPrivatePrometheus(
        webhertzobserve.WithPrometheusNamespace("orders"),
        webhertzobserve.WithPrometheusSubsystem("http_server"),
    ),
)
```

adapter 创建私有 Registry、注册 Web Collector，并发布：

- `webhertz.RequestObserver`；
- `webhertzobserve.PrometheusHandler`。

关闭时只注销本 adapter 的 Collector。private Registry 没有隐藏 Server 或 goroutine。

### 3.2 Shared Registry

应用需要聚合 Go、Process 或业务 Collector 时，由应用创建 Registry，并用 borrowed
语义发布：

```go
registry := prometheus.NewRegistry()

definitions := []ioc.Definition{
    ioc.Define(
        registry,
        ioc.WithDeclare[webhertzobserve.PrometheusRegistry](),
        ioc.WithBorrowed(),
    ),
}

observeModule := webhertzobserve.NewModule(
    webhertzobserve.WithSharedPrometheus(
        webhertzobserve.WithPrometheusNamespace("orders"),
    ),
)
```

shared Registry 的创建者负责其生命周期。adapter 只拥有自身 Collector
registration，关闭时不会关闭或清空整个 Registry。

不要使用 `prometheus.DefaultRegisterer` 或 `DefaultGatherer`，否则测试、多个
Runtime 和未知全局 Collector 会互相污染。

### 3.3 指标合同

默认指标：

| 指标 | Labels |
|---|---|
| `gobase_http_server_requests_total` | method, route, status, outcome |
| `gobase_http_server_request_duration_seconds` | method, route, outcome |
| `gobase_http_server_request_bytes` | method, route |
| `gobase_http_server_response_bytes` | method, route, status |
| `gobase_http_server_in_flight_requests` | method |

动态请求 `/orders/42` 必须记录为路由模板 `/orders/:id`。未匹配路由固定为
`_unmatched`，非标准 Method 固定为 `_other`。Request ID、Trace ID 和原始 Path
不进入默认 Label。

## 4. OpenTelemetry

### 4.1 应用 owned Provider

应用 managed bean 创建并关闭 SDK Provider：

```go
type traceProviderOwner struct {
    provider *sdktrace.TracerProvider
}

func (this *traceProviderOwner) Init(context.Context) error {
    exporter, err := newExporter()
    if err != nil {
        return err
    }
    this.provider = sdktrace.NewTracerProvider(
        sdktrace.WithSampler(sdktrace.ParentBased(
            sdktrace.TraceIDRatioBased(0.1),
        )),
        sdktrace.WithSpanProcessor(
            sdktrace.NewBatchSpanProcessor(exporter),
        ),
    )
    return nil
}

func (this *traceProviderOwner) OTelTracerProvider() trace.TracerProvider {
    return this.provider
}

func (this *traceProviderOwner) SamplingPolicyReference() string {
    return "parentbased_ratio_10"
}

func (this *traceProviderOwner) Close(ctx context.Context) error {
    return errors.Join(
        this.provider.ForceFlush(ctx),
        this.provider.Shutdown(ctx),
    )
}
```

生产 owner 还应明确拥有 Exporter 连接、BatchSpanProcessor goroutine 和凭据。
Provider 必须覆盖 adapter 与业务 Server 的完整生命周期。

### 4.2 显式 Propagator

```go
ioc.Define(
    propagation.TraceContext{},
    ioc.WithDeclare[propagation.TextMapPropagator](),
)
```

需要 Baggage 时由应用选择组合 Propagator。adapter 不读取或修改 OTel global。

### 4.3 安装 OTel Bundle

```go
webhertzobserve.WithOpenTelemetry(
    webhertzobserve.WithSamplingPolicyReference("parentbased_ratio_10"),
    webhertzobserve.WithTracingIgnoredPaths("/healthz", "/readyz"),
)
```

sampling policy reference 必须与 Provider owner 返回值一致。它只做启动期防错，不会
改变 Provider 中已经冻结的 Sampler。

OTel adapter 原子发布一个 Hertz native Option 和一个 Server Middleware。任何一半
非法都会让 IOC Build 失败，不会留下半插桩 Server。

默认 Span：

- 名称：`METHOD route`；
- Route 使用稳定模板；
- 只记录 method、route、status 和受控 error type；
- 不记录完整 URL、query、Header、Cookie、Body、panic payload 或 Stack。

## 5. 安装与关联顺序

与观测相关的固定相对顺序：

```text
advanced outer
-> OTel instrumentation
-> Request ID
-> request completion event
-> Recovery
-> application chain
```

完整标准链由 [app-web-hertz 使用指南](app-web-hertz-usage.md) 维护。

这保证：

1. Server Span 覆盖完整业务处理。
2. 完成事件在 Recovery 返回后看到最终状态与 panic outcome。
3. Metrics 和其他完成事件消费者使用同一个稳定快照。
4. 有效 Span 的 Trace ID 可以通过 metadata bridge 写入完成事件。

应用使用 `app-web-hertz` 时，不要通过高级 Server Option 再次安装
`webhertz.WithRequestObservers`，否则同一请求会产生多个完成事件边界。

## 6. 独立 Metrics Server

开箱组合只需要显式声明拓扑和使用配套 Workload。复合 `webhertzobserve.Workload()` 替代
`appwebhertz.Workload()`，不要同时声明；二者共享业务 Server 的 `web.hertz` 语义名称，
误组合会在启动前 fail-fast：

```go
observeModule := webhertzobserve.NewModule(
    webhertzobserve.WithPrivatePrometheus(),
    webhertzobserve.WithMetricsServer(),
)

err := app.Run(
    app.WithModules(observeModule, appwebhertz.NewModule()),
    app.WithWorkloads(webhertzobserve.Workload()),
)
```

默认读取 `observability.metrics.server`；默认 scope 完全缺失时使用以下安全值：

```yaml
observability:
  metrics:
    server:
      address: 127.0.0.1:9099
      path: /metrics
      readHeaderTimeout: 3s
```

需要复用应用已有配置层级时，使用
`WithMetricsServer(WithMetricsServerScope("services.api.metrics"))`。显式自定义 scope
缺失会启动失败，不会回退到默认端口。

拓扑与参数刻意分离：

1. `WithMetricsServer()` 决定是否存在第二个 HTTP 工作负载，单靠 YAML 不会启用它。
2. Module Init 只读取并冻结配置，不创建 listener 或 goroutine。
3. Workload 先同步预绑定 Metrics listener；失败时业务 Server 尚未启动。
4. 预绑定成功后并发运行业务与 Metrics Server；任一退出或 context 取消都会进入统一关闭。
5. Workload 使用 app Shutdown Policy 传入的统一 context；预算到期后执行 force close，并仍等待两个 Run goroutine 真实收敛。

内建 Server 只挂载配置的 Metrics path，不提供 health、ready、pprof 或业务路由。
需要这些端点的应用仍可直接使用 `PrometheusHandler` 组装自己的工作负载。

`/metrics` 默认只监听 loopback。需要对 Pod 外开放时，显式配置可抓取地址，并配套
NetworkPolicy、sidecar、mTLS 或鉴权。不要把 Metrics 端点默认挂到公网业务 Router。

## 7. 生命周期与关闭顺序

```text
业务/Metrics Workload 收敛
-> app-web-hertz Server Close
-> observation adapter Close
-> TracerProvider ForceFlush / Shutdown
-> Logging / Config Close
```

原因：

1. 先停止业务 Server，避免 Provider 关闭后仍产生 Span。
2. adapter 注销自身 Prometheus Collector，但不关闭 shared Registry。
3. Provider 最后 Flush，保留尾部 Span。
4. Metrics Server 是显式复合 Workload 的内部 Server，不由 Handler 或 Module Init 隐式启动。

首次 Runtime/IOC Close deadline 必须覆盖完整优雅停止预算。短 deadline 不能保证
仍在后台停止的 Server 与 Provider 依赖保持安全。

## 8. 与基础组件的关联边界

Request ID 与 Access Log 都是 `app-web-hertz` 的可选基础组件：

- Access Log 不依赖 `app-observe-webhertz`；
- 只启用 Access Log 不需要 Registry、TracerProvider 或 Exporter；
- 只启用 Prometheus/OTel 也不要求启用 Access Log；
- OTel Bundle 可以向请求完成事件补充 Trace ID；
- Access Log 若启用，可以消费该 Trace ID 实现日志与 Span 关联。

因此“Trace ID 出现在 Access Log”是两个独立能力的关联，不表示 Access Log 归属于
可观测性适配模块。Access Log 配置、skip path 和 slow threshold 见
[app-web-hertz 使用指南](app-web-hertz-usage.md#实例日志与-access-log)。

## 9. 常见错误排查

| 现象 | 检查点 |
|---|---|
| IOC Build 报 Provider/Propagator 缺失 | 是否通过正确接口声明；对象是否为 typed nil |
| 采样引用不匹配 | Provider owner 返回值与 Module Option 是否完全一致 |
| 解析不到 `PrometheusHandler` | 是否声明 private/shared Prometheus |
| Metrics Server 未启动 | 是否同时声明 `WithMetricsServer()` 并使用本模块 `Workload()` |
| 默认端口不符合部署要求 | 配置 `observability.metrics.server.address`；容器抓取通常不能使用 loopback |
| 自定义 scope 加载失败 | `WithMetricsServerScope` 路径是否存在且拼写一致 |
| 业务端口未启动且报告 listen 错误 | Metrics 端口被占用；Workload 预绑定会保证全有或全无 |
| shared 模式 Build 失败 | 是否发布 `PrometheusRegistry` borrowed bean |
| Metrics 没有样本 | 是否产生过请求；Handler 和 Observer 是否来自同一 adapter |
| route Label 基数异常 | 是否使用原始 Path；Hertz 路由是否写入模板 FullPath |
| Trace 没有父子传播 | Propagator 是否显式注册；入站 traceparent 是否有效 |
| 关闭时丢 Span | Provider 是否在业务 Server/adapter 之后 ForceFlush/Shutdown |
| 重复 Metrics/日志事件 | 是否额外安装了第二个 completion event Middleware |

错误消息应保留 `op/result/stage/reason` 和底层 cause，但不得记录 token、凭据、
Header、Cookie、Body 或完整 query。

## 10. 可运行参考

[quickstart-app-observe-webhertz](../../examples/quickstart-app-observe-webhertz/)
展示：

- private Prometheus Registry；
- 应用 owned OTel Provider；
- IOC 自动注入 Observer/Bundle；
- 配置驱动的独立 Metrics 端口；
- 动态路由模板指标；
- Request ID、Trace ID 关联；
- 模块 Workload 的双 Server 监督、强收敛屏障和优雅关闭。

控制台 Exporter 与 AlwaysSample 只用于教学；生产环境应替换为批处理远程 Exporter
和符合容量预算的 Sampler。

## 11. 相关文档

1. [可观测性入门](observability-primer.md)
2. [Web 可观测性提案](../architecture/web-observability-proposal.md)
3. [app-web-hertz 使用指南](app-web-hertz-usage.md)
4. [web-hertz 使用指南](web-hertz-usage.md)
5. [app-observe-webhertz README](../../app-observe-webhertz/README.md)
6. [高级可运行示例](../../examples/quickstart-app-observe-webhertz/README.md)
