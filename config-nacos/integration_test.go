//go:build integration

package nacos

import (
	"context"
	"encoding/json"
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

// integrationNacosHTTPTimeout 限制测试 OpenAPI 请求等待时间，避免环境故障时长期阻塞。
const integrationNacosHTTPTimeout = 10 * time.Second

// integrationNacosEnv 描述真实 Nacos 集成测试所需环境。
type integrationNacosEnv struct {
	// addr 是 SDK 连接地址；必填，支持逗号分隔多个节点。
	addr string
	// groupID 是测试专用配置分组；必填。
	groupID string
	// dataID 是测试专用配置标识；必填，测试期间会被临时覆盖。
	dataID string
	// namespace 是目标命名空间 ID；为空时使用 public。
	namespace string
	// username 是 SDK 与自动登录共用的认证用户名。
	username string
	// password 是 SDK 与自动登录共用的认证密码，不得写入日志。
	password string
	// scheme 是从 addr 推导 OpenAPI 地址时使用的协议，默认 http。
	scheme string
	// context 是 Nacos HTTP 上下文路径，默认 /nacos。
	context string
	// httpBase 是可选 OpenAPI 基址，显式值优先于 addr 推导结果。
	httpBase string
	// accessToken 是可选显式 token；为空且账号密码完整时自动获取。
	accessToken string
}

// integrationNacosConfigBackup 保存测试写入前的远端状态，用于测试结束时精确恢复。
type integrationNacosConfigBackup struct {
	// content 是原配置内容；仅在 exists=true 时参与恢复。
	content string
	// exists 表示测试开始前目标 group/dataId 是否存在。
	exists bool
}

// integrationNacosLoginResponse 描述 Nacos 登录接口中测试需要的最小响应。
type integrationNacosLoginResponse struct {
	// AccessToken 是后续配置 OpenAPI 请求使用的临时鉴权令牌。
	AccessToken string `json:"accessToken"`
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
	var err error
	env, err = authenticateIntegrationNacos(env)
	if err != nil {
		t.Fatalf("authenticate integration nacos failed: %v", err)
	}

	// 写入测试值之前先保存远端状态，确保成功与失败路径都不会长期污染环境。
	backup, err := loadIntegrationNacosConfig(env)
	if err != nil {
		t.Fatalf("backup original nacos config failed: %v", err)
	}
	t.Cleanup(func() {
		if restoreErr := restoreIntegrationNacosConfig(env, backup); restoreErr != nil {
			t.Errorf("restore original nacos config failed: %v", restoreErr)
		}
	})

	initialAddress := fmt.Sprintf(":%d", 18080+time.Now().Nanosecond()%500)
	updatedAddress := fmt.Sprintf(":%d", 19080+time.Now().Nanosecond()%500)

	if err := publishNacosConfig(env, renderServiceAddressYAML(initialAddress)); err != nil {
		t.Fatalf("publish initial nacos config failed: %v", err)
	}

	servers, err := integrationNacosClientServers(env)
	if err != nil {
		t.Fatalf("build integration nacos servers failed: %v", err)
	}
	client, err := NewClient(ClientOptions{
		Servers:     servers,
		NamespaceID: env.namespace,
		Username:    env.username,
		Password:    env.password,
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

// integrationNacosClientServers 把测试环境地址转换为结构化节点声明。
// 阶段角色：真实 SDK 建连前复用 address 解析，再应用测试环境的 HTTP 协议与上下文路径。
func integrationNacosClientServers(env integrationNacosEnv) ([]Server, error) {
	parsed, err := parseServerConfigs(ClientOptions{Address: env.addr})
	if err != nil {
		return nil, err
	}
	servers := make([]Server, 0, len(parsed))
	for _, item := range parsed {
		servers = append(servers, Server{
			Host:        item.IpAddr,
			Port:        item.Port,
			Scheme:      env.scheme,
			ContextPath: env.context,
		})
	}
	return servers, nil
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

// authenticateIntegrationNacos 在未显式提供 token 时使用账号密码登录，并返回本次测试专用的鉴权环境。
//
// 语义：
// 1. 显式 token 优先，不发起登录请求；
// 2. 账号密码均为空时按未开启鉴权处理；
// 3. 只提供一项时 fail-fast，避免把不完整凭据带入后续写请求。
func authenticateIntegrationNacos(env integrationNacosEnv) (integrationNacosEnv, error) {
	if env.accessToken != "" {
		return env, nil
	}
	if env.username == "" && env.password == "" {
		return env, nil
	}
	if env.username == "" || env.password == "" {
		return integrationNacosEnv{}, errorf("nacos auto login requires both username and password")
	}

	// 登录凭据只放入请求体；错误信息不得包含用户名、密码或返回的 token。
	form := url.Values{}
	form.Set("username", env.username)
	form.Set("password", env.password)
	resp, err := doIntegrationNacosFormRequest(http.MethodPost, integrationNacosAPIBase(env)+"/v1/auth/users/login", form)
	if err != nil {
		return integrationNacosEnv{}, errorf("request nacos login failed", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return integrationNacosEnv{}, errorf("read nacos login response failed", err)
	}
	if resp.StatusCode/100 != 2 {
		return integrationNacosEnv{}, errorf("nacos login failed status=%d", resp.StatusCode)
	}
	var result integrationNacosLoginResponse
	if err = json.Unmarshal(body, &result); err != nil {
		return integrationNacosEnv{}, errorf("decode nacos login response failed", err)
	}
	env.accessToken = strings.TrimSpace(result.AccessToken)
	if env.accessToken == "" {
		return integrationNacosEnv{}, errorf("nacos login response missing accessToken")
	}
	return env, nil
}

// loadIntegrationNacosConfig 读取测试目标的原始状态，为 cleanup 阶段提供恢复依据。
func loadIntegrationNacosConfig(env integrationNacosEnv) (integrationNacosConfigBackup, error) {
	endpoint := integrationNacosAPIBase(env) + "/v1/cs/configs"
	resp, err := doIntegrationNacosFormRequest(http.MethodGet, endpoint, integrationNacosConfigIdentity(env))
	if err != nil {
		return integrationNacosConfigBackup{}, errorf("request original nacos config failed", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return integrationNacosConfigBackup{}, errorf("read original nacos config failed", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return integrationNacosConfigBackup{exists: false}, nil
	}
	if resp.StatusCode/100 != 2 {
		return integrationNacosConfigBackup{}, errorf("load original nacos config failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return integrationNacosConfigBackup{content: string(body), exists: true}, nil
}

// restoreIntegrationNacosConfig 恢复测试前状态：原配置存在则覆盖回原文，不存在则删除测试创建项。
func restoreIntegrationNacosConfig(env integrationNacosEnv, backup integrationNacosConfigBackup) error {
	if backup.exists {
		return publishNacosConfig(env, backup.content)
	}
	return deleteIntegrationNacosConfig(env)
}

// publishNacosConfig 通过 Nacos OpenAPI 发布配置内容（用于触发真实 watcher 链路）。
func publishNacosConfig(env integrationNacosEnv, content string) error {
	endpoint := integrationNacosAPIBase(env) + "/v1/cs/configs"
	form := integrationNacosConfigIdentity(env)
	form.Set("content", content)
	resp, err := doIntegrationNacosFormRequest(http.MethodPost, endpoint, form)
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

// deleteIntegrationNacosConfig 删除测试创建的配置，使“测试前不存在”状态可被还原。
func deleteIntegrationNacosConfig(env integrationNacosEnv) error {
	endpoint := integrationNacosAPIBase(env) + "/v1/cs/configs"
	resp, err := doIntegrationNacosFormRequest(http.MethodDelete, endpoint, integrationNacosConfigIdentity(env))
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return errorf("delete nacos config failed status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if !strings.Contains(strings.ToLower(string(body)), "true") {
		return errorf("delete nacos config unexpected body=%s", strings.TrimSpace(string(body)))
	}
	return nil
}

// integrationNacosConfigIdentity 生成读取、发布和删除共用的配置定位及鉴权参数。
func integrationNacosConfigIdentity(env integrationNacosEnv) url.Values {
	form := url.Values{}
	form.Set("group", env.groupID)
	form.Set("dataId", env.dataID)
	if env.namespace != "" {
		form.Set("tenant", env.namespace)
	}
	if env.accessToken != "" {
		form.Set("accessToken", env.accessToken)
	}
	return form
}

// integrationNacosAPIBase 选择显式 HTTP 基址，或从首个 SDK 地址推导 OpenAPI 基址。
func integrationNacosAPIBase(env integrationNacosEnv) string {
	if env.httpBase != "" {
		return strings.TrimRight(env.httpBase, "/")
	}
	first := strings.TrimSpace(strings.Split(env.addr, ",")[0])
	if !strings.Contains(first, "://") {
		first = env.scheme + "://" + first
	}
	return strings.TrimRight(first, "/") + env.context
}

// doIntegrationNacosFormRequest 按 Nacos OpenAPI 约定发送表单请求，并统一限制 HTTP 等待时间。
func doIntegrationNacosFormRequest(method, endpoint string, form url.Values) (*http.Response, error) {
	var body io.Reader
	if method == http.MethodPost {
		body = strings.NewReader(form.Encode())
	} else {
		endpoint += "?" + form.Encode()
	}
	request, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response, err := (&http.Client{Timeout: integrationNacosHTTPTimeout}).Do(request)
	if err != nil {
		// GET/DELETE 的 token 位于 query；禁止把底层 url.Error 原文带入测试日志。
		return nil, errorf("nacos openapi transport failed method=%s reason=request_failed", method)
	}
	return response, nil
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
