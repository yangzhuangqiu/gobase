package nacos

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/model"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"
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

// fakeNacosConfigClient 用于 nacosSDKClient 语义测试。
type fakeNacosConfigClient struct {
	mu sync.Mutex

	getParam    vo.ConfigParam
	getResult   string
	getErr      error
	listenParam vo.ConfigParam
	listenErr   error
	cancelParam vo.ConfigParam
	cancelErr   error

	listenCalls int
	cancelCalls int
	closeCalls  int
}

// GetConfig 返回预设读取结果。
func (this *fakeNacosConfigClient) GetConfig(param vo.ConfigParam) (string, error) {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.getParam = param
	return this.getResult, this.getErr
}

// PublishConfig 为接口占位实现。
func (this *fakeNacosConfigClient) PublishConfig(param vo.ConfigParam) (bool, error) {
	return false, nil
}

// DeleteConfig 为接口占位实现。
func (this *fakeNacosConfigClient) DeleteConfig(param vo.ConfigParam) (bool, error) {
	return false, nil
}

// ListenConfig 保存监听参数并返回预设错误。
func (this *fakeNacosConfigClient) ListenConfig(param vo.ConfigParam) (err error) {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.listenParam = param
	this.listenCalls++
	return this.listenErr
}

// CancelListenConfig 记录取消调用并返回预设错误。
func (this *fakeNacosConfigClient) CancelListenConfig(param vo.ConfigParam) (err error) {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.cancelParam = param
	this.cancelCalls++
	return this.cancelErr
}

// SearchConfig 为接口占位实现。
func (this *fakeNacosConfigClient) SearchConfig(param vo.SearchConfigParam) (*model.ConfigPage, error) {
	return nil, nil
}

// CloseClient 记录 v2 SDK 客户端关闭调用。
func (this *fakeNacosConfigClient) CloseClient() {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.closeCalls++
}

// emitChange 主动触发监听回调，模拟 SDK 推送。
func (this *fakeNacosConfigClient) emitChange(data string) {
	this.mu.Lock()
	callback := this.listenParam.OnChange
	this.mu.Unlock()
	if callback != nil {
		callback("", "", "", data)
	}
}

// snapshot 返回当前调用统计快照。
func (this *fakeNacosConfigClient) snapshot() (listenCalls int, cancelCalls int, getParam vo.ConfigParam, listenParam vo.ConfigParam, cancelParam vo.ConfigParam) {
	this.mu.Lock()
	defer this.mu.Unlock()
	return this.listenCalls, this.cancelCalls, this.getParam, this.listenParam, this.cancelParam
}

// closeSnapshot 返回 SDK client 关闭调用次数。
func (this *fakeNacosConfigClient) closeSnapshot() int {
	this.mu.Lock()
	defer this.mu.Unlock()
	return this.closeCalls
}

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

// waitForCondition 在截止前轮询条件成立。
func waitForCondition(timeout time.Duration, check func() bool) bool {
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
