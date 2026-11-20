# quickstart-app-observe-webhertz

这是一个独立的高级可运行示例，演示如何把 `app-web-hertz` 与
`app-observe-webhertz` 组合成同时具备 Access Log、Request ID、Prometheus
指标和 OpenTelemetry Server Span 的应用。

示例刻意保留两类 HTTP 工作负载：

| 工作负载 | 默认地址 | 职责 |
|---|---|---|
| 业务 Hertz Server | `127.0.0.1:18082` | Controller、Recovery、Request ID、Access Log、Metrics Observer、OTel Instrumentation |
| Metrics HTTP Server | `127.0.0.1:19082` | 只暴露 `/metrics` |

Metrics 端口与业务端口分离，便于生产环境独立配置网络访问控制；示例没有把
Prometheus Handler 隐式安装到业务路由。

## 运行

在仓库根目录执行：

```powershell
& ./scripts/go.ps1 -C examples run ./quickstart-app-observe-webhertz
```

另开终端验证业务请求：

```powershell
curl.exe http://127.0.0.1:18082/api/hello
curl.exe http://127.0.0.1:18082/api/orders/42
curl.exe http://127.0.0.1:18082/api/panic
curl.exe http://127.0.0.1:18082/healthz
```

验证独立 Metrics 端口：

```powershell
curl.exe http://127.0.0.1:19082/metrics
```

请求 `/api/orders/42` 后，Metrics 中应出现稳定模板 Label：

```text
quickstart_http_server_requests_total{method="GET",outcome="success",route="/api/orders/:id",status="200"} 1
```

业务响应包含 `requestId` 与 `traceId`。控制台还会输出 Access Log 和精简教学
Span，例如：

```text
trace span="GET /api/orders/:id" trace_id=... span_id=... status=Unset
```

按 `Ctrl+C` 后，App 会并发关闭业务 Hertz 与 Metrics 两个 Workload，并等待两个 Run
真实收敛；随后 IOC 按依赖逆序关闭观测 adapter，最后由应用拥有的 TracerProvider 执行
`ForceFlush`/`Shutdown`。

## 关键装配语义

1. `traceProviderOwner` 是 managed bean，拥有 SDK Provider、Sampler 和 Exporter。
2. 默认配置同时提供 `log.zap`、`web.hertz` 与统一 `observability.webHertz` scope，
   保证日志、业务端口、指标/Trace 策略和 Metrics 端口在 IOC Build 阶段冻结。
3. `app-observe-webhertz` 只借用 Provider/Propagator，原子发布
   `InstrumentationBundle`，同时用私有 Registry 发布 `RequestObserver` 与
   `PrometheusHandler`。
4. `app-web-hertz` 从 IOC 聚合 Bundle 和 Observer，一次性组装唯一业务 Server。
5. observe Module 在 IOC Build 预绑定 Metrics 端口；两个 Module 分别贡献并运行
   唯一业务 Server 和独立 Metrics Server Workload。
6. `/healthz` 被 Access Log 和 Trace 忽略；Metrics 是否忽略应由 Collector 策略
   单独决定，本示例仍统计它。

控制台 Exporter 和 `AlwaysSample` 只用于本地教学。生产环境应由应用创建批处理
Exporter、合理 Sampler，并保持相同的 owned managed bean 生命周期。

## 地址覆盖

| 环境变量 | 默认值 |
|---|---|
| `APP_OBSERVE_WEBHERTZ_ADDRESS` | `127.0.0.1:18082` |
| `APP_OBSERVE_WEBHERTZ_METRICS_ADDRESS` | `127.0.0.1:19082` |

## File Map

1. `main.go`：Composition Root、配置、IOC Definition 与 Module 组合。
2. `controller.go`：业务、动态路由、panic 与健康检查入口。
3. `tracing.go`：应用 owned TracerProvider 和教学 Console Exporter。
4. `main_test.go`：完整 IOC 图、Bundle、Metrics Server 配置和 Prometheus 合同验证。

完整合同与生产化注意事项见
[app-observe-webhertz 使用指南](../../docs/guides/app-observe-webhertz-usage.md)。
