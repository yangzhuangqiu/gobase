package consul

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// kvReply 描述测试 Consul KV 接口的单次响应脚本。
type kvReply struct {
	status int
	index  uint64
	value  string
}

// scriptedKVServer 返回一个按脚本顺序响应的 KV 服务桩。
func scriptedKVServer(t *testing.T, expectedPath string, replies []kvReply) *httptest.Server {
	t.Helper()
	if len(replies) == 0 {
		t.Fatalf("replies must not be empty")
	}

	var (
		mu    sync.Mutex
		calls int
	)
	handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if request.URL.Path != "/v1/kv/"+expectedPath {
			t.Errorf("unexpected request path: %s", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
			return
		}

		mu.Lock()
		index := calls
		if index >= len(replies) {
			index = len(replies) - 1
		}
		reply := replies[index]
		calls++
		mu.Unlock()

		if reply.status == http.StatusNotFound {
			writer.WriteHeader(http.StatusNotFound)
			return
		}

		writer.Header().Set("X-Consul-Index", fmt.Sprintf("%d", reply.index))
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(reply.status)
		encoded := base64.StdEncoding.EncodeToString([]byte(reply.value))
		_, _ = writer.Write([]byte(fmt.Sprintf(`[{"Key":"%s","Value":"%s"}]`, expectedPath, encoded)))
	})
	return httptest.NewServer(handler)
}

// serverAddress 从 httptest URL 提取 host:port。
func serverAddress(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse server url failed: %v", err)
	}
	return parsed.Host
}

// TestConsulAPIClientGetConfig 验证 GetConfig 的成功、缺失与取消语义。
func TestConsulAPIClientGetConfig(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		server := scriptedKVServer(t, "config/app.yaml", []kvReply{
			{status: http.StatusOK, index: 1, value: "v1"},
		})
		defer server.Close()

		client, err := NewClient(ClientOptions{Address: serverAddress(t, server.URL)})
		if err != nil {
			t.Fatalf("new consul client failed: %v", err)
		}

		content, err := client.GetConfig(context.Background(), "config", "app.yaml")
		if err != nil {
			t.Fatalf("get config failed: %v", err)
		}
		if string(content) != "v1" {
			t.Fatalf("unexpected content: %s", string(content))
		}
	})

	t.Run("key_not_found", func(t *testing.T) {
		server := scriptedKVServer(t, "config/app.yaml", []kvReply{
			{status: http.StatusNotFound},
		})
		defer server.Close()

		client, err := NewClient(ClientOptions{Address: serverAddress(t, server.URL)})
		if err != nil {
			t.Fatalf("new consul client failed: %v", err)
		}

		_, err = client.GetConfig(context.Background(), "config", "app.yaml")
		assertErrorContains(t, err, "consul key not found path=config/app.yaml")
	})

	t.Run("context_canceled", func(t *testing.T) {
		inner := &consulAPIClient{}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := inner.GetConfig(ctx, "config", "app.yaml")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context canceled, got: %v", err)
		}
	})
}

// TestConsulAPIClientWatchConfig 验证 WatchConfig 的关键语义。
func TestConsulAPIClientWatchConfig(t *testing.T) {
	t.Run("nil_callback", func(t *testing.T) {
		inner := &consulAPIClient{}
		_, err := inner.WatchConfig(context.Background(), "config", "app.yaml", nil)
		assertErrorContains(t, err, "onChange callback is required")
	})

	t.Run("invalid_path", func(t *testing.T) {
		inner := &consulAPIClient{}
		_, err := inner.WatchConfig(context.Background(), "", "app.yaml", func(content []byte) {})
		assertErrorContains(t, err, "consul source requires non-empty groupID and dataID")
	})

	t.Run("watch_change_and_close", func(t *testing.T) {
		server := scriptedKVServer(t, "config/app.yaml", []kvReply{
			{status: http.StatusOK, index: 1, value: "v1"},
			{status: http.StatusOK, index: 2, value: "v2"},
			{status: http.StatusOK, index: 2, value: "v2"},
		})
		defer server.Close()

		client, err := NewClient(ClientOptions{
			Address:    serverAddress(t, server.URL),
			WaitTimeMs: 10,
		})
		if err != nil {
			t.Fatalf("new consul client failed: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		changed := make(chan string, 1)
		closer, err := client.WatchConfig(ctx, "config", "app.yaml", func(content []byte) {
			changed <- string(content)
		})
		if err != nil {
			t.Fatalf("watch config failed: %v", err)
		}
		defer func() { _ = closer.Close() }()

		select {
		case got := <-changed:
			if strings.TrimSpace(got) != "v2" {
				t.Fatalf("expected first callback with changed value v2, got: %s", got)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("expected changed callback")
		}

		if err = closer.Close(); err != nil {
			t.Fatalf("first close failed: %v", err)
		}
		if err = closer.Close(); err != nil {
			t.Fatalf("second close should be idempotent, got: %v", err)
		}
	})
}
