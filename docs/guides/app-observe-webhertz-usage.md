# app-observe-webhertz 使用指南

`app-observe-webhertz` 为 `app-web-hertz` 提供 Prometheus HTTP 指标、可选
OpenTelemetry HTTP Server Span，以及独立的 Metrics HTTP Server。

本文从使用者视角说明三件事：模块提供什么功能、如何安装这些能力、如何配置运行
策略。Metrics、Logs、Traces 的基础概念见[可观测性入门](observability-primer.md)。

Access Log、Request ID、Recovery、CORS 和 Gzip 属于 `app-web-hertz`，不由本模块
安装；相关配置见 [app-web-hertz 使用指南](app-web-hertz-usage.md)。

## 1. 提供的功能

### 1.1 Prometheus HTTP Server 指标

模块默认安装 private Prometheus Registry 和 HTTP 请求观察器。每次业务请求完成后，
会产生以下指标：

| 指标 | 类型 | Labels | 用途 |
|---|---|---|---|
| `gobase_http_server_requests_total` | Counter | method, route, status, outcome | 请求量、错误率 |
| `gobase_http_server_request_duration_seconds` | Histogram | method, route, outcome | 请求延迟分布 |
| `gobase_http_server_request_bytes` | Histogram | method, route | 请求体大小分布 |
| `gobase_http_server_response_bytes` | Histogram | method, route, status | 响应体大小分布 |
| `gobase_http_server_in_flight_requests` | Gauge | method | 当前处理中请求数 |

例如，请求 `GET /orders/42` 匹配路由 `/orders/:id` 并返回 200 后，`/metrics`
中会出现类似结果：

```text
gobase_http_server_requests_total{method="GET",outcome="success",route="/orders/:id",status="200"} 1
gobase_http_server_in_flight_requests{method="GET"} 0
```

实际请求 Path 不会进入 `route` Label；匹配成功时使用稳定的路由模板，例如实际请求
`/orders/42` 记录为 `/orders/:id`，未匹配请求统一记录为 `_unmatched`。非标准 Method
固定为 `_other`；Request ID、Trace ID、Header 和 Body 也不进入默认 Label，避免高基数
和敏感信息泄漏。

### 1.2 OpenTelemetry HTTP Server Span

显式声明 `WithOpenTelemetry()` 后，模块为业务 Hertz Server 成对安装 native Tracer
和 Server Middleware。一个成功请求会产生类似 Span：

```text
name: GET /orders/:id
kind: SERVER
attributes:
  http.request.method: GET
  http.route: /orders/:id
  http.response.status_code: 200
```

5xx 与穿透 panic 会设置稳定错误状态和 `error.type`，但不会记录原始错误文本、panic
内容、完整 URL、query、Authorization、Cookie 或 Body。入站传播上下文由应用显式
提供的 `TextMapPropagator` 提取；模块不读取 OTel global。

Tracing 默认关闭，因为 TracerProvider、Sampler、Exporter 和凭据都必须由应用拥有。

### 1.3 独立 Metrics Server

`webhertzobserve.Workload()` 运行独立 Metrics HTTP Server。它与业务 Hertz Server
是两个并列 Workload：

| Workload | 稳定名称 | 监听内容 |
|---|---|---|
| `appwebhertz.Workload()` | `web.hertz` | 业务路由 |
| `webhertzobserve.Workload()` | `web.hertz.metrics` | 仅配置的 Metrics Path |

默认端点是 `http://127.0.0.1:9099/metrics`。内建 Server 不开放 health、readiness、
pprof 或业务路由，也不会把 `/metrics` 隐式挂到公网业务端口。

Module 在 IOC Init 预绑定 Metrics listener，因此端口冲突会阻止全部 Workload 启动，
不会留下“业务 Server 已启动、Metrics Server 启动失败”的部分运行状态。

### 1.4 默认启用状态

