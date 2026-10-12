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
	// 必填：条件必填（当通过 SDKOptions 路径连接且配置了 Sources 时）。
	// 默认值：无。
	Address string
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
	// TimeoutMs 是 SDK HTTP 超时时间（毫秒）。
	// 必填：否。
	// 默认值：10000。
	TimeoutMs uint64
	// Scheme 是请求协议（http/https）。
	// 必填：否。
	// 默认值：http。
	Scheme string
	// ContextPath 是 Nacos HTTP 上下文路径。
	// 必填：否。
	// 默认值：/nacos。
	ContextPath string
	// LogDir 是 Nacos SDK 日志目录。
	// 必填：否。
	// 默认值：%TEMP%/nacos/log。
	LogDir string
	// CacheDir 是 Nacos SDK 缓存目录。
	// 必填：否。
	// 默认值：%TEMP%/nacos/cache。
	CacheDir string
	// LogLevel 是 Nacos SDK 日志级别。
	// 必填：否。
	// 默认值：warn。
	LogLevel string
	// Sources 声明远端 Nacos 配置源集合。
	// 必填：否。
	// 默认值：空（不启用 Nacos 远端加载）。
	Sources []BootstrapSource
}
