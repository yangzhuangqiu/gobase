// Package webhertzobserve 把 Hertz 请求适配到根日志、Prometheus 和 OTel HTTP server Span。
//
// 设计边界:
// 1. 不读取 config.Manager，不定义固定 scope；
// 2. 不创建 Logger/OTel Provider、Prometheus DefaultRegistry、HTTP Server 或 goroutine；
// 3. 私有 Registry 由显式 Runtime owned，共享 Registry 始终 borrowed；
// 4. OTel Provider/Propagator 始终 borrowed，不访问 OTel global；
// 5. 不依赖或修改 Hertz 全局 hlog。
package webhertzobserve
