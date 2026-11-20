package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	"github.com/yangzhuangqiu/gobase/ioc"
)

// inheritanceTestWorkload 是 Module/Workload 隔离矩阵使用的命名 Workload。
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

// moduleIsolationProbe 同时记录 Module.Register 与其贡献 Workload.Run，用于验证命令节点隔离。
type moduleIsolationProbe struct {
	// name 是命令节点局部 Module 与 Workload 的稳定身份。
	name string
	// registerCount 记录该 Module 进入 Runtime Build 的次数。
	registerCount *int
	// runCount 记录该 Module 贡献 Workload 的执行次数。
	runCount *int
}

// definitionIsolationProbe 记录当前命令私有 Bean 是否进入本次 IOC Init。
type definitionIsolationProbe struct {
	// initCount 记录该节点 Definition 被初始化的次数。
	initCount *int
}

// Init 记录当前命令私有 Bean 的初始化事件。
func (this *definitionIsolationProbe) Init(context.Context) error {
	(*this.initCount)++
	return nil
}

// Name 返回隔离矩阵使用的稳定身份。
func (this *moduleIsolationProbe) Name() string { return this.name }

// Register 记录当前 Module 实际参与了选中命令的 Runtime Build。
func (this *moduleIsolationProbe) Register(ioc.Registry) error {
	(*this.registerCount)++
	return nil
}

// Workloads 返回由当前 Module 独占的测试执行单元。
func (this *moduleIsolationProbe) Workloads() []Workload { return []Workload{this} }

// Run 记录当前节点 Workload 已通过统一监督器执行。
func (this *moduleIsolationProbe) Run(context.Context, Runtime, CommandContext) error {
	(*this.runCount)++
	return nil
}

// Shutdown 表示立即完成的测试 Workload 无额外停止动作。
func (this *moduleIsolationProbe) Shutdown(context.Context, Runtime, CommandContext) error {
	return nil
}

// TestExecutionPlanInheritanceMatrix 验证 root、一级子命令和多级子命令使用字段级归并规则。
//
// 测试方法：构造三层局部声明，逐层冻结 executionPlan；断言 Runtime/预算就近覆盖，
// Before/FlagBinder/Bean/Module/Workload/Shutdown Hook 始终只取选中节点。
func TestExecutionPlanInheritanceMatrix(t *testing.T) {
	rootBinder := &commandCountingBinder{}
	levelOneBinder := &commandCountingBinder{}
	levelTwoBinder := &commandCountingBinder{}
	root := newCommandConfiguration("root", runtimeRequirementRequired)
	root.FlagBinder = rootBinder
	root.BeforeRunHooks = []BeforeRunHook{func(context.Context, Runtime, CommandContext) error { return nil }}
	root.Modules = []Module{newTestWorkloadModule("root-module", &inheritanceTestWorkload{name: "root"})}
	root.ShutdownHooks = []namedShutdownHook{{Name: "root"}}
	root.ShutdownBudget = shutdownBudgetDeclaration{
		WorkloadGracefulTimeout:    time.Second,
		RuntimeCloseTimeout:        2 * time.Second,
		WorkloadGracefulTimeoutSet: true,
		RuntimeCloseTimeoutSet:     true,
	}
	levelOne := newCommandConfiguration("level-one", runtimeRequirementDisabled)
	levelOne.FlagBinder = levelOneBinder
	levelOne.BeforeRunHooks = []BeforeRunHook{func(context.Context, Runtime, CommandContext) error { return nil }}
	levelOne.Modules = []Module{newTestWorkloadModule("level-one-module", &inheritanceTestWorkload{name: "level-one"})}
	levelOne.ShutdownHooks = []namedShutdownHook{{Name: "level-one"}}
	levelOne.ShutdownBudget.WorkloadGracefulTimeout = 3 * time.Second
	levelOne.ShutdownBudget.WorkloadGracefulTimeoutSet = true
	levelTwo := newCommandConfiguration("level-two", runtimeRequirementUnspecified)
	levelTwo.FlagBinder = levelTwoBinder
	levelTwo.BeforeRunHooks = []BeforeRunHook{func(context.Context, Runtime, CommandContext) error { return nil }}
	levelTwo.Modules = []Module{newTestWorkloadModule("level-two-module", &inheritanceTestWorkload{name: "level-two"})}
	levelTwo.ShutdownHooks = []namedShutdownHook{{Name: "level-two"}}
	levelTwo.ShutdownBudget.RuntimeCloseTimeout = 4 * time.Second
	levelTwo.ShutdownBudget.RuntimeCloseTimeoutSet = true

	tests := []struct {
		name           string
		path           []commandConfiguration
		requirement    runtimeRequirement
		binder         FlagBinder
		moduleName     string
		workloadName   string
		beforeCount    int
		shutdownNames  []string
		workloadBudget time.Duration
		runtimeBudget  time.Duration
	}{
		{"root", []commandConfiguration{root}, runtimeRequirementRequired, rootBinder, "root-module", "root", 1, []string{"root"}, time.Second, 2 * time.Second},
		{"level-one", []commandConfiguration{root, levelOne}, runtimeRequirementDisabled, levelOneBinder, "level-one-module", "level-one", 1, []string{"level-one"}, 3 * time.Second, 2 * time.Second},
		{"level-two", []commandConfiguration{root, levelOne, levelTwo}, runtimeRequirementDisabled, levelTwoBinder, "level-two-module", "level-two", 1, []string{"level-two"}, 3 * time.Second, 4 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := buildExecutionPlan(test.path)
			workloads, err := resolveModuleWorkloads(plan.Modules)
			if err != nil {
				t.Fatalf("resolve module workloads: %v", err)
			}
			shutdownNames := make([]string, 0, len(plan.ShutdownHooks))
			for _, hook := range plan.ShutdownHooks {
				shutdownNames = append(shutdownNames, hook.Name)
			}
			if plan.RuntimeRequirement != test.requirement || plan.FlagBinder != test.binder ||
				len(plan.Modules) != 1 || plan.Modules[0].Name() != test.moduleName ||
				len(workloads) != 1 || workloads[0].Name() != test.workloadName ||
				len(plan.BeforeRunHooks) != test.beforeCount || !slices.Equal(shutdownNames, test.shutdownNames) ||
				plan.ShutdownBudget.WorkloadGracefulTimeout != test.workloadBudget ||
				plan.ShutdownBudget.RuntimeCloseTimeout != test.runtimeBudget {
				t.Fatalf("unexpected plan: %+v shutdown=%v", plan, shutdownNames)
			}
		})
	}
}

