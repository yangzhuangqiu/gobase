package consul

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// fakeProviderClient 用于 provider 组装路径测试。
type fakeProviderClient struct{}

// GetConfig 返回空内容。
func (this *fakeProviderClient) GetConfig(ctx context.Context, groupID, dataID string) ([]byte, error) {
	return nil, nil
}

// WatchConfig 返回空 watcher。
func (this *fakeProviderClient) WatchConfig(ctx context.Context, groupID, dataID string, onChange func(content []byte)) (io.Closer, error) {
	return nil, nil
}

// Close 结束测试 Client；没有资源需要释放。
func (this *fakeProviderClient) Close() error { return nil }

// assertErrorContains 断言错误文本包含片段。
func assertErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error to contain %q, got nil", want)
	}
	if !containsToken(err.Error(), want) {
		t.Fatalf("expected error to contain %q, got: %v", want, err)
	}
}

// assertErrorToken 断言错误中包含 key=value 标记。
func assertErrorToken(t *testing.T, err error, key, value string) {
	t.Helper()
	assertErrorContains(t, err, key+"="+value)
}

// assertErrorCode 断言错误包含预期错误码。
func assertErrorCode(t *testing.T, err error, code ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error code %s, got nil", code)
	}
	if !errcode.IsCode[ErrorCode](err, code) {
		t.Fatalf("expected error code %s, got: %v", code, err)
	}
}

// waitUntil 在截止前轮询条件成立。
func waitUntil(timeout time.Duration, check func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if check() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// containsToken 在 message 中查找 want 片段。
func containsToken(message, want string) bool {
	return len(want) == 0 || (len(message) >= len(want) && indexOf(message, want) >= 0)
}

// indexOf 返回子串首次出现位置。
func indexOf(message, want string) int {
	for i := 0; i+len(want) <= len(message); i++ {
		if message[i:i+len(want)] == want {
			return i
		}
	}
	return -1
}
