// Package webhertzobserve 把 Hertz 请求适配到根日志、Prometheus 和 OTel HTTP server Span。
//
// 设计边界:
// 1. Metrics/Tracing adapter 默认不读取配置，也不启动 listener 或 goroutine；
// 2. 显式 WithMetricsServer 时只读取独立 scope，由 Workload 创建和监督 Metrics HTTP Server；
// 3. 不创建 Logger/OTel Provider、Prometheus DefaultRegistry 或 Exporter；
// 4. 私有 Registry 由显式 Runtime owned，共享 Registry 始终 borrowed；
// 5. OTel Provider/Propagator 始终 borrowed，不访问 OTel global；
// 6. 不依赖或修改 Hertz 全局 hlog。
package webhertzobserve
