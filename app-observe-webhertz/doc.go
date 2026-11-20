// Package webhertzobserve 把 Hertz 请求适配到根日志、Prometheus 和 OTel HTTP server Span。
//
// 设计边界:
// 1. NewModule 默认创建 private Prometheus，并在 IOC Init 读取统一 Config 快照、预绑定独立 Metrics listener；
// 2. appwebhertz.Workload 与本包 Workload 分别运行业务 Hertz 和 Metrics HTTP Server；
// 3. Module Init 不启动 Server 或 goroutine，主动运行只发生在显式 Workload；
// 4. 不创建 Logger/OTel Provider、Prometheus DefaultRegistry 或 Exporter；
// 5. private Registry owned、shared Registry borrowed，OTel Provider/Propagator 始终 borrowed；
// 6. 不依赖或修改 Hertz 全局 hlog。
//
// 配置边界:
// 1. namespace、subsystem、buckets、ignoredPaths、samplingPolicyReference 和 Metrics Server 参数进入 ConfigManager；
// 2. Registry 所有权、OTel 安装、Formatter 与 Enricher 保持代码声明；
// 3. 默认 scope 缺失使用 DefaultConfig，自定义 scope 缺失 fail-fast。
package webhertzobserve
