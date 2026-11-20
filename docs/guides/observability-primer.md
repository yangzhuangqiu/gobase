# 可观测性入门：从“服务出问题了”到定位根因

本文面向第一次系统接触可观测性的开发者。它先回答“为什么需要、实际怎样使用、
业界通常如何组合”，再说明 gobase 当前覆盖的边界。

如果已经理解 Metrics、Logs、Traces、Profiles 以及 Prometheus、OpenTelemetry 的
角色，可以直接阅读 [Web 可观测性提案](../architecture/web-observability-proposal.md)。

## 1. 可观测性到底解决什么问题

线上服务通常不是简单地“活着”或“挂了”。更常见的是：

- 只有一部分接口变慢；
- 只有某个租户、某种请求或某个下游依赖失败；
- 平均延迟正常，但少量用户等待数秒；
- 发布后错误率缓慢上升；
- 服务最终恢复了，却无法确认是修复生效还是流量自然下降。

可观测性的目标，是让团队能够根据系统主动输出的数据，回答系统内部正在发生什么，
尤其是事先没有为某个具体故障编写专用诊断代码时，仍然可以继续追问“为什么”。

它主要支持五类工作：

| 工作 | 要回答的问题 |
|---|---|
| 发现 | 用户是否正在受影响？是否需要告警？ |
| 定界 | 哪个服务、接口、版本、区域或依赖受影响？ |
| 定位 | 时间花在哪里？错误从哪里产生？ |
| 验证 | 发布、扩容或回滚后是否真的恢复？ |
| 规划 | 流量、容量和可靠性趋势是否即将越过边界？ |

“监控”和“可观测性”不需要被理解成互斥概念。监控通常负责持续检查已知风险并告警；
可观测性还强调面对未知问题时，能够利用已有信号做临时查询和关联分析。

## 2. 用一次慢请求事故理解完整过程

假设订单服务发布后，用户反馈 `GET /orders/42` 偶尔需要 3 秒。

### 2.1 没有可观测性时

团队通常只能：

1. 登录机器搜索文本日志；
2. 猜测是数据库、网络、锁还是新版本代码；
3. 临时增加日志并重新发布；
4. 等待问题再次出现；
5. 因为缺少量化数据，无法确认回滚是否真正解决问题。

这条路径慢，而且诊断动作本身会改变线上系统。

### 2.2 有基本可观测性时

一次典型调查可以是：

1. **Metrics 发现异常**：`/orders/:id` 的 P95 延迟从 200ms 升到 2.8s，
   错误率同时上升，并且变化从新版本发布后开始。
2. **Metrics 缩小范围**：只有订单详情接口异常，流量和 CPU 没有明显增加，
   因此先排除全局容量不足。
3. **Access Log 找到样本**：慢请求记录包含 route、status、duration、request ID
   和 trace ID。
4. **Trace 定位路径**：该 trace 显示总耗时 2.9s，其中数据库调用占 2.6s，
   其他服务调用正常。
5. **业务日志解释原因**：相同 trace ID 下出现“连接池等待超时”，并带有安全的
   数据库实例标识。
6. **修复后验证**：回滚连接池配置后，延迟分位数和错误率恢复；新 trace 中数据库
   span 回到正常范围。

这个案例里没有任何一种信号单独完成全部工作：

- Metrics 告诉你“是否异常、影响多大、从何时开始”；
- Access Log/业务日志提供具体事件和上下文；
- Trace 告诉你“单个请求经过哪里、时间花在哪里”；
- 如果问题是 CPU 热点或内存分配，最后还需要 Profile 定位到函数。

## 3. 四类常见信号

### 3.1 Metrics：聚合后的数字趋势

Metrics 是随时间记录的数值，例如请求数、错误数、延迟分布、连接池占用率。

适合：

- 持续告警；
- 查看趋势和影响范围；
- 比较发布前后；
- 计算 SLI/SLO；
- 长期、低成本保留聚合结果。

不适合：

- 还原某一个请求的完整上下文；
- 记录订单号、用户 ID、原始 URL 等无限变化的值。

