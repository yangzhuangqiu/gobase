package consul

import (
	"context"
	"io"
	"testing"
)

// closableConsulClient 记录 Session 是否关闭底层 Client。
type closableConsulClient struct{ closeCalls int }

// GetConfig 返回空配置。
func (this *closableConsulClient) GetConfig(context.Context, string, string) ([]byte, error) {
	return nil, nil
}

// WatchConfig 返回空 watcher。
func (this *closableConsulClient) WatchConfig(context.Context, string, string, func([]byte)) (io.Closer, error) {
	return nil, nil
}

// Close 记录底层 Client 释放次数。
func (this *closableConsulClient) Close() error { this.closeCalls++; return nil }

// TestSessionCloseHonorsClientOwnership 验证 Session 只关闭 Provider 自建 Client。
// 测试方法：分别创建 owned/borrowed Session 并重复 Close，断言 owned 只关闭一次、borrowed 从不关闭。
func TestSessionCloseHonorsClientOwnership(t *testing.T) {
	ownedClient := &closableConsulClient{}
	owned := newSession(ownedClient, []BootstrapSource{{Prefix: "p", Key: "k"}}, true)
	if err := owned.Close(); err != nil {
		t.Fatalf("close owned session failed: %v", err)
	}
	if err := owned.Close(); err != nil {
		t.Fatalf("close owned session twice failed: %v", err)
	}
	if ownedClient.closeCalls != 1 {
		t.Fatalf("expected owned client closed once, got=%d", ownedClient.closeCalls)
	}

	borrowedClient := &closableConsulClient{}
	borrowed := newSession(borrowedClient, []BootstrapSource{{Prefix: "p", Key: "k"}}, false)
	if err := borrowed.Close(); err != nil {
		t.Fatalf("close borrowed session failed: %v", err)
	}
	if borrowedClient.closeCalls != 0 {
		t.Fatalf("expected borrowed client untouched, got=%d", borrowedClient.closeCalls)
	}
}
