package webhertz

import (
	"context"
	"errors"
	"time"

	"github.com/yangzhuangqiu/gobase/errcode"
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
// - nil: 服务按 handlerCompletion 合同停止，包含 context 取消或显式 Shutdown；
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
	go this.observeStartup()

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

// WaitStarted 等待首次 Run 被 Hertz 确认为 running 或发布稳定启动失败。
//
// 参数:
// - ctx: 必填；只约束当前调用者等待，不触发或取消 Server 生命周期。
//
// 返回:
// - nil: 本运行周期已经成功启动；
// - error: 当前等待被取消，或 Server 在启动成功前失败/被终结。
func (this *Server) WaitStarted(ctx context.Context) error {
	if ctx == nil {
		return errorForContextInvalid("op=web_hertz.wait_started result=failed stage=validate_context reason=context_required")
	}
	this.mu.Lock()
	startupDone := this.startupDone
	this.mu.Unlock()
	// 已缓存的共享结果优先于 caller context，保证后续查询不因 select 随机性改变语义。
	select {
	case <-startupDone:
		return this.resolveStartupResult()
	default:
	}
	select {
	case <-startupDone:
		return this.resolveStartupResult()
	case <-ctx.Done():
		// 两个信号并发到达时再次检查共享结果，使启动成功/失败提交拥有确定优先级。
		select {
		case <-startupDone:
			return this.resolveStartupResult()
		default:
		}
		return errorForStartupFailed(
			"op=web_hertz.wait_started result=failed stage=wait reason=context_done",
			errors.Join(ErrStartupWaitInterrupted, ctx.Err()),
		)
	}
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
		this.completeStartupLocked(errorForStartupFailed("op=web_hertz.startup result=failed stage=transition_state reason=shutdown_before_ready"))
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
		this.completeStartupLocked(errorForStartupFailed(
			"op=web_hertz.startup result=failed stage=transition_state reason=run_context_done",
			ctx.Err(),
		))
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
	// Engine 返回只表示监听/transport 已停止；required 合同仍需等待 Handler 屏障。
	this.state = lifecycleStateStopping
	close(this.engineDone)
	completeNaturally := !this.shutdownStarted
	this.mu.Unlock()
	if completeNaturally {
		// 自然退出同样关闭准入并等待 Handler，避免异常监听退出绕过资源安全合同。
		this.beginHandlerDrain()
		<-this.startupObservationDone
		this.waitHandlerDrain()
		this.completeShutdown(nil)
	}
}

// callEngineRun 把 Hertz/netpoll 运行期 panic 转换为根 web.Server 允许返回的 error。
// 阶段角色：构造期路由 panic 不经过此边界，仍保持 fail-fast panic 语义。
func (this *Server) callEngineRun() (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			message := messagef("op=web_hertz.run result=failed stage=engine_run reason=panic panic=%v", recovered)
			if cause, ok := recovered.(error); ok {
				err = errorForRunFailed(message, cause)
				return
			}
			err = errorForRunFailed(message)
		}
	}()
	return this.engine.Run()
}

// observeStartup 等待 Hertz 对外确认运行状态，并发布一次可广播的 startup result。
// 阶段角色：把内部 IsRunning 轮询收敛为启动成功/失败二选一的稳定完成点。
func (this *Server) observeStartup() {
	defer close(this.startupObservationDone)
	ticker := time.NewTicker(engineStatePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-this.engineDone:
			this.completeStartup(this.startupFailureFromEngine())
			return
		case <-ticker.C:
			if !this.engine.IsRunning() {
				continue
			}
			this.mu.Lock()
			ready := this.state == lifecycleStateStarting
			if ready {
				this.state = lifecycleStateRunning
				this.completeStartupLocked(nil)
			}
			this.mu.Unlock()
			if !ready {
				this.completeStartup(errorForStartupFailed("op=web_hertz.startup result=failed stage=observe reason=shutdown_before_ready"))
				return
			}
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
	if this.state == lifecycleStateStarting {
		this.completeStartupLocked(errorForStartupFailed("op=web_hertz.startup result=failed stage=transition_state reason=shutdown_before_ready"))
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
	// 第一阶段：required 合同先原子关闭准入；bestEffort 不安装 gate，保持 Hertz 原生热路径与关闭语义。
	this.beginHandlerDrain()
	ticker := time.NewTicker(engineStatePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-this.engineDone:
			<-this.startupObservationDone
			this.waitHandlerDrain()
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
			<-this.startupObservationDone
			// Hertz ExitWaitTime 只约束 transport；required 合同继续等待全部已放行 Handler 返回。
			this.waitHandlerDrain()
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

// beginHandlerDrain 在 required 合同下关闭请求准入；bestEffort 保持空操作。
// 阶段角色：共享 Shutdown driver 与 Engine 自然退出路径共用的一次性准入边界。
func (this *Server) beginHandlerDrain() {
	if this.requestGate == nil {
		return
	}
	this.requestGate.beginDrain()
}

// waitHandlerDrain 在 required 合同下等待全部已放行 Handler 返回；bestEffort 保持空操作。
// 阶段角色：Server 发布 shutdownDone 前的最终 Handler 完成合同。
func (this *Server) waitHandlerDrain() {
	if this.requestGate == nil {
		return
	}
	this.requestGate.waitDrained()
}

// startupFailureFromEngine 把启动成功前的 Engine 结果转换为稳定 startup error，并保留下层 Run cause。
func (this *Server) startupFailureFromEngine() error {
	this.mu.Lock()
	engineErr := this.engineErr
	this.mu.Unlock()
	if engineErr == nil {
		return errorForStartupFailed("op=web_hertz.startup result=failed stage=engine_run reason=stopped_before_ready")
	}
	return errorForStartupFailed(
		"op=web_hertz.startup result=failed stage=engine_run reason=engine_error",
		normalizeEngineRunError(engineErr),
	)
}

// completeStartup 原子发布一次首次启动结果。
func (this *Server) completeStartup(err error) {
	this.mu.Lock()
	this.completeStartupLocked(err)
	this.mu.Unlock()
}

// completeStartupLocked 在锁内关闭 startupDone 并缓存首次结果。
// 前置条件：调用方持有 mu。
func (this *Server) completeStartupLocked(err error) {
	if this.startupCompleted {
		return
	}
	this.startupErr = err
	this.startupCompleted = true
	close(this.startupDone)
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
	this.state = lifecycleStateStopped
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
	return normalizeEngineRunError(engineErr)
}

// normalizeEngineRunError 保留已经编码的运行期 panic，否则为普通 Engine error 补充稳定 Run code。
func normalizeEngineRunError(engineErr error) error {
	if errcode.IsCode[ErrorCode](engineErr, ErrCodeRunFailed) {
		return engineErr
	}
	return errorForRunFailed("op=web_hertz.run result=failed stage=engine_run reason=engine_error", engineErr)
}

// resolveStartupResult 返回全部稳定等待者共享的首次启动结果。
func (this *Server) resolveStartupResult() error {
	this.mu.Lock()
	err := this.startupErr
	this.mu.Unlock()
	return err
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

// isHertzNotRunningError 归一化 Hertz 未导出的重复停止状态错误。
// 说明：Hertz v0.10.5 未导出该 sentinel，适配层通过锁定版本和测试约束文本。
func isHertzNotRunningError(err error) bool {
	return err != nil && err.Error() == "engine is not running"
}
