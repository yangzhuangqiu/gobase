package provider

import (
	"context"
	"io"
)

// Session 定义 Provider 单次 Open 所建立的运行会话。
// 所有权：Provider.Open 返回非 nil Session 后立即移交给调用方；Close 必须幂等，并可与在途 Load 并发。
type Session interface {
	// Sources 返回本会话可加载的配置源快照；至少包含一个非 nil Source。
	Sources() []Source
	// Load 加载来自当前 Session 的指定 Source 的完整原始内容。
	// 生命周期：应尽力响应 ctx 取消；底层不支持调用中取消时，由 Provider 请求超时负责最终收敛。
	Load(ctx context.Context, source Source) ([]byte, error)
	// Close 释放连接及会话级资源。
	Close() error
}

// WatchableSession 定义 Session 可选的配置变更监听能力。
// 仅支持 pull 的 Session 无需实现本接口。
type WatchableSession interface {
	Session
	// Watch 监听指定 Source；变化时触发回调，内容仍由 Manager 通过 Load 全量重建。
	Watch(ctx context.Context, source Source, onChange func()) (io.Closer, error)
}
