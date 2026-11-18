package app

import (
	"errors"
	"sync"
)

// executionSession 保存一次 Run 事务独占的进程入口控制状态。
// 阶段角色：显式贯穿命令执行与 Shutdown 协调，不借助 context.Value 隐式传递控制能力。
type executionSession struct {
	// restoreSignals 恢复本次 Run 安装前的进程信号行为。
	restoreSignals func()
	// restored 在信号订阅已经停止后关闭，用于结束监听 goroutine。
	restored chan struct{}
	// restoreOnce 保证并发触发、正常返回和 defer 最多恢复一次信号行为。
	restoreOnce sync.Once
	// stateMu 保护跨监听 goroutine 与命令 goroutine 共享的阶段和信号原因。
	stateMu sync.RWMutex
	// coordinatorStarted 表示 Runner/Shutdown 协调器已经接管本次执行。
	coordinatorStarted bool
	// signalCause 保存触发本次关闭的首个进程信号；没有信号时为 nil。
	signalCause *processSignalCause
}

// newExecutionSession 创建 Run 本次调用独占的显式执行状态。
func newExecutionSession(restoreSignals func()) *executionSession {
	return &executionSession{
		restoreSignals: restoreSignals,
		restored:       make(chan struct{}),
	}
}

// RestoreSignals 停止本次信号订阅并恢复系统默认行为；允许并发重复调用。
func (this *executionSession) RestoreSignals() {
	if this == nil {
		return
	}
	this.restoreOnce.Do(func() {
		if this.restoreSignals != nil {
			this.restoreSignals()
		}
		close(this.restored)
	})
}

// SignalsRestored 返回信号监听分支的结束通知。
func (this *executionSession) SignalsRestored() <-chan struct{} {
	if this == nil {
		return nil
	}
	return this.restored
}

// RecordSignal 记录首个进程信号并返回可传播到 context 的类型化取消原因。
func (this *executionSession) RecordSignal(signalCause *processSignalCause) *processSignalCause {
	if this == nil || signalCause == nil {
		return signalCause
	}
	this.stateMu.Lock()
	defer this.stateMu.Unlock()
	if this.signalCause == nil {
		this.signalCause = signalCause
	}
	return this.signalCause
}

// SignalCause 返回触发当前 Run 关闭的首个进程信号原因。
func (this *executionSession) SignalCause() *processSignalCause {
	if this == nil {
		return nil
	}
	this.stateMu.RLock()
	defer this.stateMu.RUnlock()
	return this.signalCause
}

// IsSignalCause 判断取消原因是否来自当前 Run 实际收到的进程信号。
func (this *executionSession) IsSignalCause(cause error) bool {
	signalCause := this.SignalCause()
	return signalCause != nil && errors.Is(cause, signalCause)
}

// MarkCoordinatorStarted 标记 Runner/Shutdown 协调器已经接管后续关闭。
func (this *executionSession) MarkCoordinatorStarted() {
	if this == nil {
		return
	}
	this.stateMu.Lock()
	this.coordinatorStarted = true
	this.stateMu.Unlock()
}

// CoordinatorStarted 判断信号取消是否已经进入 Runner/Shutdown 协调阶段。
func (this *executionSession) CoordinatorStarted() bool {
	if this == nil {
		return false
	}
	this.stateMu.RLock()
	defer this.stateMu.RUnlock()
	return this.coordinatorStarted
}
