package webhertzobserve

import "github.com/prometheus/client_golang/prometheus"

// PrometheusRegistry 定义共享 Registry 必须同时满足的注册与采集合同。
//
// 典型调用场景：应用拥有自定义 Registry，并以 borrowed bean 方式交给适配 Module 注册 HTTP 指标和构造 Handler。
type PrometheusRegistry interface {
	prometheus.Registerer
	prometheus.Gatherer
}
