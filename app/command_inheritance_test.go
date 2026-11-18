package app

import (
	"context"
	"slices"
	"testing"
	"time"
)

// inheritanceTestWorkload 是继承矩阵使用的命名 Workload。
type inheritanceTestWorkload struct {
	// name 是断言 Workload 局部作用域的稳定名称。
	name string
}

// Name 返回测试 Workload 名称。
func (this *inheritanceTestWorkload) Name() string { return this.name }

// Run 立即完成，使测试进入统一关闭流程。
func (this *inheritanceTestWorkload) Run(context.Context, Runtime, CommandContext) error { return nil }

// Shutdown 表示测试 Workload 已完成，无额外关闭动作。
func (this *inheritanceTestWorkload) Shutdown(context.Context, Runtime, CommandContext) error {
	return nil
}

// TestExecutionPlanInheritanceMatrix 验证 root、一级子命令和多级子命令使用字段级归并规则。
//
// 测试方法：构造三层局部声明，逐层冻结 executionPlan；断言 Runtime/预算就近覆盖，
// Before 按路径追加，Shutdown Hook 支持清空继承，而 FlagBinder/Workload 始终只取选中节点。
func TestExecutionPlanInheritanceMatrix(t *testing.T) {
	rootBinder := &commandCountingBinder{}
	levelOneBinder := &commandCountingBinder{}
	levelTwoBinder := &commandCountingBinder{}
	root := newCommandConfiguration("root", RuntimeModeEnabled)
	root.FlagBinder = rootBinder
	root.BeforeHooks = []BeforeRunHook{func(context.Context, Runtime, CommandContext) error { return nil }}
	root.Workloads = []Workload{&inheritanceTestWorkload{name: "root"}}
	root.ShutdownHooks = []namedShutdownHook{{Name: "root"}}
	root.ShutdownBudget = shutdownBudgetDeclaration{
		WorkloadGracefulTimeout:    time.Second,
		RuntimeCloseTimeout:        2 * time.Second,
		WorkloadGracefulTimeoutSet: true,
		RuntimeCloseTimeoutSet:     true,
	}
	levelOne := newCommandConfiguration("level-one", RuntimeModeDisabled)
	levelOne.FlagBinder = levelOneBinder
	levelOne.BeforeHooks = []BeforeRunHook{func(context.Context, Runtime, CommandContext) error { return nil }}
	levelOne.Workloads = []Workload{&inheritanceTestWorkload{name: "level-one"}}
	levelOne.ShutdownHooks = []namedShutdownHook{{Name: "level-one"}}
	levelOne.ShutdownBudget.WorkloadGracefulTimeout = 3 * time.Second
	levelOne.ShutdownBudget.WorkloadGracefulTimeoutSet = true
	levelTwo := newCommandConfiguration("level-two", runtimeModeUnspecified)
	levelTwo.FlagBinder = levelTwoBinder
	levelTwo.BeforeHooks = []BeforeRunHook{func(context.Context, Runtime, CommandContext) error { return nil }}
	levelTwo.Workloads = []Workload{&inheritanceTestWorkload{name: "level-two"}}
	levelTwo.DisableInheritedShutdownHooks = true
	levelTwo.ShutdownHooks = []namedShutdownHook{{Name: "level-two"}}
	levelTwo.ShutdownBudget.RuntimeCloseTimeout = 4 * time.Second
	levelTwo.ShutdownBudget.RuntimeCloseTimeoutSet = true

	tests := []struct {
		name           string
		path           []commandConfiguration
		runtimeMode    RuntimeMode
		binder         FlagBinder
		workloadName   string
		beforeCount    int
		shutdownNames  []string
		workloadBudget time.Duration
		runtimeBudget  time.Duration
	}{
		{"root", []commandConfiguration{root}, RuntimeModeEnabled, rootBinder, "root", 1, []string{"root"}, time.Second, 2 * time.Second},
		{"level-one", []commandConfiguration{root, levelOne}, RuntimeModeDisabled, levelOneBinder, "level-one", 2, []string{"root", "level-one"}, 3 * time.Second, 2 * time.Second},
		{"level-two", []commandConfiguration{root, levelOne, levelTwo}, RuntimeModeDisabled, levelTwoBinder, "level-two", 3, []string{"level-two"}, 3 * time.Second, 4 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := buildExecutionPlan(test.path)
			shutdownNames := make([]string, 0, len(plan.ShutdownHooks))
			for _, hook := range plan.ShutdownHooks {
				shutdownNames = append(shutdownNames, hook.Name)
			}
			if plan.RuntimeMode != test.runtimeMode || plan.FlagBinder != test.binder ||
				len(plan.Workloads) != 1 || plan.Workloads[0].Name() != test.workloadName ||
				len(plan.BeforeHooks) != test.beforeCount || !slices.Equal(shutdownNames, test.shutdownNames) ||
				plan.ShutdownBudget.WorkloadGracefulTimeout != test.workloadBudget ||
				plan.ShutdownBudget.RuntimeCloseTimeout != test.runtimeBudget {
				t.Fatalf("unexpected plan: %+v shutdown=%v", plan, shutdownNames)
			}
		})
	}
}

// TestNestedCommandUsesUnifiedWorkloadPath 验证多级命令可递归构建并通过 Workload 接收完整 CommandContext。
//
// 测试方法：声明 root→admin→status 三层命令，仅叶子声明 Runner Workload；执行带参数的叶子，
// 断言命令名称、完整路径和参数均由统一执行路径传入。
func TestNestedCommandUsesUnifiedWorkloadPath(t *testing.T) {
	var observedName string
	var observedPath string
	var observedArgs []string
	application := New(
		WithName("tool"),
		WithCommands(NewCommand("admin",
			WithCommands(NewCommand("status [service]",
				WithRuntimeMode(RuntimeModeDisabled),
				WithWorkloads(Runner(func(_ context.Context, _ Runtime, command CommandContext) error {
					observedName = command.Name()
					observedPath = command.Path()
					observedArgs = command.Args()
					return nil
				})),
			)),
		)),
	)
	if err := application.ExecuteContext(context.Background(), []string{"admin", "status", "payment"}); err != nil {
		t.Fatalf("execute nested command failed: %v", err)
	}
	if observedName != "status" || observedPath != "tool admin status" || !slices.Equal(observedArgs, []string{"payment"}) {
		t.Fatalf("unexpected nested context: name=%s path=%s args=%v", observedName, observedPath, observedArgs)
	}
}
