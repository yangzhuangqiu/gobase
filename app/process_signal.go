package app

import (
	"context"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
)

// processSignalSource 定义进程入口使用的信号订阅适配边界。
// 阶段角色：App.Run 安装和恢复进程级信号行为；ExecuteContext 不使用该能力。
type processSignalSource interface {
	// Notify 创建首个配置信号到达时取消的 context，并返回恢复默认行为的函数。
	Notify(parent context.Context, signals ...os.Signal) (context.Context, func())
}

// osProcessSignalSource 使用标准库实现进程信号订阅。
type osProcessSignalSource struct{}

// Notify 把所选进程信号转换为 context 取消，并允许 Shutdown 开始时立即恢复默认信号行为。
func (this osProcessSignalSource) Notify(parent context.Context, signals ...os.Signal) (context.Context, func()) {
	return signal.NotifyContext(parent, signals...)
}

// defaultShutdownSignals 返回当前平台最小且可预期的默认退出信号集合。
// Unix 同时处理 SIGINT/SIGTERM；Windows 的 SIGTERM 语义不稳定，只处理 os.Interrupt。
func defaultShutdownSignals() []os.Signal {
	if runtime.GOOS == "windows" {
		return []os.Signal{os.Interrupt}
	}
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}

// executionControl 保存一次进程入口执行独占的信号恢复能力。
// 阶段角色：任一 Shutdown 触发后立即恢复系统默认信号行为，使后续信号可强制终止进程。
type executionControl struct {
	// RestoreSignals 恢复本次 Run 安装前的进程信号行为；允许重复调用。
	RestoreSignals func()
	// coordinatorStarted 标记取消已经由 Runner/Shutdown 协调器接管。
	coordinatorStarted atomic.Bool
}

// executionControlContextKey 隔离进程信号控制器在命令 context 中的存储键。
type executionControlContextKey struct{}

// newExecutionControl 把底层恢复函数封装为并发安全、最多执行一次的进程入口控制器。
func newExecutionControl(restore func()) *executionControl {
	var once sync.Once
	return &executionControl{RestoreSignals: func() {
		once.Do(func() {
			if restore != nil {
				restore()
			}
		})
	}}
}

// withExecutionControl 把进程入口控制能力附加到本次命令 context。
func withExecutionControl(ctx context.Context, control *executionControl) context.Context {
	return context.WithValue(ctx, executionControlContextKey{}, control)
}

// markExecutionCoordinatorStarted 标记后续 signal cancellation 已进入统一 Shutdown 流程。
func markExecutionCoordinatorStarted(ctx context.Context) {
	if control := executionControlFrom(ctx); control != nil {
		control.coordinatorStarted.Store(true)
	}
}

// executionCoordinatorStarted 判断进程入口是否已经进入 Runner/Shutdown 协调阶段。
func executionCoordinatorStarted(ctx context.Context) bool {
	control := executionControlFrom(ctx)
	return control != nil && control.coordinatorStarted.Load()
}

// executionControlFrom 读取 Run 注入的进程入口控制器；ExecuteContext 返回 nil。
func executionControlFrom(ctx context.Context) *executionControl {
	control, _ := ctx.Value(executionControlContextKey{}).(*executionControl)
	return control
}

// restoreExecutionSignals 在 Shutdown 开始时恢复进程信号默认行为；嵌入式入口为 no-op。
func restoreExecutionSignals(ctx context.Context) {
	control := executionControlFrom(ctx)
	if control != nil && control.RestoreSignals != nil {
		control.RestoreSignals()
	}
}
