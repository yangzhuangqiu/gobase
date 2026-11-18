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

// TestShutdownBudgetBuildsFieldOverrides 验证命令预算通过 CommandOption 建立字段级覆盖并拒绝歧义声明。
//
// 测试方法：先声明两个合法字段并检查 set 标记，再分别构造负值和同字段重复声明，
// 断言错误在 NewCommand 构建期记录且包含稳定 reason。
func TestShutdownBudgetBuildsFieldOverrides(t *testing.T) {
	command := NewCommand("import", WithShutdownBudget(ShutdownBudget{
		WorkloadGracefulTimeout: 30 * time.Second,
		RuntimeCloseTimeout:     8 * time.Second,
	}))
	budget := command.configuration.ShutdownBudget
	if !budget.WorkloadGracefulTimeoutSet || !budget.RuntimeCloseTimeoutSet ||
		budget.WorkloadGracefulTimeout != 30*time.Second || budget.RuntimeCloseTimeout != 8*time.Second {
		t.Fatalf("unexpected command budget: %+v", budget)
	}

	tests := []struct {
		name    string
		command Command
		reason  string
	}{
		{
			name:    "negative workload",
			command: NewCommand("negative", WithShutdownBudget(ShutdownBudget{WorkloadGracefulTimeout: -time.Second})),
			reason:  "workload_graceful_timeout_negative",
		},
		{
			name: "duplicate runtime",
			command: NewCommand("duplicate",
				WithShutdownBudget(ShutdownBudget{RuntimeCloseTimeout: time.Second}),
				WithShutdownBudget(ShutdownBudget{RuntimeCloseTimeout: 2 * time.Second}),
			),
			reason: "runtime_close_timeout_duplicate_declaration",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.command.configuration.InputErr
			if err == nil || !strings.Contains(err.Error(), test.reason) {
				t.Fatalf("expected budget reason %q, got=%v", test.reason, err)
			}
		})
	}
}
