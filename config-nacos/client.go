package nacos

import (
	"context"
	"io"
)

// Client 定义 Nacos Session 使用的最小厂商连接能力。
//
// 典型调用场景：SDK 适配器或测试替身实现该接口，由 Nacos Provider 封装为中立 Session。
type Client interface {
	// GetConfig 拉取指定 groupId/dataId 的完整原始配置。
	GetConfig(ctx context.Context, groupID, dataID string) ([]byte, error)
	// WatchConfig 监听指定 groupId/dataId，内容变化时回调最新原始值。
	WatchConfig(ctx context.Context, groupID, dataID string, onChange func(content []byte)) (io.Closer, error)
	// Close 释放 Client 级资源；底层 SDK 不支持整体关闭时应提供明确的幂等 no-op。
	Close() error
}
