package web

import "context"

// Server 定义 Web 服务在应用运行阶段的稳定生命周期契约。
//
// 典型调用场景:
// 1. 应用 Runner 在依赖初始化完成后调用 Run，并以传入 context 统一协调退出；
// 2. 资源所有者在构建回滚或应用关闭阶段调用 Shutdown，收敛监听器和在途请求；
// 3. 具体框架模块实现本接口，同时保留自身原生配置、路由与中间件模型。
//
// 语义:
// 1. 单个 Server 只允许进入一次运行周期；并发或重复 Run 必须返回错误，不得启动第二个实例；
// 2. Run 阻塞到服务停止；ctx 取消表示请求优雅停止，停止成功时返回 nil；
// 3. Shutdown 可与 Run 并发调用，停止触发必须幂等，并受本次调用 ctx 的取消或 deadline 约束；
// 4. Shutdown 一旦触发即终结本对象的运行周期；未运行时先 Shutdown，后续 Run 也必须返回错误；
// 5. Run/Shutdown 的 nil ctx、监听失败和停止失败由具体实现返回错误，不得 panic；
// 6. Server 不接管进程信号，信号必须由应用宿主转换为 context 取消或显式 Shutdown。
type Server interface {
	// Run 启动服务并阻塞到监听失败或服务完成停止。
	//
	// 参数:
	// - ctx: 必填；控制本次运行周期，取消表示请求优雅停止。
	//
	// 返回:
	// - nil: 服务正常停止，包含 ctx 取消触发且优雅停止成功；
	// - error: 输入、重复运行、监听或停止阶段失败，具体错误语义由实现模块定义。
	Run(ctx context.Context) error

	// Shutdown 请求服务优雅停止并等待本次调用的收敛结果。
	//
	// 参数:
	// - ctx: 必填；只约束本次停止等待，不得重新激活或重复释放底层资源。
	//
	// 返回:
	// - nil: 服务已停止且资源释放完成；未运行或已停止状态也应返回 nil；
	// - error: ctx 已取消、deadline 到期或底层资源停止失败。
	Shutdown(ctx context.Context) error
}
