package webhertzobserve

import "go.opentelemetry.io/otel/trace"

// TracerProvider 定义 OTel Hertz 适配器借用的 Provider 语义引用。
//
// 典型调用场景:
// 1. 应用级 managed bean 创建 SDK Provider、Exporter 和 Sampler，并以本接口发布；
// 2. Web 适配器只在 Init 阶段借用 Provider，关闭责任仍属于创建它的 bean。
type TracerProvider interface {
	// OTelTracerProvider 返回已初始化且覆盖 Web Server 生命周期的 OTel Provider。
	// 返回值必须非 nil；适配器不会回退到 OTel 全局 Provider。
	OTelTracerProvider() trace.TracerProvider
	// SamplingPolicyReference 返回 Provider 实际采用的稳定采样策略引用。
	// 该值用于启动期匹配，不在请求路径动态改变 Sampler。
	SamplingPolicyReference() string
}
