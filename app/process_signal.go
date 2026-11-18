package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

// processSignalSource 定义进程入口使用的信号订阅适配边界。
// 阶段角色：App.Run 安装和恢复进程级信号行为；ExecuteContext 不使用该能力。
type processSignalSource interface {
	// Notify 订阅配置信号并返回信号事件和恢复默认行为的函数。
	Notify(signals ...os.Signal) (<-chan os.Signal, func())
}

// osProcessSignalSource 使用标准库实现进程信号订阅。
type osProcessSignalSource struct{}

// Notify 订阅所选进程信号，并保留实际信号身份供 Run 构造类型化取消原因。
func (this osProcessSignalSource) Notify(signals ...os.Signal) (<-chan os.Signal, func()) {
	events := make(chan os.Signal, 1)
	signal.Notify(events, signals...)
	return events, func() { signal.Stop(events) }
}

// defaultShutdownSignals 返回当前平台最小且可预期的默认退出信号集合。
// Unix 同时处理 SIGINT/SIGTERM；Windows 的 SIGTERM 语义不稳定，只处理 os.Interrupt。
func defaultShutdownSignals() []os.Signal {
	if runtime.GOOS == "windows" {
		return []os.Signal{os.Interrupt}
	}
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

// processSignalCause 表示 Run 因具体进程信号进入关闭流程。
type processSignalCause struct {
	// Signal 是本次 Run 实际收到的首个配置信号。
	Signal os.Signal
}

// Error 返回可检索的信号关闭原因，并保留具体信号名称。
func (this *processSignalCause) Error() string {
	if this == nil || this.Signal == nil {
		return "op=app.run result=triggered reason=process_signal source=process_signal signal=unknown"
	}
	return fmt.Sprintf("op=app.run result=triggered reason=process_signal source=process_signal signal=%s", this.Signal)
}

// Unwrap 保留标准 context 取消分支，同时让调用方可以读取类型化信号身份。
func (this *processSignalCause) Unwrap() error {
	return context.Canceled
}

// watchProcessSignals 把首个具体信号转换为类型化 context cause，并立即恢复默认行为。
// 阶段角色：Run 启动后独立等待信号；正常执行先结束时由 executionSession 通知退出。
func watchProcessSignals(
	events <-chan os.Signal,
	session *executionSession,
	cancel context.CancelCauseFunc,
) {
	select {
	case received := <-events:
		if received == nil {
			return
		}
		cause := session.RecordSignal(&processSignalCause{Signal: received})
		// 先恢复默认处理，再发布取消，保证关闭流程启动时第二次信号已经可以强制退出。
		session.RestoreSignals()
		cancel(cause)
	case <-session.SignalsRestored():
	}
}
