package consul

import (
	"testing"
	"time"
)

// TestNormalizeConsulAddress 验证地址、协议与路径归一化语义。
func TestNormalizeConsulAddress(t *testing.T) {
	address, scheme, path, err := normalizeConsulAddress("http://127.0.0.1:8500/api", "")
	if err != nil {
		t.Fatalf("normalize address failed: %v", err)
	}
	if address != "127.0.0.1:8500" || scheme != "http" || path != "/api" {
		t.Fatalf("unexpected normalized result address=%s scheme=%s path=%s", address, scheme, path)
	}

	address, scheme, path, err = normalizeConsulAddress("127.0.0.1:8500", "https")
	if err != nil {
		t.Fatalf("normalize host:port failed: %v", err)
	}
	if address != "127.0.0.1:8500" || scheme != "https" || path != "" {
		t.Fatalf("unexpected host:port result address=%s scheme=%s path=%s", address, scheme, path)
	}
}

// TestNormalizeConsulAddressFailsOnEmpty 验证空地址会被 fail-fast 拒绝。
func TestNormalizeConsulAddressFailsOnEmpty(t *testing.T) {
	_, _, _, err := normalizeConsulAddress("", "")
	if err == nil {
		t.Fatalf("expected empty address error")
	}
	assertErrorContains(t, err, "consul address is required")
}

// TestBuildConsulKVPath 验证 group/data 到 KV 路径的映射。
func TestBuildConsulKVPath(t *testing.T) {
	got, err := buildConsulKVPath("/config/", "/app.yaml/")
	if err != nil {
		t.Fatalf("build path failed: %v", err)
	}
	if got != "config/app.yaml" {
		t.Fatalf("unexpected path: %s", got)
	}
}

// TestChooseConsulWaitTime 验证阻塞查询等待时长的默认值与覆盖语义。
func TestChooseConsulWaitTime(t *testing.T) {
	if got := chooseConsulWaitTime(0); got != DefaultWatchWaitTime {
		t.Fatalf("expected default wait time %s, got: %s", DefaultWatchWaitTime, got)
	}
	if got := chooseConsulWaitTime(1500); got != 1500*time.Millisecond {
		t.Fatalf("expected wait time 1500ms, got: %s", got)
	}
}

// TestNewClientRequiresAddress 验证缺少地址时建连会 fail-fast。
func TestNewClientRequiresAddress(t *testing.T) {
	_, err := NewClient(ClientOptions{})
	if err == nil {
		t.Fatalf("expected empty address error")
	}
	assertErrorContains(t, err, "consul address is required")
}
