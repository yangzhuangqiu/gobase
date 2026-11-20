# Web 可观测性提案

本文是一份架构决策文档，定义 Hertz Web 请求接入 Prometheus Metrics 与
OpenTelemetry Tracing 的跨模块方案。提案只讨论可观测性适配、诊断暴露和资源
所有权；Server、Router、Controller、Filter 以及 Access Log 等基础能力分别由
`web-hertz` 和 `app-web-hertz` 的稳定设计负责。

> 第一次接触可观测性时，不建议直接从“稳定扩展合同”开始。请先阅读
> [可观测性入门](../guides/observability-primer.md)，了解可观测性解决的问题、
> Metrics/Logs/Traces/Profiles 的区别、事故调查路径和业界常见组合。

## 如何阅读本提案

本文回答的是“gobase 应该怎样提供 Web 可观测性能力”，不是一套通用概念教程。
推荐按以下顺序阅读：

1. 先看下方贯穿案例，确认每种信号在调查中承担什么角色。
2. 阅读“背景与问题”和“决策摘要”，理解模块为什么这样拆分。
3. 需要评审 API 与实现时，再阅读扩展合同、生命周期和安全不变量。
4. 需要直接写应用代码时，转到
   [app-observe-webhertz 使用指南](../guides/app-observe-webhertz-usage.md)。

## 贯穿案例：订单接口发布后变慢

假设 `GET /orders/42` 在一次发布后偶尔从 200ms 变成 3s：

| 调查阶段 | 要回答的问题 | 使用的信号 | 本项目提供的应用侧能力 |
|---|---|---|---|
| 发现 | 是否真的变慢，影响多少请求？ | Metrics | route 级请求量、错误率和延迟分布 |
| 定界 | 哪个接口、状态和时间段异常？ | Metrics | 有限基数 route/status/outcome |
| 找样本 | 哪次请求最有代表性？ | Access Log | 可选请求完成日志、request ID、trace ID |
| 定位 | 3 秒花在当前服务还是下游？ | Trace | Hertz server Span 与上下文传播 |
| 解释 | 为什么下游调用失败？ | 业务日志/依赖 Span | 提供关联基础，具体依赖埋点由应用选择 |
| 验证 | 回滚后是否恢复？ | Metrics + Trace | 用相同指标和新请求样本复查 |

这里的关键不是“把所有数据都收集起来”，而是让调查能够从聚合异常逐步钻取到
具体请求。Access Log 是日志信号，但在本项目中的安装归属仍是 `app-web-hertz`
可选基础组件；`app-observe-webhertz` 只负责 Metrics 和 Tracing 适配。

本提案覆盖上表中的“应用产生和装配信号”。Prometheus Server、Alertmanager、
Grafana、OTel Collector、Trace 后端、日志存储和告警规则仍由应用或部署环境负责。

## 1. 背景与问题

Web 请求需要输出有限基数 Metrics 和可传播的 Server Span，但这两类能力具有不同
的运行语义：

1. Metrics 只需要请求开始和最终完成态，不需要模拟 Hertz 内部阶段事件。
2. Tracing 必须同时安装 Hertz native Tracer Option 与 Server Middleware。
3. Prometheus Registry、OTel Provider、Exporter 和 Metrics listener 都是有所有权的
   运行时资源，不能隐藏在 Config 或 Middleware 构造函数中；listener 在 IOC Init
   预绑定，主动 Server 只能由 Module 贡献的 Workload 在 app 监督阶段运行。
4. 业务 Server 不应直接依赖 Prometheus、OTel SDK 或具体 Exporter。
5. 实际请求 Path、Header、Cookie、Body 和错误文本不能无边界进入 Label 或 Attribute。

如果把上述能力直接塞入 `web-hertz`，会让基础 provider 依赖具体观测后端；如果由
`app-web-hertz` 创建 Registry/Provider，又会把应用装配与后端资源生命周期混为一体。

## 2. 决策摘要

本提案采用三层协作：

1. `web-hertz` 提供后端无关的请求完成事件合同，以及 Hertz 原生 Option/Middleware
   扩展入口。
2. `app-web-hertz` 从 IOC 聚合 `RequestObserver` 与 `InstrumentationBundle`，
   并把它们安装到固定阶段。
3. `app-observe-webhertz` 按需把完成事件适配为 Prometheus 指标，把 borrowed
   TracerProvider/Propagator 适配为 OTel Bundle。