常见 Web 指标遵循 RED：

| 信号 | 含义 | 示例 |
|---|---|---|
| Rate | 请求速率 | 每秒请求数 |
| Errors | 失败情况 | 5xx 比例、业务失败比例 |
| Duration | 延迟分布 | P50、P95、P99 |

Google SRE 常用的“四个黄金信号”还包括 Saturation，用于判断 CPU、线程池、
连接池、队列等资源是否接近容量边界。

### 3.2 Logs：离散事件及其上下文

Logs 是带时间戳的事件记录。它既包括业务日志、错误日志，也包括每个 HTTP 请求
完成时产生的 Access Log。

适合：

- 解释具体失败原因；
- 保留审计或状态变化记录；
- 通过 request ID、trace ID 关联同一次请求；
- 搜索已知错误码和结构化字段。

不适合：

- 依靠扫描海量日志替代聚合告警；
- 默认记录 token、Cookie、请求体或完整凭据；
- 把任意文本当成稳定查询合同。

Access Log 是日志信号的一种，但“属于哪类信号”和“由哪个模块安装”是两个问题。
在 gobase 中，它由 `app-web-hertz` 作为可选 Web 基础组件安装；它可以带 trace ID
参与关联，却不因此成为 `app-observe-webhertz` 的职责。

### 3.3 Traces：单个请求的端到端路径

Trace 描述一个请求跨越多个组件或服务的完整路径。每个步骤称为 Span，通常包含
开始时间、持续时间、父子关系、状态和受控属性。

适合：

- 定位跨服务延迟；
- 找出错误最初出现在哪个依赖；
- 理解重试、扇出和异步调用；
- 关联同一请求的日志。

代价与限制：

- 数据量通常高于 Metrics，需要采样和保留策略；
- 只有服务端 Span 时，只能看到请求进入当前服务这一段；
- 要看到数据库、HTTP client、RPC 和消息队列，需要分别安装对应 instrumentation；
- Trace 后端和 Collector 不等于应用内 SDK。

### 3.4 Profiles：代码级资源消耗

Profile 按函数或调用栈聚合 CPU、内存分配、锁等待等信息，常以火焰图展示。

适合：

- CPU 飙高但请求链路看不出原因；
- 内存分配、GC 或锁竞争问题；
- 找出需要优化的具体函数。

Profile 不是当前 Web 可观测性适配提案的核心范围。诊断端口可以按应用安全策略
暴露 pprof，但采集、存储和持续 Profiling 需要独立方案。

## 4. 信号如何协作

实际调查通常按“从便宜的聚合信号逐步钻取到昂贵的详细信号”进行：

```text
告警 / Dashboard
       │
       ▼
Metrics：哪个服务、接口、时间段异常？
       │ route / status / 时间窗口
       ▼
Logs：有哪些具体错误或慢请求？
       │ request_id / trace_id
       ▼
Trace：请求在哪个步骤耗时或失败？
       │ service / operation / host
       ▼
Profile：哪段代码消耗了 CPU、内存或锁？
```

关联字段必须有明确用途：

- `route` 适合做低基数 Metrics 维度；
- `request_id` 适合在日志中定位单次请求；
- `trace_id` 适合在日志和 Trace 之间跳转；
- 原始 path、用户 ID、订单 ID 不应成为默认 Metrics label。

## 5. 业界方案通常由哪些部分组成

“安装一个 SDK”并不等于完成可观测性。完整链路通常有六层：

```text
业务服务
  -> Instrumentation（产生 Metrics / Logs / Traces / Profiles）
  -> SDK、Agent 或 Exporter（聚合、采样、批量发送）
  -> Scraper / Collector（采集、处理、路由）
  -> Backend（存储和查询）
  -> Dashboard / Explore（展示和临时分析）
  -> Alerting / On-call（通知和事件响应）
```

常见开源组合：

