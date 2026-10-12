//go:build integration

package nacos

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yangzhuangqiu/gobase/config"
)

// integrationNacosEnv 描述真实 Nacos 集成测试所需环境。
type integrationNacosEnv struct {
	addr        string
	groupID     string
	dataID      string
	namespace   string
	username    string
	password    string
	scheme      string
	context     string
	httpBase    string
	accessToken string
}

// TestIntegrationNacosWatchRefreshSkeleton 是真实 Nacos 链路测试骨架：
// 1) 发布初始配置
// 2) Build 并确认初始值可读
// 3) 发布新配置并等待 watcher -> refresh 生效
//
// 运行前需提供环境变量（缺失时自动 Skip）：
// - CONFIG_IT_NACOS_ADDR
// - CONFIG_IT_NACOS_GROUP
// - CONFIG_IT_NACOS_DATA_ID
// 可选：
// - CONFIG_IT_NACOS_NAMESPACE
// - CONFIG_IT_NACOS_USERNAME
// - CONFIG_IT_NACOS_PASSWORD
// - CONFIG_IT_NACOS_SCHEME（默认 http）
// - CONFIG_IT_NACOS_CONTEXT_PATH（默认 /nacos）
// - CONFIG_IT_NACOS_HTTP_BASE（发布接口基址，默认按 addr 推导）
// - CONFIG_IT_NACOS_TOKEN（鉴权 token）
func TestIntegrationNacosWatchRefreshSkeleton(t *testing.T) {
	env, ok := loadIntegrationNacosEnv(t)
	if !ok {
		return
	}

	initialAddress := fmt.Sprintf(":%d", 18080+time.Now().Nanosecond()%500)
	updatedAddress := fmt.Sprintf(":%d", 19080+time.Now().Nanosecond()%500)

	if err := publishNacosConfig(env, renderServiceAddressYAML(initialAddress)); err != nil {
		t.Fatalf("publish initial nacos config failed: %v", err)
	}

	client, err := NewClient(ClientOptions{
		Address:     env.addr,
		NamespaceID: env.namespace,
		Username:    env.username,
		Password:    env.password,
		Scheme:      env.scheme,
		ContextPath: env.context,
	})
	if err != nil {
		t.Fatalf("create nacos sdk client failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	manager, err := config.New(
		config.Provider(NewWithClient(client, BootstrapSource{GroupID: env.groupID, DataID: env.dataID})),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	if err = waitUntil(15*time.Second, func() bool {
		snapshot := manager.Current()
		if snapshot == nil {
			return false
		}
		return snapshot.String("service.address", "") == initialAddress
	}); err != nil {
		t.Fatalf("initial nacos value not visible in snapshot: %v", err)
	}

	beforeVersion := manager.Current().Version()
	if err = publishNacosConfig(env, renderServiceAddressYAML(updatedAddress)); err != nil {
		t.Fatalf("publish updated nacos config failed: %v", err)
	}

	if err = waitUntil(20*time.Second, func() bool {
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

	closedAddress := fmt.Sprintf(":%d", 20080+time.Now().Nanosecond()%500)
	if err = publishNacosConfig(env, renderServiceAddressYAML(closedAddress)); err != nil {
		t.Fatalf("publish post-close nacos config failed: %v", err)
	}
	if err = assertNoRefreshAfterClose(manager, versionBeforeClose, updatedAddress, 5*time.Second); err != nil {
		t.Fatalf("expected no refresh after close: %v", err)
	}
}

// loadIntegrationNacosEnv 读取并校验集成测试所需环境；缺失必须项时 Skip。
func loadIntegrationNacosEnv(t *testing.T) (integrationNacosEnv, bool) {
	t.Helper()
	env := integrationNacosEnv{
		addr:        strings.TrimSpace(os.Getenv("CONFIG_IT_NACOS_ADDR")),
		groupID:     strings.TrimSpace(os.Getenv("CONFIG_IT_NACOS_GROUP")),
		dataID:      strings.TrimSpace(os.Getenv("CONFIG_IT_NACOS_DATA_ID")),
		namespace:   strings.TrimSpace(os.Getenv("CONFIG_IT_NACOS_NAMESPACE")),
		username:    strings.TrimSpace(os.Getenv("CONFIG_IT_NACOS_USERNAME")),
		password:    strings.TrimSpace(os.Getenv("CONFIG_IT_NACOS_PASSWORD")),
		scheme:      strings.TrimSpace(os.Getenv("CONFIG_IT_NACOS_SCHEME")),
		context:     strings.TrimSpace(os.Getenv("CONFIG_IT_NACOS_CONTEXT_PATH")),
		httpBase:    strings.TrimSpace(os.Getenv("CONFIG_IT_NACOS_HTTP_BASE")),
		accessToken: strings.TrimSpace(os.Getenv("CONFIG_IT_NACOS_TOKEN")),
	}
	if env.scheme == "" {
		env.scheme = "http"
	}
	if env.context == "" {
		env.context = "/nacos"
	}
	if env.addr == "" || env.groupID == "" || env.dataID == "" {
		t.Skip("skip integration nacos test: required env missing (CONFIG_IT_NACOS_ADDR/CONFIG_IT_NACOS_GROUP/CONFIG_IT_NACOS_DATA_ID)")
		return integrationNacosEnv{}, false
	}
	return env, true
}

// publishNacosConfig 通过 Nacos OpenAPI 发布配置内容（用于触发真实 watcher 链路）。
func publishNacosConfig(env integrationNacosEnv, content string) error {
	base := env.httpBase
	if base == "" {
		first := strings.TrimSpace(strings.Split(env.addr, ",")[0])
		if !strings.Contains(first, "://") {
			first = env.scheme + "://" + first
		}
		base = strings.TrimRight(first, "/") + env.context
	}
	endpoint := strings.TrimRight(base, "/") + "/v1/cs/configs"

	form := url.Values{}
	form.Set("group", env.groupID)
	form.Set("dataId", env.dataID)
	form.Set("content", content)
	if env.namespace != "" {
		form.Set("tenant", env.namespace)
	}
	if env.accessToken != "" {
		form.Set("accessToken", env.accessToken)
	}

	resp, err := http.PostForm(endpoint, form)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return errorf("publish nacos config failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if !strings.Contains(strings.ToLower(string(body)), "true") {
		return errorf("publish nacos config unexpected body=%s", strings.TrimSpace(string(body)))
	}
	return nil
}

// waitUntil 轮询等待条件成立。
func waitUntil(timeout time.Duration, check func() bool) error {
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

// assertNoRefreshAfterClose 校验在观察窗口内快照版本和值保持稳定。
func assertNoRefreshAfterClose(manager config.Manager, expectedVersion uint64, expectedAddress string, duration time.Duration) error {
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

// renderServiceAddressYAML 生成最小可用测试配置文本。
func renderServiceAddressYAML(address string) string {
	return fmt.Sprintf("service:\n  address: \"%s\"\n", address)
}
