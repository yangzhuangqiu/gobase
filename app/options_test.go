package app

import (
	"context"
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

// TestRuntimeRequirementOptionsKeepTriStateInternal 验证公共语义 Option 能禁用并在子树中恢复 Runtime。
//
// 测试方法：root 使用 WithoutRuntime，子命令使用 RequireRuntime；冻结两层计划并断言
// root 为 disabled、子命令恢复 required，调用方无需接触内部继承状态。
func TestRuntimeRequirementOptionsKeepTriStateInternal(t *testing.T) {
	application := New(
		WithoutRuntime(),
		WithCommands(NewCommand("repair", RequireRuntime())),
	)
	rootPlan := buildExecutionPlan([]commandConfiguration{application.rootCommand})
	child := application.rootCommand.Commands[0].configuration
	childPlan := buildExecutionPlan([]commandConfiguration{application.rootCommand, child})
	if rootPlan.RuntimeRequirement != runtimeRequirementDisabled {
		t.Fatalf("expected root without runtime, got=%d", rootPlan.RuntimeRequirement)
	}
	if childPlan.RuntimeRequirement != runtimeRequirementRequired {
		t.Fatalf("expected child to require runtime, got=%d", childPlan.RuntimeRequirement)
	}
}
