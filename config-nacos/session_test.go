package nacos

import (
	"context"
	"io"
	"testing"
)

// closableNacosClient 记录 Session 是否关闭底层 Client。
type closableNacosClient struct{ closeCalls int }

// GetConfig 返回空配置。
func (this *closableNacosClient) GetConfig(context.Context, string, string) ([]byte, error) {
	return nil, nil
}

// WatchConfig 返回空 watcher。
func (this *closableNacosClient) WatchConfig(context.Context, string, string, func([]byte)) (io.Closer, error) {
	return nil, nil
}

// Close 记录底层 Client 释放次数。
func (this *closableNacosClient) Close() error { this.closeCalls++; return nil }

// TestSessionCloseHonorsClientOwnership 验证 Session 只关闭 Provider 自建 Client。
// 测试方法：分别创建 owned/borrowed Session 并重复 Close，断言 owned 只关闭一次、borrowed 从不关闭。
func TestSessionCloseHonorsClientOwnership(t *testing.T) {
	ownedClient := &closableNacosClient{}
	owned := newSession(ownedClient, []BootstrapSource{{GroupID: "g", DataID: "d"}}, true)
	if err := owned.Close(); err != nil {
		t.Fatalf("close owned session failed: %v", err)
	}
	if err := owned.Close(); err != nil {
		t.Fatalf("close owned session twice failed: %v", err)
	}
	if ownedClient.closeCalls != 1 {
		t.Fatalf("expected owned client closed once, got=%d", ownedClient.closeCalls)
	}

	borrowedClient := &closableNacosClient{}
	borrowed := newSession(borrowedClient, []BootstrapSource{{GroupID: "g", DataID: "d"}}, false)
	if err := borrowed.Close(); err != nil {
		t.Fatalf("close borrowed session failed: %v", err)
	}
	if borrowedClient.closeCalls != 0 {
		t.Fatalf("expected borrowed client untouched, got=%d", borrowedClient.closeCalls)
	}
}
