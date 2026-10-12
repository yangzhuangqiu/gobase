package app

import (
	"context"
	"os"
	"testing"
)

// TestUseCaseAppRunAndAddCommand 验证 App.Run 与 AddCommand 的主流程语义。
//
// 测试方法：
// 1. 追加一个可执行子命令并设置 os.Args 触发该子命令；
// 2. 调用 Run 并断言返回成功且子命令业务被执行。
func TestUseCaseAppRunAndAddCommand(t *testing.T) {
	originArgs := os.Args
	t.Cleanup(func() { os.Args = originArgs })

	runCalled := false
	application := New(WithName("demo"))
	application.AddCommand(Command{
		Use:   "doctor",
		Short: "doctor command",
		Runner: func(ctx context.Context, runtime Runtime, command CommandContext) error {
			runCalled = true
			return nil
		},
	})
	os.Args = []string{"demo", "doctor"}

	if err := application.Run(); err != nil {
		t.Fatalf("run app failed: %v", err)
	}
	if !runCalled {
		t.Fatal("expected sub command run to be called")
	}
}
