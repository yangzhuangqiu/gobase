package app

import "sync"

// commandExecutionBinding 为缓存命令树绑定当前一次执行的 session。
// 阶段角色：Cobra 回调从稳定绑定槽取得本次状态，避免重建 Flag 声明或借助 context.Value。
type commandExecutionBinding struct {
	// mu 保护顺序执行切换与 Cobra 回调读取 session；App 仍不承诺并发执行安全。
	mu sync.RWMutex
	// session 是当前 Run/ExecuteContext 独占的执行状态；嵌入式执行可以为 nil。
	session *executionSession
}

// bindSession 换入本次执行状态，并返回恢复先前状态的清理函数。
func (this *commandExecutionBinding) bindSession(session *executionSession) func() {
	if this == nil {
		return func() {}
	}
	// 执行前只替换会话指针；缓存 Cobra 命令和 Flag 声明保持不变。
	this.mu.Lock()
	previous := this.session
	this.session = session
	this.mu.Unlock()
	return func() {
		// 执行结束恢复先前绑定，使 panic/defer 与可能的嵌套调用不遗留状态。
		this.mu.Lock()
		this.session = previous
		this.mu.Unlock()
	}
}

// currentSession 返回当前命令事务显式绑定的执行状态。
func (this *commandExecutionBinding) currentSession() *executionSession {
	if this == nil {
		return nil
	}
	this.mu.RLock()
	defer this.mu.RUnlock()
	return this.session
}
