package consul

import (
	"strings"

	"github.com/yangzhuangqiu/gobase/config/provider"
)

// BootstrapSource 描述启动阶段的单个 Consul 远端源声明。
type BootstrapSource struct {
	// Name 是远端源显示名称（用于日志与运行时元数据）。
	// 必填：否。
	// 默认值："<prefix>/<key>"。
	Name string
	// Prefix 是 Consul KV 路径前缀。
	// 必填：是（声明该源时必须提供）。
	// 默认值：无。
	Prefix string
	// Key 是 Consul KV 的 key 名称。
	// 必填：是（声明该源时必须提供）。
	// 默认值：无。
	Key string
	// Priority 控制远端源内部合并优先级（值越大优先级越高）。
	// 必填：否。
	// 默认值：0。
	Priority int
	// Optional 控制该源是否允许降级跳过。
	// 必填：否。
	// 默认值：false（拉取/监听失败会导致启动或刷新失败）。
	Optional bool
}

// Info 返回 Config 内核使用的 Consul Source 中立元信息。
func (this BootstrapSource) Info() provider.SourceInfo {
	name := strings.TrimSpace(this.Name)
	if name == "" {
		name = strings.TrimSpace(this.Prefix) + "/" + strings.TrimSpace(this.Key)
	}
	return provider.SourceInfo{Name: name, Priority: this.Priority, Optional: this.Optional}
}

// Bootstrap 定义 Consul 启动参数结构，统一承接 file/env/cli/code 输入。
type Bootstrap struct {
	// Address 是 Consul 地址，例如 "127.0.0.1:8500"。
	// 必填：条件必填（当通过 ClientOptions 路径连接且配置了 Sources 时）。
	// 默认值：无。
	Address string
	// Scheme 是请求协议（http/https）。
	// 必填：否。
	// 默认值：http。
	Scheme string
	// Datacenter 是目标 Consul 数据中心。
	// 必填：否。
	// 默认值：空（使用 agent/server 默认值）。
	Datacenter string
	// Namespace 是目标 Consul 命名空间。
	// 必填：否。
	// 默认值：空。
	Namespace string
	// Token 是 Consul ACL 访问令牌。
	// 必填：否。
	// 默认值：空。
	Token string
	// WaitTimeMs 是阻塞查询等待时长（毫秒）。
	// 必填：否。
	// 默认值：30000。
	WaitTimeMs uint64
	// Sources 声明远端 Consul KV 源集合。
	// 必填：否。
	// 默认值：空（不启用 Consul 远端加载）。
	Sources []BootstrapSource
}
