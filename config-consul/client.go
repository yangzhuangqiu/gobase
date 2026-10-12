package consul

import (
	"context"
	"io"
)

// Client 定义 Consul Session 使用的最小厂商连接能力。
//
// 典型调用场景：SDK 适配器或测试替身实现该接口，由 Consul Provider 封装为中立 Session。
type Client interface {
	// GetConfig 拉取指定 KV prefix/key 的完整原始配置。
	GetConfig(ctx context.Context, prefix, key string) ([]byte, error)
	// WatchConfig 监听指定 KV prefix/key，内容变化时回调最新原始值。
	WatchConfig(ctx context.Context, prefix, key string, onChange func(content []byte)) (io.Closer, error)
	// Close 释放 Client 级连接资源；必须幂等。
	Close() error
}