应用 composition root 创建并拥有 Registry、TracerProvider 和 Exporter；Metrics
Server Resource 由 observe Module 创建并预绑定 listener，独立 Workload 只负责主动
运行和关闭。适配 bean 不拥有 borrowed 外部资源，也不在 IOC Init 启动 goroutine。

## 3. 范围与非目标

### 3.1 In Scope

1. Prometheus HTTP server 请求指标。
2. OTel HTTP server Span、传播和 Trace ID 元数据桥接。
3. private/shared Prometheus Registry 的注册所有权。
4. `PrometheusHandler` 与独立 Metrics Server 的边界。
5. TracerProvider、Exporter、adapter 与业务 Server 的关闭顺序。
6. Label/Attribute 基数、安全白名单和可验证不变量。

### 3.2 Out of Scope

1. Access Log、Request ID、Recovery、CORS 和 Gzip。
2. Controller、Filter、NoRoute、NoMethod 和路由配置。
3. Prometheus 抓取系统、OTel Collector 或远程 Exporter 的部署。
4. 创建或修改 `prometheus.DefaultRegistry`、OTel global Provider/Propagator。
5. 未显式声明拓扑时自动创建 listener、Metrics Server 或 goroutine；为 health、pprof
   等端点建立隐式通用管理端口。
6. 把完整 URL、query、Header、Cookie、Body、panic payload 或 Stack 作为默认信号。

Access Log 在模块归属和安装方式上是 `app-web-hertz` 的可选基础组件，不属于本提案
定义的观测适配能力。它在系统信号分类上仍可用于运行诊断，并可通过完成事件中的
Trace ID 与 Span 建立关联；这种关联不改变其模块归属。

## 4. 模块与依赖边界

| 模块 | 本提案中的职责 | 明确不负责 |
|---|---|---|
| `web-hertz` | 复制请求开始/完成值快照；同步 fan-out Observer；保留 Hertz 原生扩展 | Prometheus、OTel、IOC、Exporter |
| `app-web-hertz` | 聚合 Observer/Bundle；固定安装顺序；桥接 Request ID、Trace ID 和 panic 标记 | 创建 Registry、Provider、Exporter、诊断 Server |
| `app-observe-webhertz` | Prometheus Collector/Handler；OTel Bundle；Metrics Server Resource 与独立 Workload | 拥有外部 Provider；在 Module Init 启动 Server/goroutine；暴露 health/pprof |
| Application | 选择能力；创建 owned Provider/Exporter；选择 Workload 与暴露策略 | 把资源所有权下放给请求 Middleware |

依赖方向：

```text
Application
  ├─ owns Prometheus Registry
  ├─ owns OTel Provider / Exporter / Sampler
  ├─ declares app-observe-webhertz adapters / Metrics Resource
  ├─ selects app-web-hertz Workload
  ├─ selects app-observe-webhertz Workload
  └─ declares app-web-hertz Server

app-web-hertz
  ├─ borrows RequestObserver[]
  ├─ borrows InstrumentationBundle[]
  └─ builds web-hertz Server

app-observe-webhertz
  ├─ implements RequestObserver
  ├─ implements PrometheusHandler
  ├─ implements InstrumentationBundle
  └─ owns Metrics listener / Server Resource
```

`web-hertz` 不反向依赖 app 层；`app-web-hertz` 不依赖具体适配包；适配包不依赖
具体业务 Controller 或应用配置模型。

## 5. 稳定扩展合同

### 5.1 请求完成事件

`HTTPRequestObservation` 是 `web-hertz` 的通用请求完成事件，不等同于可观测性
模块。它可以被基础 Access Log、Prometheus Metrics 或其他快速本地消费者复用。

稳定字段包括：

- Request ID、Trace ID；
- 归一化 Method、模板 Route、无 query Path；
- 最终 Status、Duration、Request/Response Bytes；
- 有限 Outcome。

约束：

1. 快照在请求结束前从池化 `RequestContext` 复制完成。
2. Observer 不得保留 Header、Body 或请求池对象。
3. 未匹配路由固定为 `_unmatched`，非标准 Method 固定为 `_other`。
4. `Path`、Request ID、Trace ID 不得作为默认 Metrics Label。
5. Observer 同步执行，必须快速、并发安全；远程发送交给 SDK batch/exporter。
6. 单个 Observer panic 被隔离，不能破坏业务响应。

`RequestLifecycleObserver` 只补充请求开始事件，用于 in-flight Gauge。
`RequestObservationMetadataProvider` 只补充 Request ID、Trace ID 和已收敛 panic
标记，不能覆盖 Method、Route、Status 等稳定维度。

