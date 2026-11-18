package app

import "context"

// Workload 定义由 app 监督的主动运行单元合同。
//
// 典型调用场景：HTTP/gRPC Server、Consumer、Scheduler 或常驻 Worker 需要与进程信号、
// Runtime Resource 释放形成确定的先停后关顺序。
//
// 语义：
// 1. Name 在单个 App 内必须非空且唯一；
// 2. Run 启动并阻塞到工作负载完全退出；多个 Workload 任一返回都会触发全组关闭；
// 3. Shutdown 请求 graceful 停止；实现必须允许它与 Run 并发，并支持 Run 已返回后的幂等调用；
// 4. app 只有在 Shutdown 与 Run 都返回后才释放 Resource；Workload 不得自行关闭 Runtime。
type Workload interface {
	// Name 返回用于声明去重、错误定位和关闭观测的稳定名称。
	Name() string
	// Run 启动主动工作负载，并在其内部执行单元全部退出后返回。
	Run(ctx context.Context, runtime Runtime, command CommandContext) error
	// Shutdown 请求工作负载在策略 context 内优雅停止；返回不替代 app 对 Run 收敛点的等待。
	Shutdown(ctx context.Context, runtime Runtime, command CommandContext) error
}
