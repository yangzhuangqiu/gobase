package global

import (
	"context"
	"errors"
	"testing"

	applogzap "github.com/yangzhuangqiu/gobase/app-log-zap"
	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/errcode"
	baselog "github.com/yangzhuangqiu/gobase/log"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
)

// globalFailingProvider 模拟关闭阶段失败的全局日志资源。
type globalFailingProvider struct {
	// closeErr 是 Close 应返回并保留在 global 错误链中的根因。
	closeErr error
}

// Factory 返回无外部资源的 LoggerFactory，供 globalRuntime 完成正常安装。
func (this *globalFailingProvider) Factory() baselog.LoggerFactory {
	return baselog.NewLoggerFactory(nil)
}

// Close 返回预置根因，用于验证全局关闭错误码和 cause 保留语义。
func (this *globalFailingProvider) Close() error {
	return this.closeErr
}

// TestGlobalRuntimeOwnsProviderSubscriptionAndManager 验证迁移后的全局入口保持正确资源所有权。
//
// 测试方法：用真实 Manager 激活 global，断言组合 Provider 注册订阅；Close 后断言 Provider 清空、订阅释放且 Manager 关闭。
func TestGlobalRuntimeOwnsProviderSubscriptionAndManager(t *testing.T) {
	prepareGlobalStateForTest(t)
	// 第一步：创建无外部 watcher 的真实 Manager，并安装新的 app-log-zap 组合 Provider。
	manager, err := config.New(
		config.File(config.FileDisabled()),
		config.Defaults(map[string]any{"log": map[string]any{"zap": logzap.Config{Level: "info"}}}),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build config manager failed: %v", err)
	}
	if err = runtimeInstance.activateWithManager(manager); err != nil {
		_ = manager.Close()
		t.Fatalf("activate global runtime failed: %v", err)
	}
	if Provider() == nil || manager.Status().Resources().SubscriptionCount != 1 {
		t.Fatalf("global provider did not own subscription: provider=%T subscriptions=%d", Provider(), manager.Status().Resources().SubscriptionCount)
	}
	Get("global.lifecycle.test").Info(context.Background(), "global provider active")

	// 第二步：显式 Close 必须先取消订阅，再关闭 Manager，并保持幂等。
	if err = Close(); err != nil {
		t.Fatalf("close global runtime failed: %v", err)
	}
	status := manager.Status()
	if Provider() != nil || status.Resources().SubscriptionCount != 0 || status.State() != config.ManagerStateClosed {
		t.Fatalf("unexpected global close state: provider=%T manager=%s subscriptions=%d", Provider(), status.State(), status.Resources().SubscriptionCount)
	}
	if err = Close(); err != nil {
		t.Fatalf("second close should return first stable result: %v", err)
	}
}

// TestGlobalRuntimeDegradesToPureProvider 验证 ConfigManager 不可用时不会把配置依赖带入回退核心。
//
// 测试方法：直接触发降级，断言只安装纯 log-zap Provider、Manager 保持 nil，并可正常关闭。
func TestGlobalRuntimeDegradesToPureProvider(t *testing.T) {
	prepareGlobalStateForTest(t)
	runtimeInstance.degradeToFallback(errors.New("config unavailable"))
	if runtimeInstance.manager != nil || Provider() == nil {
		t.Fatalf("unexpected fallback state: manager=%T provider=%T", runtimeInstance.manager, Provider())
	}
	Logger().Warn(context.Background(), "global fallback active")
	if err := Close(); err != nil {
		t.Fatalf("close fallback provider failed: %v", err)
	}
}

// TestGlobalProviderBuildFailureUsesLayeredErrorCodes 验证 global 构建失败保留全局、适配器与配置三层语义。
//
// 测试方法：向内部构建入口传入 nil Manager，断言错误链依次可命中 global provider、provider subscribe 和 config subscribe code。
func TestGlobalProviderBuildFailureUsesLayeredErrorCodes(t *testing.T) {
	provider, err := newGlobalRuntime().buildProviderFromManager(nil)
	if err == nil || provider != nil {
		t.Fatalf("expected global provider build failure: provider=%T err=%v", provider, err)
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeGlobalProviderBuildFailed) {
		t.Fatalf("expected global provider build error code, err=%v", err)
	}
	if !errcode.IsCode[applogzap.ErrorCode](err, applogzap.ErrCodeProviderSubscribeFailed) {
		t.Fatalf("expected nested app-log-zap subscribe error code, err=%v", err)
	}
	if !errcode.IsCode[config.ErrorCode](err, config.ErrCodeConfigManagerSubscribeFailed) {
		t.Fatalf("expected nested config subscribe error code, err=%v", err)
	}
}

// TestGlobalRuntimeCloseFailureUsesErrorCode 验证全局关闭失败可分支且不会丢失原始资源错误。
//
// 测试方法：安装 Close 返回预置错误的 Provider，执行两次 shutdown，断言错误码、cause、资源清理和幂等结果。
func TestGlobalRuntimeCloseFailureUsesErrorCode(t *testing.T) {
	cause := errors.New("provider close failed")
	runtime := newGlobalRuntime()
	runtime.assignProvider(&globalFailingProvider{closeErr: cause})
	err := runtime.shutdown()
	if !errcode.IsCode[ErrorCode](err, ErrCodeGlobalCloseFailed) {
		t.Fatalf("expected global close error code, err=%v", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("expected global close error to retain cause, err=%v", err)
	}
	if runtime.provider != nil || runtime.factory != nil || runtime.logger != nil {
		t.Fatalf("global close failure did not clear resource references: provider=%T factory=%T logger=%T", runtime.provider, runtime.factory, runtime.logger)
	}
	if secondErr := runtime.shutdown(); secondErr != err {
		t.Fatalf("expected idempotent close error instance: first=%v second=%v", err, secondErr)
	}
}

// prepareGlobalStateForTest 将包级全局状态重置为可重复测试基线。
// 阶段角色：隔离 package init 和不同测试持有的全局资源。
func prepareGlobalStateForTest(t *testing.T) {
	t.Helper()
	_ = Close()
	runtimeInstance = newGlobalRuntime()
	t.Cleanup(func() {
		_ = Close()
		runtimeInstance = newGlobalRuntime()
	})
}
