package apphlogzap

import (
	"context"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/yangzhuangqiu/gobase/errcode"
	hlogzap "github.com/yangzhuangqiu/gobase/hlog-zap"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

// TestWrapInstallsAndRestoresHlogWithProvider 验证 Runtime 日志资源统一管理 Factory、hlog 和 Provider 顺序。
// 测试方法：无 Manager 构建默认 Zap Provider，断言 hlog 被替换、Factory 可用，关闭后恢复且重复关闭幂等。
func TestWrapInstallsAndRestoresHlogWithProvider(t *testing.T) {
	previous := hlog.DefaultLogger()
	provider, err := Wrap(newTestLogging(t)).Build(context.Background(), nil)
	if err != nil {
		t.Fatalf("build app hlog zap: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	if provider.Factory() == nil || hlog.DefaultLogger() == previous {
		t.Fatal("expected factory and installed hlog")
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("close app hlog zap: %v", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("repeat close app hlog zap: %v", err)
	}
	if hlog.DefaultLogger() != previous {
		t.Fatal("expected previous hlog logger after close")
	}
}

// TestWrapRejectsSecondActiveRuntimeAndRollsBackProvider 验证全局 hlog 不允许两个并行 Runtime 竞争所有权。
// 测试方法：保持首个 Provider 活跃并构建第二个，断言 build.failed 包含 install conflict，随后关闭首个恢复全局状态。
func TestWrapRejectsSecondActiveRuntimeAndRollsBackProvider(t *testing.T) {
	first, err := Wrap(newTestLogging(t)).Build(context.Background(), nil)
	if err != nil {
		t.Fatalf("build first runtime logger: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := Wrap(newTestLogging(t)).Build(context.Background(), nil)
	if second != nil || !errcode.IsCode[ErrorCode](err, ErrCodeBuildFailed) || !errcode.IsCode[hlogzap.ErrorCode](err, hlogzap.ErrCodeInstallConflict) {
		t.Fatalf("expected second runtime build failure: provider=%v err=%v", second, err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first runtime logger: %v", err)
	}
}

// TestWrapRejectsProviderWithoutZapCapability 验证包装器不会把根日志 Provider 猜测为 Zap Provider。
// 测试方法：delegate 返回只满足根合同的 Provider，断言 Build 失败、资源被关闭且全局 hlog 不变。
func TestWrapRejectsProviderWithoutZapCapability(t *testing.T) {
	previous := hlog.DefaultLogger()
	plain := &testPlainProvider{factory: baselog.NewLoggerFactory(nil)}
	provider, err := Wrap(&testLogging{provider: plain}).Build(context.Background(), nil)
	if provider != nil || !errcode.IsCode[ErrorCode](err, ErrCodeBuildFailed) {
		t.Fatalf("expected zap capability failure: provider=%v err=%v", provider, err)
	}
	if plain.closeCount != 1 || hlog.DefaultLogger() != previous {
		t.Fatalf("unexpected rollback: close_count=%d hlog_changed=%t", plain.closeCount, hlog.DefaultLogger() != previous)
	}
}

// TestWrapRejectsNilLogging 验证缺少 delegate 时在 Runtime Build 边界返回稳定错误。
// 测试方法：包装 nil Logging 并执行 Build，断言没有 Provider 且命中 build_failed。
func TestWrapRejectsNilLogging(t *testing.T) {
	provider, err := Wrap(nil).Build(context.Background(), nil)
	if provider != nil || !errcode.IsCode[ErrorCode](err, ErrCodeBuildFailed) {
		t.Fatalf("expected nil logging failure: provider=%v err=%v", provider, err)
	}
}
