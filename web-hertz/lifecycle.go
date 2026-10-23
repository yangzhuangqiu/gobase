package webhertz

import (
	"context"
	"errors"
	"fmt"
	"time"

	baselog "github.com/yangzhuangqiu/gobase/log"
)

// lifecycleState 表示 Server 单运行周期的内部阶段。
type lifecycleState uint8

const (
	// lifecycleStateNew 表示 Server 尚未运行，允许首次 Run 或终结性 Shutdown。
	lifecycleStateNew lifecycleState = iota
	// lifecycleStateStarting 表示 Hertz Run 已进入但监听状态尚未可见。
	lifecycleStateStarting
	// lifecycleStateRunning 表示 Hertz Engine 已确认处于监听运行状态。
	lifecycleStateRunning
	// lifecycleStateStopping 表示共享停止事务已触发，等待底层 Run 收敛。
	lifecycleStateStopping
	// lifecycleStateStopped 表示单运行周期已终结，不允许再次 Run。
	lifecycleStateStopped
)

const (
	// engineStatePollInterval 是等待 Hertz 从 starting 进入 running 的轮询间隔。
	engineStatePollInterval = time.Millisecond
)

// String 返回生命周期阶段的稳定诊断名称。
func (this lifecycleState) String() string {
	switch this {
	case lifecycleStateNew:
		return "new"
	case lifecycleStateStarting:
		return "starting"
	case lifecycleStateRunning:
		return "running"
	case lifecycleStateStopping:
		return "stopping"
	case lifecycleStateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// Run 启动 Hertz 并阻塞到监听失败或服务完成停止。
//
// 参数:
// - ctx: 必填；取消时触发共享优雅停止事务。
//
// 返回:
// - nil: 服务正常停止，包含 context 取消或显式 Shutdown；
// - error: nil context、重复运行、Hertz 运行或停止失败。
func (this *Server) Run(ctx context.Context) error {
	if ctx == nil {
		return errorForContextInvalid("op=web_hertz.run result=failed stage=validate_context reason=context_required")
	}
	shouldStart, err := this.beginRun(ctx)
	if err != nil || !shouldStart {
		return err
	}

	// 第一阶段：Hertz Run 独立执行，当前调用者负责等待完整运行周期。
	go this.executeEngineRun()
	go this.observeRunning()

	// 第二阶段：context 取消只触发一次共享停止，Run 仍等待 Engine 真正返回。
	select {
	case <-this.engineDone:
	case <-ctx.Done():
		this.requestShutdown()
		<-this.engineDone
	}
	// Engine 返回与 Shutdown 结果发布存在极短并发窗口；统一等待稳定快照。
	<-this.shutdownDone

	// 第三阶段：统一归一化优雅停止与真实运行/停止错误。
	result := this.resolveRunResult()
	if result != nil {
		logError(this.logger, ctx, "Hertz server lifecycle failed",
			baselog.F("op", "web_hertz.run"),
			baselog.F("result", "failed"),
			baselog.F("reason", "lifecycle_failed"),
			baselog.Err(result),
		)
	}
	return result
}

// Shutdown 触发共享优雅停止并等待当前调用者的 context 或稳定停止结果。
//
// 参数:
// - ctx: 必填；只约束当前调用者等待，不会截断已触发的底层停止事务。
//
// 返回:
// - nil: Server 已停止；
// - error: nil context、当前等待取消/超时或底层 Hertz Shutdown 失败。
func (this *Server) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return errorForContextInvalid("op=web_hertz.shutdown result=failed stage=validate_context reason=context_required")
	}

	// 第一阶段：new 状态直接终结；运行态只允许一个调用者启动底层停止事务。
	this.mu.Lock()
	if this.state == lifecycleStateNew {
		this.state = lifecycleStateStopped
		this.completeShutdownLocked(nil)
		this.mu.Unlock()
		logInfo(this.logger, ctx, "Hertz server stopped before run",
			baselog.F("op", "web_hertz.shutdown"),
			baselog.F("result", "ok"),
			baselog.F("reason", "before_run"),
		)
		return nil
	}
	startDriver := this.requestShutdownLocked()
	shutdownDone := this.shutdownDone
	this.mu.Unlock()
	if startDriver {
		go this.driveShutdown()
	}

	// 第二阶段：调用者 deadline 不污染后台共享停止结果。
	select {
	case <-shutdownDone:
		return this.resolveShutdownResult()
	case <-ctx.Done():
		err := errorForShutdownFailed(
			"op=web_hertz.shutdown result=failed stage=wait reason=context_done",
			errors.Join(ErrShutdownWaitInterrupted, ctx.Err()),
		)
		logError(this.logger, ctx, "Hertz shutdown wait interrupted",
			baselog.F("op", "web_hertz.shutdown"),
			baselog.F("result", "failed"),
			baselog.F("reason", "context_done"),
			baselog.Err(ctx.Err()),
		)
		return err
	}
}

