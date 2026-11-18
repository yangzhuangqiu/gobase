package app

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TestShutdownPolicyRejectsInvalidDeclarations 验证统一 Policy 独立承担旧分散入口的全部输入校验。
//
// 测试方法：分别声明空/含 nil 的信号、负预算和重复信号字段，断言构建期错误保留稳定 reason。
func TestShutdownPolicyRejectsInvalidDeclarations(t *testing.T) {
	// 第一阶段：用独立 App 隔离每种输入，避免前一个声明错误影响后续 reason 断言。
	cases := []struct {
		name   string
		app    *App
		reason string
	}{
		{name: "empty signals", app: New(WithShutdownPolicy(ShutdownPolicy{Signals: []os.Signal{}})), reason: "signals_empty"},
		{name: "nil signal", app: New(WithShutdownPolicy(ShutdownPolicy{Signals: []os.Signal{nil}})), reason: "signal_is_nil"},
		{name: "negative workload timeout", app: New(WithShutdownPolicy(ShutdownPolicy{WorkloadGracefulTimeout: -time.Second})), reason: "workload_graceful_timeout_negative"},
		{name: "negative runtime timeout", app: New(WithShutdownPolicy(ShutdownPolicy{RuntimeCloseTimeout: -time.Second})), reason: "runtime_close_timeout_negative"},
		{
			name: "duplicate signals",
			app: New(
				WithShutdownPolicy(ShutdownPolicy{Signals: []os.Signal{os.Interrupt}}),
				WithShutdownPolicy(ShutdownPolicy{Signals: []os.Signal{os.Interrupt}}),
			),
			reason: "signals_duplicate_declaration",
		},
	}

	// 第二阶段：逐案验证错误在执行前可检索，不依赖底层 signal.Notify 或 Runtime Build。
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if item.app.inputErr == nil || !strings.Contains(item.app.inputErr.Error(), item.reason) {
				t.Fatalf("expected shutdown policy reason %q, got=%v", item.reason, item.app.inputErr)
			}
		})
	}
}
