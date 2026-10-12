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
		WithRunner(func(_ context.Context, runtime Runtime) error {
			if runtime == nil {
				return errors.New("runtime is nil in runner")
			}
			trace = append(trace, "run")
			return nil
		}),
		WithShutdown(WithShutdownHook("server", func(_ context.Context, runtime Runtime, _ CommandContext) error {
			if runtime == nil {
				return errors.New("runtime is nil in shutdown hook")
			}
			trace = append(trace, "shutdown")
			return nil
		})),
	)
	if err := application.ExecuteContext(context.Background(), nil); err != nil {
		t.Fatalf("execute app failed: %v", err)
	}
	expect := []string{"init", "before", "run", "shutdown", "close"}
	if !slices.Equal(trace, expect) {
		t.Fatalf("unexpected lifecycle order: expect=%v actual=%v", expect, trace)
	}
}

// TestUseCaseRuntimeClosesWhenBeforeHookFails 验证 before hook 失败仍会收敛 Runtime。
//
// 测试方法：
// 1. 注册可观察 Close 的 bean；
// 2. before hook 返回错误，runner 不应执行；
// 3. 断言错误返回且 Close 仍发生。
func TestUseCaseRuntimeClosesWhenBeforeHookFails(t *testing.T) {
	trace := make([]string, 0, 3)
	runtimeApplication := appruntime.New(commandRuntimeTraceOptions(&trace)...)
	application := New(
		WithRuntime(runtimeApplication),
		WithBeforeRunHook(func(context.Context, Runtime, CommandContext) error {
			trace = append(trace, "before")
			return errors.New("before failed")
		}),
		WithRunner(func(context.Context, Runtime) error {
			trace = append(trace, "run")
			return nil
		}),
		WithShutdown(WithShutdownHook("server", func(context.Context, Runtime, CommandContext) error {
			trace = append(trace, "shutdown")
			return nil
		})),
	)
	err := application.ExecuteContext(context.Background(), nil)
	if err == nil {
		t.Fatalf("execute should fail")
	}
	if !slices.Equal(trace, []string{"init", "before", "close"}) {
		t.Fatalf("unexpected failure lifecycle: %v", trace)
	}
}

// TestUseCaseDisabledRuntimeCommand 验证诊断子命令可跳过 app-runtime 构建。
//
// 测试方法：
// 1. 配置一个可观察 Runtime 蓝图；
// 2. 执行 RuntimeModeDisabled 子命令；
// 3. 断言 Before Hook 被跳过、Shutdown Hook 收到 nil Runtime，且 runner 成功。
func TestUseCaseDisabledRuntimeCommand(t *testing.T) {
	trace := make([]string, 0, 1)
	runtimeApplication := appruntime.New(commandRuntimeTraceOptions(&trace)...)
	called := false
	shutdownCalled := false
	application := New(
		WithRuntime(runtimeApplication),
		WithBeforeRunHook(func(context.Context, Runtime, CommandContext) error {
			return errors.New("disabled command must skip before hooks")
		}),
		WithShutdown(WithShutdownHook("diagnostic", func(_ context.Context, runtime Runtime, command CommandContext) error {
			shutdownCalled = true
			if runtime != nil || command.Name() != "doctor" {
				return errors.New("unexpected disabled shutdown context")
			}
			return nil
		})),
		WithCommands(Command{
			Use:         "doctor",
			RuntimeMode: RuntimeModeDisabled,
			Runner: func(_ context.Context, runtime Runtime, command CommandContext) error {
				called = true
				if runtime != nil || command.Name() != "doctor" {
					return errors.New("unexpected disabled runtime context")
				}
				return nil
			},
		}),
	)
	if err := application.ExecuteContext(context.Background(), []string{"doctor"}); err != nil {
		t.Fatalf("execute disabled runtime command failed: %v", err)
	}
	if !called {
		t.Fatalf("doctor runner was not called")
	}
	if !shutdownCalled {
		t.Fatalf("disabled runtime command must still run inherited shutdown hooks")
	}
	if len(trace) != 0 {
		t.Fatalf("disabled command must not build runtime, trace=%v", trace)
	}
}