| 能力 | 默认状态 | 如何改变 |
|---|---|---|
| private Prometheus Registry 与 Observer | 启用 | `WithSharedPrometheus()` 改为 borrowed shared Registry |
| Metrics Server Resource | 启用并在 IOC Init 预绑定 | 运行仍需显式加入 `webhertzobserve.Workload()` |
| OTel Server Span | 关闭 | `WithOpenTelemetry()` |
| Formatter / Enricher | 默认安全实现 / 无 Enricher | 通过 `WithOpenTelemetry(...)` 的代码 Option 扩展 |

## 2. 如何安装

### 2.1 最小安装：Prometheus + 独立 Metrics Server

在应用中同时加入 observe Module 和 observe Workload：

```go
err := app.Run(
    app.WithLogging(apphlogzap.Wrap(applogzap.New())),
    app.WithBeanDefinitions(
        appwebhertz.DefineController(&OrderController{}),
    ),
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

安装后的验证方式：

```shell
curl http://127.0.0.1:9099/metrics
```

只注册 `NewModule()` 而不加入 `webhertzobserve.Workload()` 时，Observer 和 Handler
会被装配，但独立 Metrics Server 不会主动运行。Module 不会在后台隐藏启动 goroutine。

### 2.2 安装 OpenTelemetry

应用需要声明自己拥有的 `TracerProvider` 和显式 Propagator，再启用 OTel：

```go
err := app.Run(
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
        appwebhertz.NewModule(),
        webhertzobserve.NewModule(
            webhertzobserve.WithOpenTelemetry(),
        ),
    ),
    app.WithWorkloads(
        appwebhertz.Workload(),
        webhertzobserve.Workload(),
    ),
)
```

Provider owner 至少需要实现以下合同，并在 `Close` 中 Flush/Shutdown 自己拥有的 SDK
资源：

```go
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

`samplingPolicyReference` 只是启动期一致性校验，不会修改 Provider 已冻结的 Sampler。
配置引用和 Provider 返回值不一致时，IOC Build fail-fast。

### 2.3 使用 shared Prometheus Registry

