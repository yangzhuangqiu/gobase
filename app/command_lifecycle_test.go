package app

import (
	"context"
	"errors"
	"slices"
	"testing"

	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
)

// TestUseCaseRuntimeLivesThroughShutdownHooks 验证 Runtime 在 Shutdown Hooks 完全返回后才关闭。
//
// 测试方法：
// 1. Runtime Config Provider 提供可观察生命周期资源；
// 2. before、runner、shutdown 分别记录动作并检查 Runtime；
// 3. ExecuteContext 后断言关闭发生在 shutdown 之后。
func TestUseCaseRuntimeLivesThroughShutdownHooks(t *testing.T) {
	trace := make([]string, 0, 5)
	runtimeOptions := []appruntime.Option{appruntime.WithName("command-runtime")}
	runtimeOptions = append(runtimeOptions, commandRuntimeTraceOptions(&trace)...)
	runtimeApplication := appruntime.New(runtimeOptions...)
	application := New(
		WithRuntime(runtimeApplication),
		WithBeforeRunHook(func(_ context.Context, runtime Runtime, _ CommandContext) error {
			if runtime == nil {
				return errors.New("runtime is nil in before hook")
			}
			trace = append(trace, "before")
			return nil
		}),
		WithModules(Runner(func(_ context.Context, runtime Runtime, _ CommandContext) error {
			if runtime == nil {
				return errors.New("runtime is nil in runner")
			}
			trace = append(trace, "run")
			return nil
		})),
		WithShutdownHook("server", func(_ context.Context, runtime Runtime, _ CommandContext) error {
			if runtime == nil {
				return errors.New("runtime is nil in shutdown hook")
			}
			trace = append(trace, "shutdown")
			return nil
		}),
	)
	if err := application.ExecuteContext(context.Background(), nil); err != nil {
		t.Fatalf("execute app failed: %v", err)
	}
	expect := []string{"init", "before", "run", "shutdown", "close"}
	if !slices.Equal(trace, expect) {
		t.Fatalf("unexpected lifecycle order: expect=%v actual=%v", expect, trace)
	}
}

// TestUseCaseRuntimeClosesWhenBeforeRunHookFails 验证 BeforeRunHook 失败仍会收敛 Runtime。
//
// 测试方法：
// 1. 注册可观察 Close 的 bean；
// 2. before hook 返回错误，runner 不应执行；
// 3. 断言错误返回且 Close 仍发生。
func TestUseCaseRuntimeClosesWhenBeforeRunHookFails(t *testing.T) {
	trace := make([]string, 0, 3)
	runtimeApplication := appruntime.New(commandRuntimeTraceOptions(&trace)...)
	application := New(
		WithRuntime(runtimeApplication),
		WithBeforeRunHook(func(context.Context, Runtime, CommandContext) error {
			trace = append(trace, "before")
			return errors.New("before failed")
		}),
		WithModules(Runner(func(context.Context, Runtime, CommandContext) error {
			trace = append(trace, "run")
			return nil
		})),
		WithShutdownHook("server", func(context.Context, Runtime, CommandContext) error {
			trace = append(trace, "shutdown")
			return nil
		}),
	)
	err := application.ExecuteContext(context.Background(), nil)
	if err == nil {
		t.Fatalf("execute should fail")
	}
	if !slices.Equal(trace, []string{"init", "before", "close"}) {
		t.Fatalf("unexpected failure lifecycle: %v", trace)
	}
}