### 5.2 Instrumentation Bundle

OTel Server Span 同时需要 Hertz native Tracer Option 和 Server Middleware。两者
通过不暴露 OTel 类型的 `InstrumentationBundle` 原子发布：

```text
borrowed TracerProvider + Propagator
    -> Hertz native tracer Option
    -> Server Middleware
    -> InstrumentationBundle
    -> app-web-hertz validation
    -> web-hertz Server construction
```

Bundle 内每个 Option 和 Handler 在 Engine 创建前完成校验；任一元素非法时整个
Server 构造失败，不允许只安装一半。

### 5.3 固定相对顺序

本提案只冻结与观测相关的相对位置：

```text
advanced outer
-> instrumentation
-> Request ID
-> completion event
-> Recovery
-> application chain
```

完整标准链由 `app-web-hertz` 设计文档维护。这个顺序保证 Span 覆盖完整请求，
完成事件在 Recovery 返回后看到最终状态和 panic outcome，并能从有效 Span 获取
Trace ID。

## 6. Prometheus 方案

### 6.1 指标合同

默认指标：

| 指标 | 类型 | Labels |
|---|---|---|
| `gobase_http_server_requests_total` | Counter | method, route, status, outcome |
| `gobase_http_server_request_duration_seconds` | Histogram | method, route, outcome |
| `gobase_http_server_request_bytes` | Histogram | method, route |
| `gobase_http_server_response_bytes` | Histogram | method, route, status |
| `gobase_http_server_in_flight_requests` | Gauge | method |

Histogram 使用秒和 byte 作为单位。namespace、subsystem 和 buckets 属于统一 Config
启动快照；名称、类型和 Label 一经发布即视为 Dashboard/Alert 合同。Registry 的
private/shared 所有权仍由代码声明，避免配置隐式创建或借用资源。

### 6.2 Registry 模式

private 模式由 adapter 创建私有 Registry；shared 模式借用应用发布的
`PrometheusRegistry`。两种模式下 adapter 都拥有并注销自身 Collector
registration，但不关闭 shared Registry。

不访问 DefaultRegistry，避免测试污染、重复注册和未知 Collector 被隐式暴露。

### 6.3 Handler 边界

`PrometheusHandler` 只执行 Gather，不创建 listener 或 Server。它由 IOC 发布给
应用诊断工作负载。业务 Router 不自动增加 `/metrics`。

## 7. OpenTelemetry 方案

### 7.1 Provider 与 Propagator

应用 managed bean 创建 SDK Provider、Sampler、SpanProcessor 和 Exporter，并以
`TracerProvider` 窄接口发布。adapter 只借用：

- `OTelTracerProvider() trace.TracerProvider`；
- `SamplingPolicyReference() string`。

`propagation.TextMapPropagator` 同样由应用显式发布。adapter 不回退 OTel global。

### 7.2 Sampling Policy Reference

Sampler 在 Provider 构造时冻结。统一 Config 中的 sampling policy reference
只做启动期一致性校验，不在请求路径动态改变采样率。引用不一致时 IOC Build
fail-fast，避免配置看似生效但实际接错 Provider。

### 7.3 Span 安全模型

默认 Server Span 只记录：

- HTTP method；
- 稳定 route pattern；
- 最终 status；
- 受控 error type。

`ServerSpanAttributeEnricher` 只接收安全完成态快照；保留键和 authorization、
cookie、token、body、query 等敏感键会被过滤。未匹配路由使用固定值，不记录实际
请求 Path。

Request ID 默认与 Trace ID 独立。需要对齐时必须显式选择对齐生成器，并为无有效
Span 场景提供 fallback。

## 8. Metrics Server

Metrics 等敏感运维端点推荐使用独立端口。内建 Metrics Server 是应用显式选择、由
适配模块 Workload 管理的同级工作负载；Module Init 只预绑定 listener，不启动 Server。

稳定原则：

1. 使用私有 `http.ServeMux`，不修改全局 DefaultServeMux。
2. IOC Init 预绑定 listener，再允许任何 Workload 启动，避免端口冲突留下部分启动状态。
3. 只把配置的 Metrics path 挂载到同一原子 adapter 的 `PrometheusHandler`。
4. 默认监听 loopback；对外开放时由部署配置 NetworkPolicy、sidecar、mTLS 或鉴权。
5. 业务 Hertz 与 Metrics 使用两个独立 Workload；任一意外退出时 App 取消其他成员并等待全部关闭。
6. `NewModule()` 安装 adapter 和 Metrics Resource，显式 `Workload()` 决定主动运行；配置只提供运行策略，不决定能力安装和资源所有权。
7. 默认监听 `127.0.0.1:9099`，路径 `/metrics`；自定义 scope 缺失和非法配置均
   fail-fast。
