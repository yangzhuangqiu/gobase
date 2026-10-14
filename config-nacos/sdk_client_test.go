package nacos

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestNacosSDKClientGetConfig 验证 GetConfig 的成功、取消与错误透传语义。
func TestNacosSDKClientGetConfig(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		fakeSDKClient := &fakeNacosConfigClient{getResult: "service:\n  name: demo\n"}
		client := &nacosSDKClient{client: fakeSDKClient}

		content, err := client.GetConfig(context.Background(), "g", "d")
		if err != nil {
			t.Fatalf("get config failed: %v", err)
		}
		if string(content) != "service:\n  name: demo\n" {
			t.Fatalf("unexpected content: %s", string(content))
		}
		_, _, getParam, _, _ := fakeSDKClient.snapshot()
		if getParam.Group != "g" || getParam.DataId != "d" {
			t.Fatalf("unexpected get param: %#v", getParam)
		}
	})

	t.Run("context_canceled", func(t *testing.T) {
		fakeSDKClient := &fakeNacosConfigClient{}
		client := &nacosSDKClient{client: fakeSDKClient}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := client.GetConfig(ctx, "g", "d")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got: %v", err)
		}
		_, _, getParam, _, _ := fakeSDKClient.snapshot()
		if getParam.Group != "" || getParam.DataId != "" {
			t.Fatalf("expected sdk get not called, got param: %#v", getParam)
		}
	})

	t.Run("upstream_error", func(t *testing.T) {
		upstreamErr := errors.New("get_failed")
		fakeSDKClient := &fakeNacosConfigClient{getErr: upstreamErr}
		client := &nacosSDKClient{client: fakeSDKClient}

		_, err := client.GetConfig(context.Background(), "g", "d")
		if !errors.Is(err, upstreamErr) {
			t.Fatalf("expected upstream error, got: %v", err)
		}
	})
}

// TestNacosSDKClientWatchConfigRejectsNilCallback 验证 nil 回调会 fail-fast。
func TestNacosSDKClientWatchConfigRejectsNilCallback(t *testing.T) {
	client := &nacosSDKClient{client: &fakeNacosConfigClient{}}
	_, err := client.WatchConfig(context.Background(), "g", "d", nil)
	assertErrorContains(t, err, "onChange callback is required")
}

// TestNacosSDKClientCloseDelegatesToV2Client 验证适配层会释放 v2 SDK 的 gRPC 客户端。
// 测试方法：关闭适配层并断言 CloseClient 被调用一次且返回 nil。
func TestNacosSDKClientCloseDelegatesToV2Client(t *testing.T) {
	fakeSDKClient := &fakeNacosConfigClient{}
	client := &nacosSDKClient{client: fakeSDKClient}
	if err := client.Close(); err != nil {
		t.Fatalf("close nacos sdk client failed: %v", err)
	}
	if got := fakeSDKClient.closeSnapshot(); got != 1 {
		t.Fatalf("expected one CloseClient call, got: %d", got)
	}
}

// TestNacosSDKClientWatchConfigPropagatesListenError 验证监听注册错误会透传。
func TestNacosSDKClientWatchConfigPropagatesListenError(t *testing.T) {
	listenErr := errors.New("listen_failed")
	fakeSDKClient := &fakeNacosConfigClient{listenErr: listenErr}
	client := &nacosSDKClient{client: fakeSDKClient}

	_, err := client.WatchConfig(context.Background(), "g", "d", func(content []byte) {})
	if !errors.Is(err, listenErr) {
		t.Fatalf("expected listen error, got: %v", err)
	}
}

// TestNacosSDKClientWatchConfigDeliversChangeAndCloseIdempotent 验证回调桥接与 Close 幂等。
func TestNacosSDKClientWatchConfigDeliversChangeAndCloseIdempotent(t *testing.T) {
	fakeSDKClient := &fakeNacosConfigClient{}
	client := &nacosSDKClient{client: fakeSDKClient}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	changed := make(chan string, 1)
	closer, err := client.WatchConfig(ctx, "g", "d", func(content []byte) {
		changed <- string(content)
	})
	if err != nil {
		t.Fatalf("watch config failed: %v", err)
	}

	fakeSDKClient.emitChange("new_value")
	select {
	case got := <-changed:
		if got != "new_value" {
			t.Fatalf("unexpected callback value: %s", got)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("expected onChange callback")
	}

	if err = closer.Close(); err != nil {
		t.Fatalf("first close failed: %v", err)
	}
	if err = closer.Close(); err != nil {
		t.Fatalf("second close should be idempotent, got: %v", err)
	}

	listenCalls, cancelCalls, _, listenParam, cancelParam := fakeSDKClient.snapshot()
	if listenCalls != 1 {
		t.Fatalf("expected one listen call, got: %d", listenCalls)
	}
	if listenParam.Group != "g" || listenParam.DataId != "d" {
		t.Fatalf("unexpected listen param: %#v", listenParam)
	}
	if cancelCalls != 1 {
		t.Fatalf("expected one cancel call after close, got: %d", cancelCalls)
	}
	if cancelParam.Group != "g" || cancelParam.DataId != "d" {
		t.Fatalf("unexpected cancel param: %#v", cancelParam)
	}
}

// TestNacosSDKClientWatchConfigAutoCloseOnContextDone 验证 ctx 取消后会自动取消监听。
func TestNacosSDKClientWatchConfigAutoCloseOnContextDone(t *testing.T) {
	fakeSDKClient := &fakeNacosConfigClient{}
	client := &nacosSDKClient{client: fakeSDKClient}
	ctx, cancel := context.WithCancel(context.Background())

	closer, err := client.WatchConfig(ctx, "g", "d", func(content []byte) {})
	if err != nil {
		t.Fatalf("watch config failed: %v", err)
	}
	cancel()
	defer func() { _ = closer.Close() }()

	ok := waitForCondition(1*time.Second, func() bool {
		_, cancelCalls, _, _, _ := fakeSDKClient.snapshot()
		return cancelCalls == 1
	})
	if !ok {
		_, cancelCalls, _, _, _ := fakeSDKClient.snapshot()
		t.Fatalf("expected auto cancel call after context done, got: %d", cancelCalls)
	}
}

// TestParseServerConfigsSkipsEmptyFragments 验证多地址解析会跳过空片段。
func TestParseServerConfigsSkipsEmptyFragments(t *testing.T) {
	configs, err := parseServerConfigs(ClientOptions{
		Address: "127.0.0.1:8848, , [::1]:18848,,",
	})
	if err != nil {
		t.Fatalf("parse server configs failed: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("expected 2 parsed addresses, got: %d", len(configs))
	}
}

// TestParseServerConfigsRejectsNoValidAddress 验证无有效地址时返回 fail-fast 错误。
func TestParseServerConfigsRejectsNoValidAddress(t *testing.T) {
	_, err := parseServerConfigs(ClientOptions{Address: " , , "})
	assertErrorContains(t, err, "no valid nacos server address")
}

// TestNewClientRejectsInvalidLogLevel 验证程序化 ClientOptions 无法绕过日志级别校验。
// 测试方法：传入有效地址和非法级别，断言在创建真实 SDK Client 前返回参数错误。
func TestNewClientRejectsInvalidLogLevel(t *testing.T) {
	_, err := NewClient(ClientOptions{Address: "127.0.0.1:8848", LogLevel: "verbose"})
	assertErrorCode(t, err, ErrCodeNacosSDKInvalidInput)
	assertErrorContains(t, err, "invalid nacos log level")
}