// beginRun 原子校验并进入首次运行周期。
// 阶段角色：已取消 context 不启动监听，但会终结当前单运行周期。
func (this *Server) beginRun(ctx context.Context) (bool, error) {
	this.mu.Lock()
	defer this.mu.Unlock()
	if this.state != lifecycleStateNew {
		return false, errorForStateInvalid(messagef("op=web_hertz.run result=failed stage=transition_state reason=run_not_allowed state=%s", this.state.String()))
	}
	if ctx.Err() != nil {
		this.state = lifecycleStateStopped
		this.completeShutdownLocked(nil)
		return false, nil
	}
	this.state = lifecycleStateStarting
	return true, nil
}

// executeEngineRun 执行第三方阻塞入口并发布唯一 Engine 结果。
// 阶段角色：无论返回或 panic 都关闭 engineDone，解除 Run/Shutdown 等待者。
func (this *Server) executeEngineRun() {
	err := this.callEngineRun()

	this.mu.Lock()
	this.engineErr = err
	this.state = lifecycleStateStopped
	close(this.engineDone)
	completeNaturally := !this.shutdownStarted
	this.mu.Unlock()
	if completeNaturally {
		// 自然退出同样等待观测阶段结束，确保稳定结果后不再访问 hlog。
		<-this.runningObservationDone
		this.completeShutdown(nil)
	}
}

// callEngineRun 把 Hertz/netpoll 运行期 panic 转换为根 web.Server 允许返回的 error。
// 阶段角色：构造期路由 panic 不经过此边界，仍保持 fail-fast panic 语义。
func (this *Server) callEngineRun() (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("stage=engine_run reason=panic: %w", panicAsError(recovered))
		}
	}()
	return this.engine.Run()
}

// observeRunning 等待 Hertz 对外确认运行状态并发布一次 running 日志。
// 阶段角色：不参与停止决策，只提高状态诊断与启动可观测性。
func (this *Server) observeRunning() {
	defer close(this.runningObservationDone)
	ticker := time.NewTicker(engineStatePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-this.engineDone:
			return
		case <-ticker.C:
			if !this.engine.IsRunning() {
				continue
			}
			this.mu.Lock()
			if this.state == lifecycleStateStarting {
				this.state = lifecycleStateRunning
			}
			this.mu.Unlock()
			logInfo(this.logger, context.Background(), "Hertz server running",
				baselog.F("op", "web_hertz.run"),
				baselog.F("result", "running"),
				baselog.F("address", this.address),
			)
			return
		}
	}
}

// requestShutdown 触发一次共享停止事务，不等待结果。
// 阶段角色：供 Run context 取消路径使用。
func (this *Server) requestShutdown() {
	this.mu.Lock()
	startDriver := this.requestShutdownLocked()
	this.mu.Unlock()
	if startDriver {
		go this.driveShutdown()
	}
}