8. health、ready 与 pprof 不属于内建 Server；需要时由应用使用公开 Handler 自定义。

### 8.1 统一配置与代码声明边界

默认 scope 是 `observability.webHertz`。Module 在 IOC Init 只读取一次完整 `Config`，
先以 `DefaultConfig()` 为基底，再覆盖 ConfigManager 中显式提供的字段并统一校验：

1. namespace、subsystem、buckets、ignoredPaths、samplingPolicyReference 和 Metrics
   Server 参数属于可部署运行策略，进入 Config；
2. private/shared Registry、是否安装 OTel、Span Formatter 与 Attribute Enricher 涉及
   资源所有权或代码身份，只能由代码声明；
3. 默认 scope 缺失使用完整默认配置，自定义 scope 缺失 fail-fast；
4. Config 不启用能力、不创建 Registry/Provider/listener，也不支持这些合同的热更新。

## 9. 所有权与生命周期

依赖关系：

```text
OTel Provider owner
       ↓ borrowed
observation adapter
       ↓ Bundle / Observer
app-web-hertz managed Server
```

IOC 逆序关闭：

1. App 并发停止业务 Hertz 与 Metrics 两个 Workload，并等待两个 Run 返回。
2. IOC 关闭 adapter，兜底停止 Metrics Resource 并注销自身 Prometheus Collector。
3. Provider owner 执行 `ForceFlush` 和 `Shutdown`，释放 Exporter。

Metrics Server 由适配模块 Workload 在返回前关闭。adapter bean 只兜底关闭 owned Resource，
不关闭 shared Registry、Provider 或 Exporter；Module Init 不创建主动 goroutine。

## 10. 错误、安全与可验证不变量

错误必须包含可检索的 `op/result/stage/reason` 并保留底层 cause。关键失败阶段：

| 阶段 | 失败语义 |
|---|---|
| adapter 声明 | nil/conflicting Option 在修改 IOC 前失败 |
| Prometheus registration | 重复注册或 Registry 非法，不发布半初始化 Handler |
| OTel dependency resolution | Provider/Propagator 缺失或采样引用不一致，IOC Build 失败 |
| Bundle validation | Option/Middleware 任一非法，业务 Server 不发布 |
| metrics listen | 启动业务工作负载前返回端口错误 |
| shutdown | 保留 Server、adapter、Provider 各阶段 cause |

必须持续验证：

1. 实际请求 Path 中的动态资源 ID 不进入 route Label。
2. 未匹配请求固定使用 `_unmatched`。
3. Observer panic 不破坏响应，原始业务 panic 仍由 Recovery 处理。
4. private/shared Registry 可并存，关闭只注销本 adapter Collector。
5. OTel Option 与 Middleware 要么全部安装、要么全部不安装。
6. Provider/Propagator 不使用 global fallback。
7. Header、Cookie、Body、query、panic payload 和凭据不进入默认信号。
8. Server 先停止，Provider 最后 Flush/Shutdown。

## 11. 实施状态与开放问题

已实现：

1. 后端无关请求完成事件与生命周期扩展。
2. `app-web-hertz` Observer/Bundle 聚合和固定安装阶段。
3. Prometheus private/shared Registry、Collector registration 和 Handler。
4. borrowed Provider/Propagator、OTel Bundle、Trace ID 桥接和安全 Enricher。
5. 统一 Config 启动快照驱动的指标/Trace 策略与独立 Metrics Server、两个 Module-contributed Workload 和高级 Quickstart。

开放问题：

1. Histogram buckets 仍需按真实流量校准。
2. 当前只覆盖 HTTP server；client/RPC instrumentation 应独立定义合同。
3. health、ready、pprof 是否进入统一管理端口需要先定义安全与生命周期合同。
4. 远程 Exporter 的重试、队列和凭据属于具体应用/SDK 配置。

## 12. 相关文档

1. [可观测性入门](../guides/observability-primer.md)
2. [app-observe-webhertz 使用指南](../guides/app-observe-webhertz-usage.md)
3. [高级可运行示例](../../examples/quickstart-app-observe-webhertz/README.md)
4. [web-hertz 架构设计](web-hertz-design.md)
5. [app-web-hertz 架构设计](app-web-hertz-design.md)
6. [app-observe-webhertz README](../../app-observe-webhertz/README.md)
