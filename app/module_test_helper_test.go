package app

import "github.com/yangzhuangqiu/gobase/ioc"

// testWorkloadModule 为测试提供只贡献 Workload 的具名 Module，隔离具体基础设施装配。
type testWorkloadModule struct {
	// name 是当前测试节点内唯一的 Module 身份。
	name string
	// workloads 是收集阶段返回的纯内存测试执行单元。
	workloads []Workload
	// register 可选记录或模拟 Runtime Build 的 Module 注册阶段。
	register func(ioc.Registry) error
}

// newTestWorkloadModule 创建测试专用 workload-only Module，并复制输入切片避免外部修改。
func newTestWorkloadModule(name string, workloads ...Workload) Module {
	return &testWorkloadModule{name: name, workloads: append([]Workload{}, workloads...)}
}

// Name 返回测试断言使用的稳定 Module 身份。
func (this *testWorkloadModule) Name() string { return this.name }

// Register 委托可选测试回调；未提供时保持 workload-only 语义。
func (this *testWorkloadModule) Register(registry ioc.Registry) error {
	if this.register == nil {
		return nil
	}
	return this.register(registry)
}

// Workloads 返回该测试 Module 声明的主动单元副本。
func (this *testWorkloadModule) Workloads() []Workload {
	return append([]Workload{}, this.workloads...)
}