// TestUseCaseWithoutRuntimeCommand 验证诊断子命令可声明不构建 app-runtime。
//
// 测试方法：
// 1. 配置一个可观察 Runtime 蓝图；
// 2. 执行 WithoutRuntime 子命令；
// 3. 断言全局与子命令 Before Hook 均收到 nil Runtime、root 局部 Hook 不泄漏，且 runner 成功。
func TestUseCaseWithoutRuntimeCommand(t *testing.T) {
	trace := make([]string, 0, 1)
	runtimeApplication := appruntime.New(commandRuntimeTraceOptions(&trace)...)
	called := false
	globalBeforeCalled := false
	rootBeforeCalled := false
	childBeforeCalled := false
	shutdownCalled := false
	application := New(
		WithRuntime(runtimeApplication),
		WithGlobalBeforeRunHook(func(_ context.Context, runtime Runtime, command CommandContext) error {
			globalBeforeCalled = true
			if runtime != nil || command.Name() != "doctor" {
				return errors.New("unexpected without-runtime global before context")
			}
			return nil
		}),
		WithBeforeRunHook(func(context.Context, Runtime, CommandContext) error {
			rootBeforeCalled = true
			return nil
		}),
		WithCommands(NewCommand("doctor",
			WithoutRuntime(),
			WithBeforeRunHook(func(_ context.Context, runtime Runtime, command CommandContext) error {
				childBeforeCalled = true
				if runtime != nil || command.Name() != "doctor" {
					return errors.New("unexpected without-runtime command before context")
				}
				return nil
			}),
			WithShutdownHook("diagnostic", func(_ context.Context, runtime Runtime, command CommandContext) error {
				shutdownCalled = true
				if runtime != nil || command.Name() != "doctor" {
					return errors.New("unexpected without-runtime shutdown context")
				}
				return nil
			}),
			WithModules(Runner(func(_ context.Context, runtime Runtime, command CommandContext) error {
				called = true
				if runtime != nil || command.Name() != "doctor" {
					return errors.New("unexpected without-runtime context")
				}
				return nil
			})),
		)),
	)
	if err := application.ExecuteContext(context.Background(), []string{"doctor"}); err != nil {
		t.Fatalf("execute without-runtime command failed: %v", err)
	}
	if !called || !globalBeforeCalled || !childBeforeCalled || rootBeforeCalled {
		t.Fatalf(
			"unexpected without-runtime lifecycle: runner=%t global_before=%t child_before=%t root_before=%t",
			called,
			globalBeforeCalled,
			childBeforeCalled,
			rootBeforeCalled,
		)
	}
	if !shutdownCalled {
		t.Fatalf("without-runtime command must still run its local shutdown hooks")
	}
	if len(trace) != 0 {
		t.Fatalf("without-runtime command must not build runtime, trace=%v", trace)
	}
}

// TestUseCaseRequireRuntimeRestoresDisabledSubtree 验证高级覆盖入口可在禁用子树中恢复 Runtime。
//
// 测试方法：父命令声明 WithoutRuntime，叶子声明 RequireRuntime 并执行；断言 Runtime 完成
// Build/Close、全局与叶子局部 Before Hook 被调用，且 Workload 收到非 nil Runtime。
func TestUseCaseRequireRuntimeRestoresDisabledSubtree(t *testing.T) {
	trace := make([]string, 0, 4)
	runtimeApplication := appruntime.New(commandRuntimeTraceOptions(&trace)...)
	application := New(
		WithRuntime(runtimeApplication),
		WithGlobalBeforeRunHook(func(_ context.Context, runtime Runtime, _ CommandContext) error {
			if runtime == nil {
				return errors.New("required runtime is nil in global before hook")
			}
			trace = append(trace, "global-before")
			return nil
		}),
		WithCommands(NewCommand("admin",
			WithoutRuntime(),
			WithBeforeRunHook(func(context.Context, Runtime, CommandContext) error {
				return errors.New("unselected parent before hook must not run")
			}),
			WithCommands(NewCommand("repair",
				RequireRuntime(),
				WithBeforeRunHook(func(_ context.Context, runtime Runtime, _ CommandContext) error {
					if runtime == nil {
						return errors.New("required runtime is nil in command before hook")
					}
					trace = append(trace, "command-before")
					return nil
				}),
				WithModules(Runner(func(_ context.Context, runtime Runtime, command CommandContext) error {
					if runtime == nil || command.Name() != "repair" {
						return errors.New("unexpected required-runtime context")
					}
					trace = append(trace, "run")
					return nil
				})),
			)),
		)),
	)
	if err := application.ExecuteContext(context.Background(), []string{"admin", "repair"}); err != nil {
		t.Fatalf("execute required-runtime override failed: %v", err)
	}
	if !slices.Equal(trace, []string{"init", "global-before", "command-before", "run", "close"}) {
		t.Fatalf("unexpected required-runtime lifecycle: %v", trace)
	}
}
