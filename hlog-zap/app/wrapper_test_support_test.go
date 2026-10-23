package apphlogzap

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/yangzhuangqiu/gobase/config"
	baselog "github.com/yangzhuangqiu/gobase/log"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
)

// testLogging 模拟由 composition root 显式传入 Wrap 的日志构造实现。
type testLogging struct {
	// provider 是 Build 成功后交给包装器管理的日志资源。
	provider baselog.Provider
	// buildErr 用于覆盖 delegate 构建失败分支。
	buildErr error
}

// Build 返回测试预设的 Provider 或错误，不读取 ConfigManager。
func (this *testLogging) Build(context.Context, config.Manager) (baselog.Provider, error) {
	return this.provider, this.buildErr
}

// testZapRuntimeProvider 模拟 app-log-zap 返回的“根 Provider + 窄 Zap 核心能力”。
type testZapRuntimeProvider struct {
	// core 是 Factory 与 hlog 共享且由当前 Provider 唯一关闭的 Zap 核心。
	core *logzap.Provider
	// closeOnce 保证测试资源可以同时由断言和 Cleanup 安全收口。
	closeOnce sync.Once
	// closeErr 保存首次核心关闭结果。
	closeErr error
}

// Factory 返回 Zap 核心持有的业务 LoggerFactory。
func (this *testZapRuntimeProvider) Factory() baselog.LoggerFactory { return this.core.Factory() }

// ZapProvider 暴露只借用、不转移关闭权的 Zap 核心。
func (this *testZapRuntimeProvider) ZapProvider() *logzap.Provider { return this.core }

// Close 幂等关闭测试 Zap 核心。
func (this *testZapRuntimeProvider) Close() error {
	this.closeOnce.Do(func() { this.closeErr = this.core.Close() })
	return this.closeErr
}

// testPlainProvider 只满足根日志合同，用于验证缺少 ZapProvider 能力时的回滚。
type testPlainProvider struct {
	// factory 是无需外部资源的 fallback LoggerFactory。
	factory baselog.LoggerFactory
	// closeCount 记录包装器是否回收 delegate 已交付资源。
	closeCount int
}

// Factory 返回 fallback LoggerFactory。
func (this *testPlainProvider) Factory() baselog.LoggerFactory { return this.factory }

// Close 记录一次资源回收。
func (this *testPlainProvider) Close() error {
	this.closeCount++
	return nil
}

// newTestLogging 创建具有 ZapProvider 窄能力的测试 Logging。
// 阶段角色：每个测试拥有独立文件输出，避免共享底层资源干扰 hlog Lease 断言。
func newTestLogging(t *testing.T) *testLogging {
	t.Helper()
	// 为每次 Build 准备独立 Zap 核心，隔离并行测试的输出与关闭状态。
	core, err := logzap.NewProvider(logzap.Config{
		Level:  "info",
		Mode:   logzap.ModeFile,
		Format: logzap.FormatJSON,
		Rotate: logzap.RotateConfig{Filename: filepath.Join(t.TempDir(), "runtime.log")},
	})
	if err != nil {
		t.Fatalf("build test zap provider: %v", err)
	}
	// Cleanup 只兜底测试提前退出；正常路径仍由包装器接管并关闭资源。
	provider := &testZapRuntimeProvider{core: core}
	t.Cleanup(func() { _ = provider.Close() })
	return &testLogging{provider: provider}
}
