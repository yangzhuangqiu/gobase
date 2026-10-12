//go:build integration

package consul

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/yangzhuangqiu/gobase/config"
)

// integrationConsulEnv 描述真实 Consul 集成测试所需环境。
type integrationConsulEnv struct {
	addr       string
	groupID    string
	dataID     string
	scheme     string
	datacenter string
	namespace  string
	token      string
}

// TestIntegrationConsulWatchRefreshSkeleton 是真实 Consul 链路测试骨架：
// 1) 发布初始配置
// 2) Build 并确认初始值可读
// 3) 发布新配置并等待 watcher -> refresh 生效
//
// 运行前需提供环境变量（缺失时自动 Skip）：
// - CONFIG_IT_CONSUL_ADDR
// - CONFIG_IT_CONSUL_GROUP
// - CONFIG_IT_CONSUL_DATA_ID
// 可选：
// - CONFIG_IT_CONSUL_SCHEME（默认 http）
// - CONFIG_IT_CONSUL_DATACENTER
// - CONFIG_IT_CONSUL_NAMESPACE
// - CONFIG_IT_CONSUL_TOKEN
func TestIntegrationConsulWatchRefreshSkeleton(t *testing.T) {
	env, ok := loadIntegrationConsulEnv(t)
	if !ok {
		return
	}

	initialAddress := fmt.Sprintf(":%d", 28080+time.Now().Nanosecond()%500)
	updatedAddress := fmt.Sprintf(":%d", 29080+time.Now().Nanosecond()%500)

	if err := publishConsulConfig(env, renderConsulServiceAddressYAML(initialAddress)); err != nil {
		t.Fatalf("publish initial consul config failed: %v", err)
	}

	client, err := NewClient(ClientOptions{
		Address:    env.addr,
		Scheme:     env.scheme,
		Datacenter: env.datacenter,
		Namespace:  env.namespace,
		Token:      env.token,
		WaitTimeMs: 500,
	})
	if err != nil {
		t.Fatalf("create consul api client failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	manager, err := config.New(
		config.Provider(NewWithClient(client, BootstrapSource{Name: "integration", Prefix: env.groupID, Key: env.dataID})),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	if err = waitUntilConsul(15*time.Second, func() bool {
		snapshot := manager.Current()
		if snapshot == nil {
			return false
		}
		return snapshot.String("service.address", "") == initialAddress
	}); err != nil {
		t.Fatalf("initial consul value not visible in snapshot: %v", err)
	}

	beforeVersion := manager.Current().Version()
	if err = publishConsulConfig(env, renderConsulServiceAddressYAML(updatedAddress)); err != nil {
		t.Fatalf("publish updated consul config failed: %v", err)
	}

	if err = waitUntilConsul(20*time.Second, func() bool {
		snapshot := manager.Current()
		if snapshot == nil {
			return false
		}
		return snapshot.String("service.address", "") == updatedAddress && snapshot.Version() > beforeVersion
	}); err != nil {
		t.Fatalf("watcher refresh did not propagate updated value: %v", err)
	}

	versionBeforeClose := manager.Current().Version()
	if err = manager.Close(); err != nil {
		t.Fatalf("close manager failed: %v", err)
	}

	closedAddress := fmt.Sprintf(":%d", 30080+time.Now().Nanosecond()%500)
	if err = publishConsulConfig(env, renderConsulServiceAddressYAML(closedAddress)); err != nil {
		t.Fatalf("publish post-close consul config failed: %v", err)
	}
	if err = assertNoRefreshAfterCloseConsul(manager, versionBeforeClose, updatedAddress, 5*time.Second); err != nil {
		t.Fatalf("expected no refresh after close: %v", err)
	}
}

// loadIntegrationConsulEnv 读取并校验集成测试所需环境；缺失必须项时 Skip。
func loadIntegrationConsulEnv(t *testing.T) (integrationConsulEnv, bool) {
	t.Helper()
	env := integrationConsulEnv{
		addr:       strings.TrimSpace(os.Getenv("CONFIG_IT_CONSUL_ADDR")),
		groupID:    strings.TrimSpace(os.Getenv("CONFIG_IT_CONSUL_GROUP")),
		dataID:     strings.TrimSpace(os.Getenv("CONFIG_IT_CONSUL_DATA_ID")),
		scheme:     strings.TrimSpace(os.Getenv("CONFIG_IT_CONSUL_SCHEME")),
		datacenter: strings.TrimSpace(os.Getenv("CONFIG_IT_CONSUL_DATACENTER")),
		namespace:  strings.TrimSpace(os.Getenv("CONFIG_IT_CONSUL_NAMESPACE")),
		token:      strings.TrimSpace(os.Getenv("CONFIG_IT_CONSUL_TOKEN")),
	}
	if env.scheme == "" {
		env.scheme = "http"
	}
	if env.addr == "" || env.groupID == "" || env.dataID == "" {
		t.Skip("skip integration consul test: required env missing (CONFIG_IT_CONSUL_ADDR/CONFIG_IT_CONSUL_GROUP/CONFIG_IT_CONSUL_DATA_ID)")
		return integrationConsulEnv{}, false
	}
	return env, true
}

// publishConsulConfig 向 Consul KV 发布配置内容（用于触发真实 watcher 链路）。
func publishConsulConfig(env integrationConsulEnv, content string) error {
	path, err := buildConsulKVPath(env.groupID, env.dataID)
	if err != nil {
		return err
	}
	address, scheme, pathPrefix, err := normalizeConsulAddress(env.addr, env.scheme)
	if err != nil {
		return err
	}

	cfg := api.DefaultConfig()
	cfg.Address = address
	cfg.Scheme = scheme
	cfg.PathPrefix = pathPrefix
	cfg.Datacenter = env.datacenter
	cfg.Namespace = env.namespace
	cfg.Token = env.token

	client, err := api.NewClient(cfg)
	if err != nil {
		return errorf("create consul publish client failed: %w", err)
	}
	_, err = client.KV().Put(&api.KVPair{Key: path, Value: []byte(content)}, nil)
	if err != nil {
		return errorf("publish consul config failed path=%s: %w", path, err)
	}
	return nil
}

// waitUntilConsul 轮询等待条件成立。
func waitUntilConsul(timeout time.Duration, check func() bool) error {
	deadline := time.Now().Add(timeout)
	for {
		if check() {
			return nil
		}
		if time.Now().After(deadline) {
			return errorf("timeout waiting condition")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// assertNoRefreshAfterCloseConsul 校验在观察窗口内快照版本和值保持稳定。
func assertNoRefreshAfterCloseConsul(manager config.Manager, expectedVersion uint64, expectedAddress string, duration time.Duration) error {
	deadline := time.Now().Add(duration)
	for {
		snapshot := manager.Current()
		if snapshot == nil {
			return errorf("latest snapshot is nil")
		}
		if snapshot.Version() != expectedVersion {
			return errorf("version changed after close expected=%d actual=%d", expectedVersion, snapshot.Version())
		}
		if snapshot.String("service.address", "") != expectedAddress {
			return errorf("service.address changed after close expected=%s actual=%s", expectedAddress, snapshot.String("service.address", ""))
		}
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// renderConsulServiceAddressYAML 生成最小可用测试配置文本。
func renderConsulServiceAddressYAML(address string) string {
	return fmt.Sprintf("service:\n  address: \"%s\"\n", address)
}
