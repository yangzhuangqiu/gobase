package webhertzobserve

// ServerSpanSnapshot 是 OTel Server Span 命名和扩展使用的安全值快照。
type ServerSpanSnapshot struct {
	// Method 是标准化前的 HTTP method token；来源为当前请求方法。
	Method string
	// RoutePattern 是注册路由模板；未匹配时固定为 `_unmatched`，不回退原始路径。
	RoutePattern string
	// Status 是最终 HTTP 状态码；Span 开始阶段为零，完成阶段为实际值。
	Status int
	// Panicked 表示 panic 穿透当前插桩 Middleware；内层 Recovery 已处理时由最终 5xx 状态表达失败。
	Panicked bool
}

// ServerSpanNameFormatter 根据安全快照生成 Server Span 名称。
// 返回空白值时适配器回退到 `METHOD route`，禁止使用原始 URL、query 或 Header。
type ServerSpanNameFormatter func(snapshot ServerSpanSnapshot) string