| 信号 | 应用侧/协议 | 采集与存储 | 查询、展示与告警 |
|---|---|---|---|
| Metrics | Prometheus client、`/metrics` | Prometheus | PromQL、Grafana、Alertmanager |
| Logs | 结构化日志、stdout | Fluent Bit/OTel Collector，Loki/Elasticsearch | LogQL/KQL、Grafana/Kibana |
| Traces | OpenTelemetry SDK、OTLP | OTel Collector，Tempo/Jaeger | Grafana/Jaeger UI |
| Profiles | pprof、持续 Profiling agent | Pyroscope 等后端 | 火焰图/UI |

也可以使用云厂商或商业 APM，把采集、存储、展示和告警作为托管服务。两类路线
解决的是同一条链路问题，主要取舍是运维成本、数据控制、生态兼容性和费用。

几个容易混淆的角色：

- **OpenTelemetry** 主要提供统一的 API、SDK、语义约定和 OTLP 传输生态；
  它不是默认的长期存储或 Dashboard。
- **Prometheus client** 只让应用产生指标；Prometheus Server 才负责抓取、存储和查询。
- **Grafana** 主要负责查询和展示数据源；它不会自动让未埋点的应用产生完整信号。
- **Collector** 负责接收、处理和转发遥测数据，不替代具体的查询存储后端。

## 6. 三种常见落地规模

### 6.1 单体或少量服务

先做：

1. 结构化应用日志和 Access Log；
2. request ID；
3. HTTP RED Metrics；
4. 一个基础 Dashboard 和少量高价值告警；
5. 安全的诊断端口。

此时 Tracing 可以暂缓，除非已经存在难以复现的慢请求或外部依赖问题。

### 6.2 多服务调用链

在基础能力上增加：

1. W3C Trace Context 传播；
2. HTTP client、RPC、数据库和消息队列 instrumentation；
3. OTel Collector；
4. Trace 后端和采样策略；
5. 日志中的 trace ID；
6. Metrics、Logs、Traces 之间的跳转。

只给入口 Web Server 建一个 Span，不能形成真正的端到端调用链。

### 6.3 有明确可靠性目标的系统

继续增加：

1. 从用户视角定义 SLI/SLO；
2. 基于错误预算设计告警；
3. 发布版本、区域、依赖等受控维度；
4. Runbook、值班和事件复盘流程；
5. 容量规划与持续 Profiling。

可观测性最终服务于可靠性决策，而不是以“采集了多少数据”为成功标准。

## 7. 四个常见场景

| 场景 | 首先看 | 然后看 | 可能结论 |
|---|---|---|---|
| 某接口突然变慢 | route 延迟 Metrics | 慢请求 Trace | 数据库、下游或重试耗时 |
| 偶发 500 | 错误率和 status Metrics | Access Log、错误日志、Trace | 特定依赖或业务分支失败 |
| CPU 持续升高 | Saturation/运行时 Metrics | Profile、发布对比 | 热函数、分配或锁竞争 |
| 用户说失败但服务 2xx | 业务 SLI、业务日志 | 对应请求 Trace | 技术成功但业务语义失败 |

最后一个场景尤其重要：HTTP 200 不等于用户目标成功。关键业务流程仍需要业务指标
和业务事件，框架级 HTTP 指标不能替代它们。

## 8. gobase 当前提供什么

```text
app-web-hertz
  ├─ Request ID（可选基础组件）
  ├─ Access Log（可选基础组件）
  └─ 聚合请求完成事件与 Instrumentation Bundle

app-observe-webhertz
  ├─ 请求完成事件 -> Prometheus Web Metrics
  ├─ borrowed OTel Provider -> Hertz Server Trace
  └─ 可选配置驱动 Metrics Server 与双工作负载 Runner

Application
  ├─ 拥有 Registry、TracerProvider、Exporter
  ├─ 显式选择是否启用 Metrics Server
  └─ 决定采样、网络暴露与鉴权策略
```

当前已经解决：

- HTTP server RED Metrics 及请求/响应大小、in-flight；
- Prometheus private/shared Registry 适配；
- Hertz server Span、上下文传播和 Trace ID 桥接；
- Access Log 与 Trace ID 的关联基础；
- 配置驱动的独立 Metrics Server 与统一 Runner；
- 资源所有权和关闭顺序。

