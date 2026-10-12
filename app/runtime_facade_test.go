package app

import (
	"context"
	"testing"

	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
)

// TestUseCaseNilRuntimeDeclarationFailsFast 验证显式 nil Runtime 不会静默回退默认蓝图。
//
// 测试方法：
// 1. 使用 WithRuntime(nil) 构建 App；
// 2. 提供合法 runner 并执行 root 命令；
// 3. 断言在 App setup 校验阶段返回错误。
func TestUseCaseNilRuntimeDeclarationFailsFast(t *testing.T) {
	application := New(
		WithRuntime(nil),
		WithRunner(func(context.Context, Runtime) error { return nil }),
	)
	if err := application.ExecuteContext(context.Background(), nil); err == nil {
		t.Fatalf("nil runtime declaration must fail")
	}
}

// TestUseCaseRuntimeApplicationNameOverrideWins 验证高级 Runtime 应用名可覆盖 CLI 默认传播值。
//
// 测试方法：同时设置 app.WithName 与 app-runtime.WithName，断言最终蓝图使用显式 Runtime identity。
func TestUseCaseRuntimeApplicationNameOverrideWins(t *testing.T) {
	application := New(
		WithName("orderctl"),
		WithRuntimeOptions(appruntime.WithName("order-service")),
	)
	if application.runtimeApplication.Info().Name != "order-service" {
		t.Fatalf("runtime application name override failed: %+v", application.runtimeApplication.Info())
	}
}