需要聚合 Go、Process 或业务 Collector 时，由应用创建 Registry，并以 borrowed 语义
发布：

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
    webhertzobserve.WithSharedPrometheus(),
)
```

模块只拥有并注销自己的 Collector registration，不关闭或清空 borrowed Registry。
默认不使用 `prometheus.DefaultRegistry`，避免全局注册污染测试和多 Runtime 场景。

### 2.4 Formatter 与 Enricher

Formatter 和 Enricher 包含函数或接口身份，不能从 ConfigManager 反序列化，因此保留
代码声明：

```go
webhertzobserve.NewModule(
    webhertzobserve.WithOpenTelemetry(
        webhertzobserve.WithServerSpanNameFormatter(formatSpanName),
        webhertzobserve.WithServerSpanAttributeEnrichers(environmentEnricher),
    ),
)
```

Enricher 只接收 `ServerSpanSnapshot` 安全快照。模块会过滤核心 HTTP Attribute、空键
以及疑似 authorization、cookie、credential、password、secret、token、body、query
的字段；Enricher panic 也不会破坏业务请求。

## 3. 如何配置

### 3.1 默认 scope 与加载规则

配置写入应用的 ConfigManager 来源即可。例如显式读取本地文件：

```go
app.Run(
    app.WithConfig(
        config.File(config.FilePath("application.yaml")),
    ),
    // Modules、Workloads 与其他应用声明……
)
```

未显式指定文件时，ConfigManager 仍遵循自身的默认文件发现规则；生产环境也可以使用
其他 Config Provider。observe Module 只消费启动时的当前 Snapshot，不拥有配置来源。

默认 ConfigManager scope 是：

```text
observability.webHertz
```

配置在 IOC Init 只读取一次，形成统一启动快照，不进行热更新。加载优先级是：

```text
显式 ConfigManager 字段 > DefaultConfig()
```

具体规则：

1. 没有 ConfigManager 或默认 scope 不存在时，使用完整 `DefaultConfig()`；
2. 默认 scope 只配置部分字段时，其余字段保留框架默认值；
3. `WithConfigScope(...)` 指定的自定义 scope 不存在时 fail-fast，避免拼写错误被静默掩盖；
4. 配置只改变已安装能力的运行策略，不会自动安装 OTel 或改变 Registry 所有权。

### 3.2 完整默认配置

下面的 YAML 与 `DefaultConfig()` 语义一致：

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

默认只监听 loopback，适合本机开箱验证。容器内需要由 Pod 外部抓取时，通常应显式
配置 `0.0.0.0:<port>`，并配套 NetworkPolicy、sidecar、mTLS 或鉴权。

### 3.3 配置项说明

| 配置项 | 默认值 | 语义与约束 |
|---|---|---|
| `prometheus.namespace` | `gobase` | 指标顶级前缀；修改会改变 Dashboard/Alert 查询合同 |
| `prometheus.subsystem` | `http_server` | HTTP Server 指标二级前缀 |
| `prometheus.buckets.duration` | Prometheus 默认秒分桶 | 必须全部大于零且严格递增 |
| `prometheus.buckets.requestBytes` | 128B～2MB 指数分桶 | 必须全部大于零且严格递增 |
| `prometheus.buckets.responseBytes` | 128B～2MB 指数分桶 | 必须全部大于零且严格递增 |
| `prometheus.server.address` | `127.0.0.1:9099` | 必须是合法 `host:port`；允许端口 `0` 用于测试 |
| `prometheus.server.path` | `/metrics` | 必须是非根绝对路径，不能包含 query/fragment |
| `prometheus.server.readHeaderTimeout` | `3s` | 必须为正时长 |
| `tracing.ignoredPaths` | `[]` | 精确绝对路径；重复项去重，不能包含 query/fragment |
| `tracing.samplingPolicyReference` | `""` | 空值不校验；非空时必须匹配 Provider owner |

namespace、subsystem 和 buckets 会直接影响外部指标合同，建议在 Dashboard/Alert 发布后
保持稳定。空 bucket 集合回退默认分桶；非法非空分桶会使 IOC Build 失败。

### 3.4 部分覆盖示例

只修改指标前缀、抓取地址和 Trace 策略时，不需要复制完整默认配置：

```yaml
observability:
  webHertz:
    prometheus:
      namespace: orders
      server:
        address: 0.0.0.0:9099
    tracing:
      ignoredPaths: [/healthz, /readyz]
      samplingPolicyReference: parentbased_ratio_10
```

未声明的 subsystem、buckets、path 和 readHeaderTimeout 仍使用默认值。Tracing 配置
只有在代码中声明 `WithOpenTelemetry()` 后才会生效。

### 3.5 自定义 scope

需要复用应用自己的配置层级时：

```go
webhertzobserve.NewModule(
    webhertzobserve.WithConfigScope("services.orders.observability"),
)
```

对应配置：

```yaml
services:
  orders:
    observability:
      prometheus:
        namespace: orders
        server:
          address: 0.0.0.0:9099