当前没有替应用解决：

- Prometheus Server、Alertmanager、Grafana 的部署；
- OTel Collector、Tempo/Jaeger 或商业后端的部署；
- 日志采集、索引和长期存储；
- HTTP client、数据库、RPC、消息队列 instrumentation；
- Dashboard、告警规则、SLO 和 Runbook；
- 持续 Profiling。

因此高级 Quickstart 证明的是“应用侧信号如何正确产生和装配”，不是一套开箱即用的
完整观测平台。

## 9. 推荐的渐进落地顺序

不要一开始同时建设所有信号和后端。建议按问题价值逐步增加：

1. **先统一日志上下文**：结构化日志、错误码、request ID、可选 Access Log。
2. **再建立服务健康视图**：RED Metrics、Dashboard、基于用户影响的告警。
3. **有跨组件定位需求时加入 Trace**：先传播上下文，再覆盖关键 client/依赖。
4. **建立信号关联**：日志携带 trace ID，Dashboard 能从异常指标跳到相关 Trace。
5. **最后完善可靠性流程**：SLI/SLO、错误预算、Runbook、发布标记和事件复盘。
6. **遇到代码级资源问题再加入 Profile**。

每个阶段都应有可验证结果。例如第一阶段不是“已经输出日志”，而是“拿到一个
request ID 能找到该请求的入口、错误和关键业务事件”；第二阶段不是“有 Dashboard”，
而是“告警后能在几分钟内指出受影响的接口与错误比例”。

## 10. 常见失败方式

1. **把动态 ID 放进 Metrics label**：时间序列数量失控，存储和查询成本爆炸。
2. **有数据但没有问题模型**：Dashboard 很多，却不能回答用户是否受影响。
3. **只采集入口 Trace**：看见服务端总耗时，却看不见下游和数据库。
4. **所有 Trace 全量永久保存**：成本迅速上升，应根据流量和诊断目标制定采样策略。
5. **日志记录敏感信息**：可观测平台扩大了凭据、个人信息的暴露面。
6. **告警等同于图表阈值**：没有持续时间、影响面和处置动作，最终产生告警疲劳。
7. **组件偷偷拥有外部资源**：Registry、Provider、Exporter、listener 无法正确关闭。

## 11. 如何判断第一阶段已经够用

面对一次 Web 故障，团队应能够回答：

- 哪个接口从什么时候开始异常？
- 请求量、错误率和延迟分别发生了什么变化？
- 影响是全局还是局部？
- 能否找到至少一个代表性请求？
- 该请求耗时或失败发生在哪个步骤？
- 日志和 Trace 是否能通过 ID 关联？
- 修复后能否用相同指标确认恢复？
- 敏感信息和高基数字段是否被控制？

如果这些问题还无法回答，优先补齐调查链路，而不是继续增加更多 Dashboard。

## 12. 下一步阅读

1. [Web 可观测性提案](../architecture/web-observability-proposal.md)：本项目为什么采用
   当前模块边界和生命周期合同。
2. [app-observe-webhertz 使用指南](app-observe-webhertz-usage.md)：如何在应用中装配 Metrics、Tracing
   和独立 Metrics Server。
3. [高级可运行示例](../../examples/quickstart-app-observe-webhertz/README.md)：完整代码。
4. [app-web-hertz 使用指南](app-web-hertz-usage.md)：Access Log、Request ID 等基础组件。

## 13. 业界参考

1. [OpenTelemetry Observability Primer](https://opentelemetry.io/docs/concepts/observability-primer/)
2. [Prometheus Overview](https://prometheus.io/docs/introduction/overview/)
3. [Google SRE Workbook：Monitoring](https://sre.google/workbook/monitoring/)
4. [Google SRE：Monitoring Distributed Systems](https://sre.google/sre-book/monitoring-distributed-systems/)
5. [Grafana：About metrics and telemetry](https://grafana.com/docs/grafana/latest/visualizations/simplified-exploration/metrics/about-metrics/)
