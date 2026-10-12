package webhertzobserve

import "net/http"

// PrometheusHandler 定义诊断工作负载可按名称注入的 Metrics HTTP Handler 合同。
//
// 说明：Handler 不拥有 listener 或 HTTP Server，调用方必须把它注册到显式诊断 Router。
type PrometheusHandler interface {
	http.Handler
}