// TestBeforeRunHookScopeMatrix 验证全局运行前 Hook 覆盖所有实际命令，而节点 Hook 只属于选中节点。
//
// 测试方法：root、一级和二级节点分别声明局部 Hook 与 Runner，并共享一个全局 Hook；
// 复用同一 App 顺序执行三个节点，逐次断言 global→selected before→selected workload 的固定顺序。
func TestBeforeRunHookScopeMatrix(t *testing.T) {
	trace := make([]string, 0, 3)
	recordBefore := func(label string) BeforeRunHook {
		return func(_ context.Context, runtime Runtime, command CommandContext) error {
			if runtime != nil {
				return errors.New("without-runtime before hook received runtime")
			}
			trace = append(trace, label+":"+command.Name())
			return nil
		}
	}
	recordRun := func(label string) Module {
		return Runner(func(_ context.Context, runtime Runtime, command CommandContext) error {
			if runtime != nil {
				return errors.New("without-runtime workload received runtime")
			}
			trace = append(trace, "run:"+label)
			return nil
		})
	}

	// 第一阶段：三层节点使用相同语义声明局部能力，全局 Hook 只在 App 上声明一次。
	application := New(
		WithName("tool"),
		WithoutRuntime(),
		WithGlobalBeforeRunHook(recordBefore("global")),
		WithBeforeRunHook(recordBefore("root")),
		WithModules(recordRun("root")),
		WithCommands(NewCommand("level-one",
			WithBeforeRunHook(recordBefore("level-one")),
			WithModules(recordRun("level-one")),
			WithCommands(NewCommand("level-two",
				WithBeforeRunHook(recordBefore("level-two")),
				WithModules(recordRun("level-two")),
			)),
		)),
	)

	// 第二阶段：每次执行后重置轨迹，避免累计结果掩盖节点间 Hook 泄漏。
	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{name: "root", expected: []string{"global:tool", "root:tool", "run:root"}},
		{name: "level-one", args: []string{"level-one"}, expected: []string{"global:level-one", "level-one:level-one", "run:level-one"}},
		{name: "level-two", args: []string{"level-one", "level-two"}, expected: []string{"global:level-two", "level-two:level-two", "run:level-two"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			trace = trace[:0]
			if err := application.ExecuteContext(context.Background(), test.args); err != nil {
				t.Fatalf("execute selected command: %v", err)
			}
			if !slices.Equal(trace, test.expected) {
				t.Fatalf("unexpected before hook scope: expect=%v actual=%v", test.expected, trace)
			}
		})
	}
}

