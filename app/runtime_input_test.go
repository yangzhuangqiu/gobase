package app

import (
	"context"
	"errors"
	"testing"

	"github.com/yangzhuangqiu/gobase/config"
)

// TestUseCaseCLIInputFlowsToRuntime 验证 app 只解析 flags，并把 pinned 输入交给 app-runtime。
//
// 测试方法：
// 1. Runtime 提供默认 service.name；
// 2. 通过 app ConfigString flag 显式覆盖；
// 3. runner 从 Runtime ConfigManager 解码并断言 CLI 值生效。
func TestUseCaseCLIInputFlowsToRuntime(t *testing.T) {
	defaultConfig := commandRuntimeConfig{}
	defaultConfig.Service.Name = "default"
	flags := &commandRuntimeFlags{}
	application := New(
		WithConfig(config.File(config.FileDisabled()), config.Defaults(defaultConfig)),
		WithFlagBinder(flags),
		WithRunner(func(_ context.Context, runtime Runtime) error {
			resolved := &commandRuntimeConfig{}
			if err := runtime.ConfigManager().Current().Decode("", resolved); err != nil {
				return err
			}
			if resolved.Service.Name != "cli" {
				return errors.New("pinned value did not reach runtime")
			}
			return nil
		}),
	)
	if err := application.ExecuteContext(context.Background(), []string{"--service.name", "cli"}); err != nil {
		t.Fatalf("execute with pinned input failed: %v", err)
	}
}
