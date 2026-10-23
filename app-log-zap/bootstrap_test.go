package applogzap

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/config/provider"
	"github.com/yangzhuangqiu/gobase/errcode"
	baselog "github.com/yangzhuangqiu/gobase/log"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
)

type appLogZapWatchCloser struct {
	// once 保证测试 watcher 的关闭回调只执行一次。
	once sync.Once
	// onClose 用于把关闭状态回写到远端测试 Client。
	onClose func()
}

// Close 模拟 watcher 的幂等资源释放，并通知测试 Client 停止投递事件。
func (this *appLogZapWatchCloser) Close() error {
	this.once.Do(func() {
		if this.onClose != nil {
			this.onClose()
		}
	})
	return nil
}

type appLogZapRemoteClient struct {
	// mu 串行保护远端内容、回调和关闭状态。
	mu sync.Mutex
	// content 是 Config Session 每次 Load 获取的当前远端配置。
	content string
	// onChange 是 Watch 注册的远端配置变化回调。
	onChange func(content []byte)
	// closed 标记 watcher 已关闭，关闭后 EmitChange 不再投递事件。
	closed bool
}

// Name 返回热更新测试 Provider 的稳定名称。
func (this *appLogZapRemoteClient) Name() string { return "demo" }

// Open 创建热更新测试使用的 Session。
func (this *appLogZapRemoteClient) Open(context.Context, map[string]any) (provider.Session, error) {
	return &appLogZapSession{client: this}, nil
}

// appLogZapSource 是日志热更新测试使用的单个 Source。
type appLogZapSource struct{}

// Info 返回测试 Source 的稳定元信息。
func (this appLogZapSource) Info() provider.SourceInfo { return provider.SourceInfo{Name: "main"} }

// appLogZapSession 把测试 Client 适配为 Config Session。
type appLogZapSession struct{ client *appLogZapRemoteClient }

// Sources 返回日志热更新测试使用的 Source。
func (this *appLogZapSession) Sources() []provider.Source {
	return []provider.Source{appLogZapSource{}}
}

// Load 拉取当前日志配置。
func (this *appLogZapSession) Load(ctx context.Context, _ provider.Source) ([]byte, error) {
	return this.client.GetConfig(ctx, "g", "d")
}

// Watch 监听日志配置变化并转换为失效通知。
func (this *appLogZapSession) Watch(ctx context.Context, _ provider.Source, onChange func()) (io.Closer, error) {
	return this.client.WatchConfig(ctx, "g", "d", func(_ []byte) { onChange() })
}

// Close 结束测试 Session；测试 Client 没有独立资源。
func (this *appLogZapSession) Close() error { return nil }

// GetConfig 返回热更新测试当前发布的远端配置内容。
func (this *appLogZapRemoteClient) GetConfig(ctx context.Context, groupID, dataID string) ([]byte, error) {
	this.mu.Lock()
	defer this.mu.Unlock()
	return []byte(this.content), nil
}

// WatchConfig 注册单个变化回调，并返回可观测关闭状态的 watcher。
func (this *appLogZapRemoteClient) WatchConfig(ctx context.Context, groupID, dataID string, onChange func(content []byte)) (io.Closer, error) {
	this.mu.Lock()
	this.onChange = onChange
	this.closed = false
	this.mu.Unlock()
	return &appLogZapWatchCloser{
		onClose: func() {
			this.mu.Lock()
			defer this.mu.Unlock()
			this.closed = true
		},
	}, nil
}

// EmitChange 更新远端内容；watcher 仍存活时同步触发失效通知。
func (this *appLogZapRemoteClient) EmitChange(content string) {
	this.mu.Lock()
	this.content = content
	onChange := this.onChange
	closed := this.closed
	this.mu.Unlock()
	if onChange != nil && !closed {
		onChange([]byte(content))
	}
}

