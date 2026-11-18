package app

import (
	"context"
	"strings"
	"testing"
)

// TestOptionsHelpers 验证简单 Option setter 与生命周期 hooks 注册语义。
//
// 测试方法：
// 1. 通过 WithDescription/WithVersion/WithBeforeRunHook/WithShutdownHook 构造应用；
// 2. 断言字段与 hooks 数量正确；
func TestOptionsHelpers(t *testing.T) {
	before := func(ctx context.Context, runtime Runtime, command CommandContext) error { return nil }
	shutdown := func(ctx context.Context, runtime Runtime, command CommandContext) error { return nil }
	application := New(
		WithDescription("demo desc"),
		WithVersion("v1.0.0"),
		WithBeforeRunHook(before),
		WithShutdownHook("server", shutdown),
	)
	if application.rootCommand.Short != "demo desc" || application.version != "v1.0.0" {
		t.Fatalf("unexpected description/version: %+v", application)
	}
	if len(application.rootCommand.BeforeHooks) != 1 || len(application.rootCommand.ShutdownHooks) != 1 {
		t.Fatalf("unexpected hooks count: before=%d shutdown=%d", len(application.rootCommand.BeforeHooks), len(application.rootCommand.ShutdownHooks))
	}
}

// TestWithRuntimeModeRejectsUnspecifiedValue 验证内部继承状态不会泄漏成用户可选模式。
//
// 测试方法：显式传入 RuntimeMode 零值构建应用，断言声明阶段记录 unsupported_mode；
// Enabled/Disabled 仍由其他命令执行测试覆盖其正常语义。
func TestWithRuntimeModeRejectsUnspecifiedValue(t *testing.T) {
	application := New(WithRuntimeMode(RuntimeMode(0)))
	if application.inputErr == nil || !strings.Contains(application.inputErr.Error(), "reason=unsupported_mode") {
		t.Fatalf("expected unspecified runtime mode to be rejected, got=%v", application.inputErr)
	}
}
