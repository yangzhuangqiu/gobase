package webhertz

// HandlerCompletion 定义 Hertz transport 停止后如何判定 Handler 阶段完成的关闭合同。
//
// 典型调用场景:
// 1. bestEffort 用于要求关闭时间有界、接受未完成 Handler 风险的服务；
// 2. required 用于要求 Handler 全部返回后才允许释放其依赖资源的服务。
type HandlerCompletion string

const (
	// HandlerCompletionBestEffort 表示沿用 Hertz 有界尽力关闭；ExitWaitTime 到期后允许 Run 返回。
	HandlerCompletionBestEffort HandlerCompletion = "bestEffort"
	// HandlerCompletionRequired 表示安装请求 gate；所有已放行 Handler 返回前 Run 不得返回。
	HandlerCompletionRequired HandlerCompletion = "required"
)

// ShutdownConfig 定义 Server 单运行周期的关闭合同。
//
// 语义：配置只在构造阶段冻结；运行期改变合同会破坏请求准入与完成屏障的一致性，因此不支持热更新。
type ShutdownConfig struct {
	// HandlerCompletion 决定 Handler 完成条件；默认 bestEffort，可选 bestEffort|required。
	HandlerCompletion HandlerCompletion `yaml:"handlerCompletion"`
}

// isSupportedHandlerCompletion 判断值是否属于稳定公开合同集合。
// 阶段角色：Config 和显式 Option 共用，避免两条声明路径接受不同值。
func isSupportedHandlerCompletion(value HandlerCompletion) bool {
	return value == HandlerCompletionBestEffort || value == HandlerCompletionRequired
}
