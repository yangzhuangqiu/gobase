package app

import "strings"

// WorkloadModule 定义同时声明 IOC 能力与主动运行单元的两阶段 Module 合同。
//
// 典型调用场景：Server、Consumer 或 Scheduler 模块先由 Register 声明 Bean，
// 再由 Workloads 暴露使用这些 Bean 的主动运行单元；app 会在 Runtime Build 前冻结并校验列表。
type WorkloadModule interface {
	Module
	// Workloads 返回本次执行应监督的稳定 Workload 列表。
	// 该方法应只构造或返回纯内存适配器，不读取 Runtime、获取资源或启动 goroutine。
	Workloads() []Workload
}

// resolveModuleWorkloads 收集选中节点 Module 贡献的主动单元并校验全局身份。
// 阶段角色：Runtime 获取配置、日志和 IOC 资源前冻结本次 Workload 执行集合。
func resolveModuleWorkloads(modules []Module) ([]Workload, error) {
	var resolved []Workload
	names := make(map[string]struct{})
	// 阶段一：只访问选中节点 Module；被动 Module 跳过，WorkloadModule 展开纯内存声明。
	for moduleIndex, module := range modules {
		contributor, ok := module.(WorkloadModule)
		if !ok {
			continue
		}
		// 阶段二：Module 已由 NodeOption 校验；这里拒绝跨 Module 的 nil、空名和重复 Workload。
		for workloadIndex, workload := range contributor.Workloads() {
			if isNilValue(workload) {
				return nil, errorForAppInternal(messagef(
					"op=app.workload result=failed stage=collect_modules reason=workload_is_nil module=%s module_index=%d workload_index=%d",
					module.Name(), moduleIndex, workloadIndex,
				))
			}
			name := strings.TrimSpace(workload.Name())
			if name == "" {
				return nil, errorForAppInternal(messagef(
					"op=app.workload result=failed stage=collect_modules reason=name_empty module=%s workload_index=%d",
					module.Name(), workloadIndex,
				))
			}
			if _, exists := names[name]; exists {
				return nil, errorForAppInternal(messagef(
					"op=app.workload result=failed stage=collect_modules reason=name_duplicate module=%s workload=%s",
					module.Name(), name,
				))
			}
			names[name] = struct{}{}
			resolved = append(resolved, workload)
		}
	}
	return resolved, nil
}
