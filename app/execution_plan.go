package app

import (
	"errors"

	"github.com/yangzhuangqiu/gobase/ioc"
)

// executionPlan 是选中命令从 root 到叶子完成字段级归并后的不可变执行快照。
type executionPlan struct {
	// RuntimeRequirement 是最近节点显式声明后得到的有效 Runtime 需求。
	RuntimeRequirement runtimeRequirement
	// FlagBinder 是选中节点局部绑定器；Flag 声明不继承。
	FlagBinder FlagBinder
	// BeforeRunHooks 是选中节点局部准备 Hook，不继承祖先声明。
	BeforeRunHooks []BeforeRunHook
	// BeanDefinitions 是选中节点局部完整 Bean 声明；对象图不继承。
	BeanDefinitions []ioc.Definition
	// Modules 是选中节点局部 Module；IOC 声明和主动执行单元都不继承。
	Modules []Module
	// ShutdownHooks 是选中节点局部高级关闭 Hook；资源补偿动作不继承。
	ShutdownHooks []namedShutdownHook
	// ShutdownBudget 是命令路径按字段最近声明优先归并后的预算。
	ShutdownBudget shutdownBudgetDeclaration
	// InputErr 是归并阶段发现的命令路径语义冲突。
	InputErr error
}

// buildExecutionPlan 按字段语义归并 root 到选中节点的声明。
// 阶段角色：命令树构建期冻结回调所需计划，执行期不再读取可变节点状态。
func buildExecutionPlan(path []commandConfiguration) executionPlan {
	plan := executionPlan{RuntimeRequirement: runtimeRequirementRequired}
	for _, node := range path {
		// Runtime 需求和预算采用最近显式声明覆盖；其余执行对象只取选中节点。
		if node.RuntimeRequirement != runtimeRequirementUnspecified {
			plan.RuntimeRequirement = node.RuntimeRequirement
		}
		if node.ShutdownBudget.WorkloadGracefulTimeoutSet {
			plan.ShutdownBudget.WorkloadGracefulTimeout = node.ShutdownBudget.WorkloadGracefulTimeout
			plan.ShutdownBudget.WorkloadGracefulTimeoutSet = true
		}
		if node.ShutdownBudget.RuntimeCloseTimeoutSet {
			plan.ShutdownBudget.RuntimeCloseTimeout = node.ShutdownBudget.RuntimeCloseTimeout
			plan.ShutdownBudget.RuntimeCloseTimeoutSet = true
		}
	}
	selected := path[len(path)-1]
	plan.FlagBinder = selected.FlagBinder
	plan.BeforeRunHooks = append([]BeforeRunHook{}, selected.BeforeRunHooks...)
	plan.BeanDefinitions = append([]ioc.Definition{}, selected.BeanDefinitions...)
	plan.Modules = append([]Module{}, selected.Modules...)
	plan.ShutdownHooks = append([]namedShutdownHook{}, selected.ShutdownHooks...)
	if plan.RuntimeRequirement == runtimeRequirementDisabled && len(plan.BeanDefinitions) > 0 {
		plan.InputErr = errors.Join(plan.InputErr, errorForAppInternal(messagef(
			"op=app.command result=failed stage=merge_bean_definitions reason=bean_definitions_require_runtime",
		)))
	}
	return plan
}
