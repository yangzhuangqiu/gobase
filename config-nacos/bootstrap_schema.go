package nacos

import (
	"strings"

	"github.com/yangzhuangqiu/gobase/config/provider"
)

// BootstrapSource 描述启动阶段的单个 Nacos 远端源声明。
type BootstrapSource struct {
	// GroupID 是 Nacos 分组标识（groupId）。
	// 必填：是（声明该源时必须提供）。
	// 默认值：无。
	GroupID string
	// DataID 是 Nacos 配置标识（dataId）。
	// 必填：是（声明该源时必须提供）。
	// 默认值：无。
	DataID string
	// Priority 控制远端源内部合并优先级（值越大优先级越高）。
	// 必填：否。
	// 默认值：0。
	Priority int
	// Optional 控制该源是否允许降级跳过。
	// 必填：否。
	// 默认值：false（拉取/监听失败会导致启动或刷新失败）。
	Optional bool
}

// Info 返回 Config 内核使用的 Nacos Source 中立元信息。
// 运行阶段使用 groupId/dataId 组合生成稳定名称，厂商定位字段仍保留在本类型内。
func (this BootstrapSource) Info() provider.SourceInfo {
	return provider.SourceInfo{
		Name:     strings.TrimSpace(this.GroupID) + "/" + strings.TrimSpace(this.DataID),
		Priority: this.Priority,
		Optional: this.Optional,
	}
}

// Bootstrap 定义 Nacos 启动参数结构，统一承接 file/env/cli/code 输入。
type Bootstrap struct {
	// Address 是 Nacos 服务地址，例如 "127.0.0.1:8848" 或逗号分隔多地址。
	// 必填：条件必填（Servers 为空且配置了 Sources 时）。
	// 默认值：无。
	Address string
	// Servers 是结构化 Nacos 服务节点；非空时整体覆盖 Address。
	// 必填：否。
	// 默认值：空（使用 Address 简写路径）。
	Servers []Server
	// NamespaceID 是 Nacos 命名空间 ID。
	// 必填：否。
	// 默认值：空（使用 SDK/服务端默认命名空间，通常为 public）。
	NamespaceID string
	// Username 是 Nacos 认证用户名。
	// 必填：否。
	// 默认值：空。
	Username string
	// Password 是 Nacos 认证密码。
	// 必填：否。
	// 默认值：空。
	Password string
	// TimeoutMs 是常规配置，控制 SDK 请求超时时间（毫秒）。
	// 必填：否。
	// 默认值：10000。
	TimeoutMs uint64
	// LogDir 是高级 SDK 配置，控制 Nacos SDK 自有日志目录。
	// 必填：否。
	// 默认值：os.TempDir()/nacos/log。
	// 说明：SDK logger 为进程级单例，首个 Client 的配置生效。
	LogDir string
	// CacheDir 是高级 SDK 配置，控制 Nacos SDK 快照缓存目录。
	// 必填：否。
	// 默认值：os.TempDir()/nacos/cache。
	// 说明：默认目录未按集群隔离，多集群场景应显式配置独立目录。
	CacheDir string
	// LogLevel 是高级 SDK 配置，控制进程级 Nacos SDK 日志级别。
	// 必填：否。
	// 默认值：warn。
	// 约束：仅允许 debug、info、warn、error；首个 Client 的配置生效。
	LogLevel string
	// Sources 声明远端 Nacos 配置源集合。
	// 必填：否。
	// 默认值：空（不启用 Nacos 远端加载）。
	Sources []BootstrapSource
}