// requestShutdownLocked 在锁内记录停止意图并判断是否需要启动 driver。
// 前置条件：调用方持有 mu。
func (this *Server) requestShutdownLocked() bool {
	if this.shutdownStarted || this.shutdownCompleted {
		return false
	}
	if this.state != lifecycleStateStarting && this.state != lifecycleStateRunning {
		return false
	}
	this.shutdownStarted = true
	this.state = lifecycleStateStopping
	return true
}

// driveShutdown 等待 Engine 真正 running 后调用一次 Hertz Shutdown。
//
// 流程:
// 1. starting 阶段不直接调用 Shutdown，避免 Hertz 返回 status not running；
// 2. Engine running 后使用 Background，让配置的 ExitWaitTime 独立约束共享停止；
// 3. 等待 Run goroutine 返回并记录最终日志后，再发布稳定停止结果。
func (this *Server) driveShutdown() {
	ticker := time.NewTicker(engineStatePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-this.engineDone:
			<-this.runningObservationDone
			this.completeShutdown(nil)
			return
		case <-ticker.C:
			if !this.engine.IsRunning() {
				continue
			}

			err := this.engine.Shutdown(context.Background())
			if isHertzNotRunningError(err) {
				// Engine 在 IsRunning 与 Shutdown 之间自然返回时，按“已停止”归一化。
				err = nil
			}
			<-this.engineDone
			<-this.runningObservationDone
			// 最终日志必须先于 shutdownDone 发布，允许上层在稳定结果后安全恢复全局 hlog。
			if err != nil {
				logError(this.logger, context.Background(), "Hertz engine shutdown failed",
					baselog.F("op", "web_hertz.shutdown"),
					baselog.F("result", "failed"),
					baselog.F("reason", "engine_shutdown_failed"),
					baselog.Err(err),
				)
			} else {
				logInfo(this.logger, context.Background(), "Hertz engine stopped",
					baselog.F("op", "web_hertz.shutdown"),
					baselog.F("result", "ok"),
					baselog.F("reason", "engine_stopped"),
				)
			}
			this.completeShutdown(err)
			return
		}
	}
}

// completeShutdown 原子发布一次共享停止结果。
func (this *Server) completeShutdown(err error) {
	this.mu.Lock()
	this.completeShutdownLocked(err)
	this.mu.Unlock()
}

// completeShutdownLocked 在锁内关闭 shutdownDone 并缓存首次结果。
// 前置条件：调用方持有 mu。
func (this *Server) completeShutdownLocked(err error) {
	if this.shutdownCompleted {
		return
	}
	this.shutdownErr = err
	this.shutdownCompleted = true
	close(this.shutdownDone)
}

// resolveRunResult 归一化 Engine Run 与共享 Shutdown 的最终结果。
func (this *Server) resolveRunResult() error {
	this.mu.Lock()
	engineErr := this.engineErr
	shutdownErr := this.shutdownErr
	this.mu.Unlock()
	if shutdownErr != nil {
		return errorForShutdownFailed(
			"op=web_hertz.run result=failed stage=engine_shutdown reason=shutdown_failed",
			errors.Join(shutdownErr, engineErr),
		)
	}
	if engineErr == nil {
		return nil
	}
	return errorForRunFailed("op=web_hertz.run result=failed stage=engine_run reason=engine_error", engineErr)
}

// resolveShutdownResult 返回共享停止事务的稳定结果。
func (this *Server) resolveShutdownResult() error {
	this.mu.Lock()
	err := this.shutdownErr
	this.mu.Unlock()
	if err == nil {
		return nil
	}
	return errorForShutdownFailed("op=web_hertz.shutdown result=failed stage=engine_shutdown reason=shutdown_failed", err)
}

// panicAsError 把任意 panic 值转为可包装 cause。
func panicAsError(value any) error {
	if err, ok := value.(error); ok {
		return err
	}
	return fmt.Errorf("%v", value)
}

// isHertzNotRunningError 归一化 Hertz 未导出的重复停止状态错误。
// 说明：Hertz v0.10.5 未导出该 sentinel，适配层通过锁定版本和测试约束文本。
func isHertzNotRunningError(err error) bool {
	return err != nil && err.Error() == "engine is not running"
}