// TestCommandBeanModuleWorkloadIsolationMatrix 验证 root、一级和多级子命令只构建选中节点对象图。
//
// 测试方法：三层节点各声明独立 Definition 与 WorkloadModule，复用同一 App 依次执行三层命令；
// 每次执行后断言仅目标节点的 Bean Init、Module Register 和 Workload Run 增加一次。
func TestCommandBeanModuleWorkloadIsolationMatrix(t *testing.T) {
	// 第一阶段：为三层节点建立彼此独立的 Bean、Module 与 Workload 计数器。
	rootBeanInit, levelOneBeanInit, levelTwoBeanInit := 0, 0, 0
	rootRegister, rootRun := 0, 0
	levelOneRegister, levelOneRun := 0, 0
	levelTwoRegister, levelTwoRun := 0, 0
	// 三层故意复用同一身份，证明名称只在选中节点内部校验，不与祖先形成隐式合并。
	rootModule := &moduleIsolationProbe{name: "selected-module", registerCount: &rootRegister, runCount: &rootRun}
	levelOneModule := &moduleIsolationProbe{name: "selected-module", registerCount: &levelOneRegister, runCount: &levelOneRun}
	levelTwoModule := &moduleIsolationProbe{name: "selected-module", registerCount: &levelTwoRegister, runCount: &levelTwoRun}
	application := New(
		WithName("tool"),
		WithBeanDefinitions(ioc.Define(&definitionIsolationProbe{initCount: &rootBeanInit})),
		WithModules(rootModule),
		WithCommands(NewCommand("level-one",
			WithBeanDefinitions(ioc.Define(&definitionIsolationProbe{initCount: &levelOneBeanInit})),
			WithModules(levelOneModule),
			WithCommands(NewCommand("level-two",
				WithBeanDefinitions(ioc.Define(&definitionIsolationProbe{initCount: &levelTwoBeanInit})),
				WithModules(levelTwoModule),
			)),
		)),
	)

	// 第二阶段：顺序复用同一 App，每次只允许选中节点对应的三类计数增长。
	tests := []struct {
		name             string
		args             []string
		expectedBeanInit []int
		expectedRegister []int
		expectedRun      []int
	}{
		{name: "root", expectedBeanInit: []int{1, 0, 0}, expectedRegister: []int{1, 0, 0}, expectedRun: []int{1, 0, 0}},
		{name: "level-one", args: []string{"level-one"}, expectedBeanInit: []int{1, 1, 0}, expectedRegister: []int{1, 1, 0}, expectedRun: []int{1, 1, 0}},
		{name: "level-two", args: []string{"level-one", "level-two"}, expectedBeanInit: []int{1, 1, 1}, expectedRegister: []int{1, 1, 1}, expectedRun: []int{1, 1, 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := application.ExecuteContext(context.Background(), test.args); err != nil {
				t.Fatalf("execute selected command: %v", err)
			}
			beanInits := []int{rootBeanInit, levelOneBeanInit, levelTwoBeanInit}
			registers := []int{rootRegister, levelOneRegister, levelTwoRegister}
			runs := []int{rootRun, levelOneRun, levelTwoRun}
			if !slices.Equal(beanInits, test.expectedBeanInit) ||
				!slices.Equal(registers, test.expectedRegister) || !slices.Equal(runs, test.expectedRun) {
				t.Fatalf("unexpected isolation counters: beans=%v registers=%v runs=%v", beanInits, registers, runs)
			}
		})
	}
}

// TestRuntimeOptionBeanDefinitionsRemainGlobalAdvancedEscape 验证高级 Runtime 蓝图 Bean 对所有 Runtime 命令生效。
//
// 测试方法：通过 WithRuntimeOptions 声明全局计数 Bean，依次执行 root 与子命令；
// 断言 Bean 每次都初始化，而节点 Runner 保持局部，固定高级全局入口与常规节点入口的边界。
func TestRuntimeOptionBeanDefinitionsRemainGlobalAdvancedEscape(t *testing.T) {
	// 第一阶段：全局 Definition 只通过显式 Runtime 高级入口声明，节点各自声明 Runner。
	globalBeanInit := 0
	application := New(
		WithRuntimeOptions(appruntime.WithBeanDefinitions(
			ioc.Define(&definitionIsolationProbe{initCount: &globalBeanInit}),
		)),
		WithModules(Runner(func(context.Context, Runtime, CommandContext) error { return nil })),
		WithCommands(NewCommand("child",
			WithModules(Runner(func(context.Context, Runtime, CommandContext) error { return nil })),
		)),
	)
	// 第二阶段：执行两个不同节点，证明稳定 Runtime 蓝图在每次 Build 都重放。
	if err := application.ExecuteContext(context.Background(), nil); err != nil {
		t.Fatalf("execute root: %v", err)
	}
	if err := application.ExecuteContext(context.Background(), []string{"child"}); err != nil {
		t.Fatalf("execute child: %v", err)
	}
	if globalBeanInit != 2 {
		t.Fatalf("global runtime definition must initialize for both commands: count=%d", globalBeanInit)
	}
}

// TestWithoutRuntimeRejectsLocalBeanDefinitions 验证无 Runtime 节点不会静默忽略其私有对象图。
//
// 测试方法：同一节点声明 WithoutRuntime、Definition 与 Runner；执行后断言 setup error
// 包含稳定冲突原因且 Bean 从未 Init，提示调用方删除对象图或显式 RequireRuntime。
func TestWithoutRuntimeRejectsLocalBeanDefinitions(t *testing.T) {
	beanInit := 0
	application := New(
		WithoutRuntime(),
		WithBeanDefinitions(ioc.Define(&definitionIsolationProbe{initCount: &beanInit})),
		WithModules(Runner(func(context.Context, Runtime, CommandContext) error { return nil })),
	)
	err := application.ExecuteContext(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "reason=bean_definitions_require_runtime") {
		t.Fatalf("expected runtime requirement conflict, got=%v", err)
	}
	if beanInit != 0 {
		t.Fatalf("without-runtime command initialized local bean: count=%d", beanInit)
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
				WithoutRuntime(),
				WithModules(Runner(func(_ context.Context, _ Runtime, command CommandContext) error {
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
