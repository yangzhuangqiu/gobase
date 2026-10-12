package app

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
)

// TestUseCasePublicOutputWithoutCobraCommand 验证调用方无需获取 Cobra Command 即可控制输出。
//
// 测试方法：
// 1. 通过 WithOutput 注入缓冲区；
// 2. ExecuteContext 执行 --version；
// 3. 断言版本文本写入公共输出契约。
func TestUseCasePublicOutputWithoutCobraCommand(t *testing.T) {
	var output bytes.Buffer
	application := New(WithVersion("v2.0.0"), WithOutput(&output))
	if err := application.ExecuteContext(context.Background(), []string{"--version"}); err != nil {
		t.Fatalf("execute version failed: %v", err)
	}
	if strings.TrimSpace(output.String()) != "v2.0.0" {
		t.Fatalf("unexpected version output: %q", output.String())
	}
}

// TestUseCaseSubCommandVersionSkipsRuntimeAndRunner 验证子命令 --version 与 root 保持相同的终止语义。
//
// 测试方法：
// 1. 为应用配置可观察 Runtime、hooks 和子命令 Runner；
// 2. 执行 `serve --version`；
// 3. 断言只输出版本，Runtime、hooks 和 Runner 均未执行。
func TestUseCaseSubCommandVersionSkipsRuntimeAndRunner(t *testing.T) {
	var output bytes.Buffer
	trace := make([]string, 0, 1)
	hookCalled := false
	runnerCalled := false

	// 构造 RuntimeModeEnabled 子命令，确保短路不是 Disabled 模式带来的结果。
	application := New(
		WithVersion("v3.0.0"),
		WithOutput(&output),
		WithRuntime(appruntime.New(commandRuntimeTraceOptions(&trace)...)),
		WithBeforeRunHook(func(context.Context, Runtime, CommandContext) error {
			hookCalled = true
			return nil
		}),
		WithShutdown(WithShutdownHook("server", func(context.Context, Runtime, CommandContext) error {
			hookCalled = true
			return nil
		})),
		WithCommands(Command{
			Use:         "serve",
			RuntimeMode: RuntimeModeEnabled,
			Runner: func(context.Context, Runtime, CommandContext) error {
				runnerCalled = true
				return nil
			},
		}),
	)

	// version 是命令级终止条件，不允许进入任何运行时生命周期阶段。
	if err := application.ExecuteContext(context.Background(), []string{"serve", "--version"}); err != nil {
		t.Fatalf("execute subcommand version failed: %v", err)
	}
	if strings.TrimSpace(output.String()) != "v3.0.0" {
		t.Fatalf("unexpected version output: %q", output.String())
	}
	if len(trace) != 0 || hookCalled || runnerCalled {
		t.Fatalf("version must skip runtime/hooks/runner trace=%v hook=%t runner=%t", trace, hookCalled, runnerCalled)
	}
}

// TestUseCaseArgumentValidatorHidesCobra 验证公共参数校验器仅接收参数快照。
//
// 测试方法：
// 1. 子命令声明 ArgumentValidator 并要求一个参数；
// 2. 传入空参数执行；
// 3. 断言校验错误原样进入命令错误链。
func TestUseCaseArgumentValidatorHidesCobra(t *testing.T) {
	application := New(WithCommands(Command{
		Use:         "inspect",
		RuntimeMode: RuntimeModeDisabled,
		ValidateArgs: func(args []string) error {
			if len(args) != 1 {
				return errors.New("one argument required")
			}
			return nil
		},
		Runner: func(context.Context, Runtime, CommandContext) error { return nil },
	}))
	err := application.ExecuteContext(context.Background(), []string{"inspect"})
	if err == nil || !strings.Contains(err.Error(), "one argument required") {
		t.Fatalf("expect argument validation error, got=%v", err)
	}
}