```

自定义 scope 是强声明：ConfigManager 缺失、Snapshot 未初始化或路径不存在都会返回
`app_observe_webhertz.config_load_failed`。

### 3.6 Config 与代码声明边界

| ConfigManager 运行策略 | 代码声明拓扑 |
|---|---|
| namespace、subsystem | private/shared Registry |
| Histogram buckets | 是否安装 OTel |
| ignoredPaths、samplingPolicyReference | TracerProvider、Propagator、Exporter 所有权 |
| Metrics Server address/path/timeout | Span Formatter、Attribute Enricher |

判断原则：可序列化且需要按环境覆盖的运行参数进入 Config；涉及资源所有权、能力安装
或代码身份的选择留在 composition root。

## 4. 运行与关闭语义

启动流程：

```text
读取并校验统一 Config
-> 构造可选 OTel Bundle
-> 注册 Prometheus Collector
-> 预绑定 Metrics listener
-> 启动业务 Hertz Workload 与 Metrics Workload
```

关闭流程：

```text
App 取消全部 Workload Run context
-> 并发调用业务 Hertz 与 Metrics Workload Shutdown
-> 等待两个 Run 真实返回
-> IOC Close 兜底关闭 Metrics Resource、注销 Collector
-> Provider owner ForceFlush / Shutdown
```

先停止业务 Server、最后关闭 Provider，可避免 Provider 已销毁后仍产生 Span。shared
Registry、TracerProvider 和 Propagator 都是 borrowed，模块不会替应用关闭它们。

任一 Workload 意外返回都会触发 App 关闭其余 Workload。Module Init 不启动 Server
或 goroutine；即使命令未进入 Workload Run，IOC Close 也会释放已经预绑定的 listener。

## 5. 常见问题

| 现象 | 检查点 |
|---|---|
| `/metrics` 无法连接 | 是否同时声明 `NewModule()` 和 `webhertzobserve.Workload()`；地址是否只监听 loopback |
| Metrics 没有业务样本 | 是否已经产生请求；是否同时安装 `appwebhertz.NewModule()` |
| 指标 route 基数异常 | 业务路由是否提供稳定 FullPath；不要自行用原始 Path 创建 Label |
| IOC Build 报 Metrics listen 失败 | 端口是否已被占用；listener 会在任一 Workload 启动前预绑定 |
| shared 模式 Build 失败 | 是否以 `PrometheusRegistry` 接口发布 borrowed Registry |
| Provider/Propagator 缺失 | 是否声明 `TracerProvider` 与 `TextMapPropagator`，对象是否为 typed nil |
| sampling policy mismatch | Config 引用是否与 Provider owner 返回值完全一致 |
| Trace 没有父子传播 | 入站 Header 是否有效；Propagator 是否支持对应格式 |
| 自定义 scope 加载失败 | scope 是否存在且拼写完全一致；自定义 scope 不回退默认配置 |
| 关闭时丢 Span | Provider owner 是否在业务 Server/adapter 之后 ForceFlush/Shutdown |

稳定错误码：

| ErrorCode | 含义 |
|---|---|
| `app_observe_webhertz.config_invalid` | 统一配置值非法 |
| `app_observe_webhertz.config_load_failed` | Manager、Snapshot 或 scope 加载失败 |
| `app_observe_webhertz.declaration_invalid` | Option、Registry、Provider/Propagator 声明非法 |
| `app_observe_webhertz.registration_failed` | Collector 注册或注销失败 |
| `app_observe_webhertz.metrics_server_listen_failed` | Metrics listener 预绑定失败 |
| `app_observe_webhertz.metrics_server_run_failed` | Metrics Server 运行或关闭失败 |
| `app_observe_webhertz.workload_resolve_failed` | Workload 无法解析 Metrics Resource |

错误消息携带 `op/result/stage/reason` 并保留底层 cause，但不会记录 token、凭据、
Header、Cookie、Body 或完整 query。

## 6. 可运行示例与相关文档

完整示例见 [quickstart-app-observe-webhertz](../../examples/quickstart-app-observe-webhertz/)；
它演示 private Registry、应用 owned OTel Provider、统一 Config、两个 Workload 以及优雅关闭。

相关文档：

1. [app-observe-webhertz README](../../app-observe-webhertz/README.md)
2. [可观测性入门](observability-primer.md)
3. [app-web-hertz 使用指南](app-web-hertz-usage.md)
4. [web-hertz 使用指南](web-hertz-usage.md)
5. [Web 可观测性架构提案](../architecture/web-observability-proposal.md)
