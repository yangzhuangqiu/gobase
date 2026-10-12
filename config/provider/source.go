package provider

// Source 定义一个可由 Session 加载的配置源契约。
// 厂商定位字段由具体实现私有持有，避免 config 内核泄漏 group/data、prefix/key 等概念。
type Source interface {
	// Info 返回在 Session 生命周期内保持不变的中立元信息。
	Info() SourceInfo
}

// SourceInfo 描述内核理解配置源所需的最小语义。
type SourceInfo struct {
	// Name 是日志、错误和 Explain 使用的稳定标识；同一 Session 内必填且唯一。
	Name string
	// Priority 是远端层内部的合并优先级，数值越大覆盖优先级越高，允许 0..1000。
	Priority int
	// Optional 表示加载或建立监听失败时是否允许跳过该 Source。
	Optional bool
}
