package nacos

// Server 描述一个 Nacos 服务节点的独立连接参数。
//
// 典型调用场景：
// 1. 集群节点使用不同 HTTP/gRPC 端口；
// 2. 非标准部署需要 HTTPS 或自定义 context path。
type Server struct {
	// Host 是节点主机名或 IP；必填，不包含 scheme、路径和端口。
	Host string
	// Port 是节点 HTTP 端口；可选，0 表示使用默认值 8848。
	Port uint64
	// Scheme 是节点 HTTP 协议；可选，仅允许 http/https，空值表示 http。
	Scheme string
	// ContextPath 是节点 HTTP 上下文路径；可选，空值表示 /nacos，缺少前导 / 时自动补齐。
	ContextPath string
	// GrpcPort 是节点 gRPC 端口；可选，0 表示由 SDK 按 Port +1000 推导。
	GrpcPort uint64
}