// waitForAppLogZapCondition 在限时内轮询异步配置传播结果。
func waitForAppLogZapCondition(timeout time.Duration, check func() bool) bool {
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

// TestUseCaseAppLogZapBootstrapAndClose 验证 Bootstrap 可基于 ConfigManager 初始化并关闭日志 Provider。
//
// 测试方法：
// 1. 构建包含 Zap 默认配置的 Manager；
// 2. 通过 Bootstrap.Build 创建日志 Provider，并注入 Redactor；
// 3. 断言 Provider 关闭后仅 error 被写入文件，且敏感字段已替换。
func TestUseCaseAppLogZapBootstrapAndClose(t *testing.T) {
	// 第一步：声明文件输出，并构建由适配器消费的配置 Manager。
	logFile := filepath.Join(t.TempDir(), "auto.log")
	defaultCfg := map[string]any{
		"log": map[string]any{
			"zap": map[string]any{
				"mode":   "file",
				"format": "text",
				"level":  "error",
				"rotate": map[string]any{
					"filename": logFile,
				},
			},
		},
	}

	manager, err := config.New(
		config.File(config.FileDisabled()),
		config.Defaults(defaultCfg),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build config manager failed: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	bootstrap := New(WithProviderOptions(
		logzap.WithRedactor(baselog.NewKeyRedactor("token")),
	))
	provider, err := bootstrap.Build(context.Background(), manager)
	if err != nil {
		t.Fatalf("build logging provider failed: %v", err)
	}
	logger := provider.Factory().Get("app.logzap.service")
	logger.Info(context.Background(), "info should be filtered")
	logger.Error(context.Background(), "error should be written", baselog.F("token", "plain-token"))
	if err = provider.Close(); err != nil {
		t.Fatalf("close logging provider failed: %v", err)
	}

	// 第二步：读取最终文件，联合验证 level 过滤、正常输出与脱敏。
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file failed: %v", err)
	}
	text := string(content)
	if strings.Contains(text, "info should be filtered") {
		t.Fatalf("expected info log filtered by level=error, content=%s", text)
	}
	if !strings.Contains(text, "error should be written") {
		t.Fatalf("expected error log in file, content=%s", text)
	}
	if strings.Contains(text, "plain-token") || !strings.Contains(text, baselog.RedactedValue) {
		t.Fatalf("expected provider option redaction, content=%s", text)
	}
}

// TestBuildExposesNarrowZapProviderCapability 验证标准 Build 结果可被框架包装器借用同生命周期 Zap 核心。
// 测试方法：使用默认配置构建 Provider，通过匿名窄接口断言 ZapProvider，校验核心非 nil 且仍由外层 Provider 关闭。
func TestBuildExposesNarrowZapProviderCapability(t *testing.T) {
	provider, err := New().Build(context.Background(), nil)
	if err != nil {
		t.Fatalf("build default provider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	accessor, ok := provider.(interface{ ZapProvider() *logzap.Provider })
	if !ok || accessor.ZapProvider() == nil {
		t.Fatalf("expected narrow zap provider capability: provider=%T", provider)
	}
}

// TestUseCaseAppLogZapRuntimeLogLevelHotReload 验证 app-log-zap 在配置热更新时仅动态调整日志级别。
//
// 测试方法：
// 1. 远端初始配置 level=error；
// 2. 执行业务后触发远端变更为 level=debug；
// 3. 轮询日志文件，断言 reload 后 debug 日志可见、reload 前 debug 仍被过滤。
func TestUseCaseAppLogZapRuntimeLogLevelHotReload(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "hot-reload.log")
	client := &appLogZapRemoteClient{
		content: "log:\n  zap:\n    mode: file\n    format: text\n    level: error\n    rotate:\n      filename: \"" + strings.ReplaceAll(logFile, "\\", "/") + "\"\n",
	}

	manager, err := config.New(config.Provider(client)).Build(context.Background())
	if err != nil {
		t.Fatalf("build config manager failed: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	provider, err := New().Build(context.Background(), manager)
	if err != nil {
		t.Fatalf("build logging provider failed: %v", err)
	}
	logger := provider.Factory().Get("app.log.reload.usecase")
	logger.Debug(context.Background(), "debug before reload")
	logger.Error(context.Background(), "error before reload")

	client.EmitChange("log:\n  zap:\n    mode: file\n    format: text\n    level: debug\n    rotate:\n      filename: \"" + strings.ReplaceAll(logFile, "\\", "/") + "\"\n")
	ok := waitForAppLogZapCondition(2*time.Second, func() bool {
		logger.Debug(context.Background(), "debug after reload")
		content, readErr := os.ReadFile(logFile)
		if readErr != nil {
			return false
		}
		return strings.Contains(string(content), "debug after reload")
	})
	if !ok {
		t.Fatalf("runtime log level did not refresh")
	}
	if err = provider.Close(); err != nil {
		t.Fatalf("close logging provider failed: %v", err)
	}

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file failed: %v", err)
	}
	text := string(content)
	if strings.Contains(text, "debug before reload") {
		t.Fatalf("expected debug before reload to be filtered, content=%s", text)
	}
	if !strings.Contains(text, "debug after reload") {
		t.Fatalf("expected debug after reload in file, content=%s", text)
	}
	if !strings.Contains(text, "error before reload") {
		t.Fatalf("expected error before reload in file, content=%s", text)
	}
}

// TestAppLogZapLifecycleContextCancelsSubscriptionWithoutClosingCore 验证运行 context 与日志资源所有权保持分离。
//
// 测试方法：使用可取消 context 调用 Bootstrap.Build，取消后等待日志订阅归零，再写日志并显式 Close，断言核心在取消后仍可输出。
func TestAppLogZapLifecycleContextCancelsSubscriptionWithoutClosingCore(t *testing.T) {
	// 第一步：仅使用本地默认配置构建日志核心，确保 Manager 中只有 app-log-zap 的订阅。
	logFile := filepath.Join(t.TempDir(), "lifecycle-context.log")
	manager, err := config.New(
		config.File(config.FileDisabled()),
		config.Defaults(map[string]any{"log": map[string]any{"zap": map[string]any{
			"mode": "file", "format": "text", "level": "info",
			"rotate": map[string]any{"filename": logFile},
		}}}),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build config manager failed: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	buildCtx, cancelBuild := context.WithCancel(context.Background())
	provider, err := New().Build(buildCtx, manager)
	if err != nil {
		t.Fatalf("build logging provider failed: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if count := manager.Status().Resources().SubscriptionCount; count != 1 {
		t.Fatalf("expected app-log-zap subscription, got=%d", count)
	}

	// 第二步：取消应用 lifecycle context，只等待配置订阅结束，不关闭日志核心。
	cancelBuild()
	if !waitForAppLogZapCondition(time.Second, func() bool {
		return manager.Status().Resources().SubscriptionCount == 0
	}) {
		t.Fatal("logging subscription was not canceled by lifecycle context")
	}
	provider.Factory().Get("app.log.lifecycle").Info(context.Background(), "log after lifecycle cancellation")

	// 第三步：显式 Close 才刷新并释放输出资源，取消 context 后写入的事件必须保留。
	if err = provider.Close(); err != nil {
		t.Fatalf("close logging provider failed: %v", err)
	}
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read lifecycle log failed: %v", err)
	}
	if !strings.Contains(string(content), "log after lifecycle cancellation") {
		t.Fatalf("lifecycle cancellation closed log core early: %s", content)
	}
}

// TestRuntimeProviderCloseCancelsSubscriptionBeforeManager 验证组合 Provider 独立拥有日志订阅。
//
// 测试方法：直接构建仍处于 running 的 Manager 与组合 Provider，关闭 Provider 后断言订阅归零而 Manager 保持可用。
func TestRuntimeProviderCloseCancelsSubscriptionBeforeManager(t *testing.T) {
	// 第一步：构建无外部 watcher 的 Manager，并由 app-log-zap 注册唯一订阅。
	manager, err := config.New(
		config.File(config.FileDisabled()),
		config.Defaults(map[string]any{"log": map[string]any{"zap": map[string]any{"level": "info"}}}),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build config manager failed: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	provider, err := newRuntimeProvider(context.Background(), manager, defaultLogScope, nil)
	if err != nil {
		t.Fatalf("build runtime provider failed: %v", err)
	}
	if count := manager.Status().Resources().SubscriptionCount; count != 1 {
		t.Fatalf("expected one logging subscription, got=%d", count)
	}

	// 第二步：只关闭日志组合资源，订阅必须释放，但 Manager 不应被越权关闭。
	if err = provider.Close(); err != nil {
		t.Fatalf("close runtime provider failed: %v", err)
	}
	status := manager.Status()
	if status.Resources().SubscriptionCount != 0 || status.State() != config.ManagerStateRunning {
		t.Fatalf("unexpected manager state after provider close: state=%s subscriptions=%d", status.State(), status.Resources().SubscriptionCount)
	}
}

// TestRuntimeProviderBuildFailureRollsBackSubscription 验证 Zap 核心构建失败既可按层分支，也不会遗留 ConfigManager 订阅。
//
// 测试方法：通过 Bootstrap 发布缺少 filename 的 file 配置，断言同时命中 bootstrap、provider、log-zap 错误码且订阅归零。
func TestRuntimeProviderBuildFailureRollsBackSubscription(t *testing.T) {
	// 第一步：让 ConfigManager 正常发布，但保留仅在 Zap 核心构建期失败的配置。
	manager, err := config.New(
		config.File(config.FileDisabled()),
		config.Defaults(map[string]any{"log": map[string]any{"zap": map[string]any{"mode": "file"}}}),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build config manager failed: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	// 第二步：核心失败后检查适配层已补偿先取得的订阅资源。
	provider, buildErr := New().Build(context.Background(), manager)
	if buildErr == nil || provider != nil {
		t.Fatalf("expected runtime provider build failure: provider=%T err=%v", provider, buildErr)
	}
	if !errcode.IsCode[ErrorCode](buildErr, ErrCodeBootstrapBuildFailed) {
		t.Fatalf("expected bootstrap error code, err=%v", buildErr)
	}
	if !errcode.IsCode[ErrorCode](buildErr, ErrCodeProviderCoreBuildFailed) {
		t.Fatalf("expected provider core build error code, err=%v", buildErr)
	}
	if !errcode.IsCode[logzap.ErrorCode](buildErr, logzap.ErrCodeLogFileFilenameRequired) {
		t.Fatalf("expected nested log-zap filename error code, err=%v", buildErr)
	}
	if count := manager.Status().Resources().SubscriptionCount; count != 0 {
		t.Fatalf("failed provider build leaked subscription: count=%d", count)
	}
}

// TestNewProviderFromManagerSubscribeFailureUsesErrorCode 验证配置订阅失败具有适配层和 config 双层错误码。
//
// 测试方法：关闭 Manager 后调用公开构建入口，断言命中 provider subscribe code，并保留底层 manager closed code。
func TestNewProviderFromManagerSubscribeFailureUsesErrorCode(t *testing.T) {
	manager, err := config.New(config.File(config.FileDisabled())).Build(context.Background())
	if err != nil {
		t.Fatalf("build config manager failed: %v", err)
	}
	if err = manager.Close(); err != nil {
		t.Fatalf("close config manager failed: %v", err)
	}
	provider, buildErr := NewProviderFromManager(context.Background(), manager, DefaultManagerScope)
	if buildErr == nil || provider != nil {
		t.Fatalf("expected subscription failure: provider=%T err=%v", provider, buildErr)
	}
	if !errcode.IsCode[ErrorCode](buildErr, ErrCodeProviderSubscribeFailed) {
		t.Fatalf("expected provider subscribe error code, err=%v", buildErr)
	}
	if !errcode.IsCode[config.ErrorCode](buildErr, config.ErrCodeConfigManagerClosed) {
		t.Fatalf("expected nested config manager closed error code, err=%v", buildErr)
	}
}

// TestRuntimeConfigBridgeApplyFailureUsesErrorCode 验证动态 level 非法时由适配层标记通知阶段。
//
// 测试方法：先创建纯 Zap 核心，再投递非法 level，断言同时命中 runtime-config 和 log-zap level 错误码。
func TestRuntimeConfigBridgeApplyFailureUsesErrorCode(t *testing.T) {
	bridge := &runtimeConfigBridge{latest: logzap.DefaultConfig()}
	core, err := bridge.buildCore(nil)
	if err != nil {
		t.Fatalf("build log core failed: %v", err)
	}
	t.Cleanup(func() { _ = core.Close() })
	err = bridge.onUpdate(config.TypedUpdate[logzap.Config]{
		Value: logzap.Config{Level: "verbose"},
	})
	if !errcode.IsCode[ErrorCode](err, ErrCodeProviderRuntimeConfigApplyFailed) {
		t.Fatalf("expected runtime config apply error code, err=%v", err)
	}
	if !errcode.IsCode[logzap.ErrorCode](err, logzap.ErrCodeLogLevelInvalid) {
		t.Fatalf("expected nested log-zap level error code, err=%v", err)
	}
}
