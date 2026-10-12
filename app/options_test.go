package app

import (
	"context"
	"testing"
)

// TestOptionsHelpers 验证简单 Option setter 与生命周期 hooks 注册语义。
//
// 测试方法：
// 1. 通过 WithDescription/WithVersion/WithBeforeRunHook/WithShutdown 构造应用；
// 2. 断言字段与 hooks 数量正确；
func TestOptionsHelpers(t *testing.T) {
	before := func(ctx context.Context, runtime Runtime, command CommandContext) error { return nil }
	shutdown := func(ctx context.Context, runtime Runtime, command CommandContext) error { return nil }
	application := New(
		WithDescription("demo desc"),
		WithVersion("v1.0.0"),
		WithBeforeRunHook(before),
		WithShutdown(WithShutdownHook("server", shutdown)),
	)
	if application.description != "demo desc" || application.version != "v1.0.0" {
		t.Fatalf("unexpected description/version: %+v", application)
	}
	if len(application.beforeHooks) != 1 || len(application.shutdown.Hooks) != 1 {
		t.Fatalf("unexpected hooks count: before=%d shutdown=%d", len(application.beforeHooks), len(application.shutdown.Hooks))
	}
}
