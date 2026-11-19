package web

import "context"

// StartupServer 定义可选的单周期启动结果合同，不扩张普通 Server 使用者的必需能力。
//
// 典型调用场景：服务注册、启动探针或组合 Workload 需要在实现确认启动成功后再执行后续动作。
//
// 语义：
// 1. WaitStarted 观察首次 Run 的启动结果，不启动 Server，也不表示当前持续健康；
// 2. 多个等待者观察同一个缓存结果，单个等待者 context 结束不得污染其他等待者；
// 3. Run 启动失败或成功启动前被终结时，全部稳定等待者获得同一个实现层 startup error；
// 4. Server 成功启动后即使随后停止，WaitStarted 仍返回首次启动成功结果。
type StartupServer interface {
	Server

	// WaitStarted 等待本运行周期首次启动成功或发布稳定启动失败。
	//
	// 参数:
	// - ctx: 必填；只限制当前调用者等待，不取消 Server 启动或其他等待者。
	//
	// 返回:
	// - nil: Server 本运行周期已经成功启动；
	// - error: 当前等待被取消，或 Server 在启动成功前失败/被终结。
	WaitStarted(ctx context.Context) error
}
