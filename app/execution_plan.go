package app

import "errors"

// executionPlan 是选中命令从 root 到叶子完成字段级归并后的不可变执行快照。
type executionPlan struct {
	// RuntimeRequirement 是最近节点显式声明后得到的有效 Runtime 需求。
	RuntimeRequirement runtimeRequirement
	// FlagBinder 是选中节点局部绑定器；Flag 声明不继承。
	FlagBinder FlagBinder
	// BeforeHooks 是按 root 到叶子顺序追加的准备 Hook。
	BeforeHooks []BeforeRunHook
	// Workloads 是选中节点局部 Workload；执行单元不继承。
	Workloads []Workload
	// ShutdownHooks 是按继承清空规则归并后的高级关闭 Hook。
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
	hookNames := make(map[string]struct{})
	for _, node := range path {
		// Runtime 需求、预算采用最近显式声明覆盖；Hook 和 Before 采用路径顺序累加。
		if node.RuntimeRequirement != runtimeRequirementUnspecified {
			plan.RuntimeRequirement = node.RuntimeRequirement
		}
		plan.BeforeHooks = append(plan.BeforeHooks, node.BeforeHooks...)
		if node.DisableInheritedShutdownHooks {
			plan.ShutdownHooks = nil
			hookNames = make(map[string]struct{})
		}
		for _, hook := range node.ShutdownHooks {
			if _, exists := hookNames[hook.Name]; exists {
				plan.InputErr = errors.Join(plan.InputErr, errorForAppInternal(messagef(
					"op=app.command result=failed stage=merge_shutdown_hooks reason=name_duplicate name=%s", hook.Name,
				)))
				continue
			}
			hookNames[hook.Name] = struct{}{}
			plan.ShutdownHooks = append(plan.ShutdownHooks, hook)
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
	plan.Workloads = append([]Workload{}, selected.Workloads...)
	return plan
}
