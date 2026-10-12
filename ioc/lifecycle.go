package ioc

import (
	"context"
	"errors"
	"runtime/debug"
	"slices"
	"strings"

	"github.com/yangzhuangqiu/gobase/errcode"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

// Initializer 定义 Build 阶段的初始化契约。
//
// 典型调用场景：
// 1. 依赖注入完成后执行启动动作；
// 2. 失败时终止 Build 并返回错误。
type Initializer interface {
	// Init 在 Build 阶段被调用，执行对象初始化逻辑。
	Init(ctx context.Context) error
}

// Closer 定义容器关闭阶段的资源回收契约。
type Closer interface {
	// Close 在容器关闭阶段被调用，执行资源释放逻辑。
	Close(ctx context.Context) error
}

// buildBeans 执行 Build 主流程：依赖图 -> 注入 -> 初始化。
//
// 流程：
// 1. 先构造依赖图并完成拓扑排序；
// 2. 再按拓扑顺序执行字段注入；
// 3. 最后执行 Init，失败时回滚已初始化对象。
func (this *container) buildBeans(ctx context.Context, beans []*beanDefinition) ([]*beanDefinition, error) {
	// 阶段一：建图并拓扑排序，提前发现循环依赖。
	graph, err := this.buildDependencyGraph(beans)
	if err != nil {
		return nil, err
	}
	order, err := this.topologicalOrder(beans, graph)
	if err != nil {
		// 降级分支：弱约束模式允许循环依赖，按注册顺序执行后续阶段。
		if !errcode.IsCode[ErrorCode](err, ErrCodeIOCBuildCircular) {
			return nil, err
		}
		if !this.allowCircular {
			this.metrics.buildCircularRejectedTotal.Add(1)
			return nil, err
		}
		chain := extractCircularChain(err)
		order = this.registrationOrder(beans)
		orderNames := this.beanNames(order)
		this.metrics.buildCircularDegradedTotal.Add(1)
		this.metrics.setLastCircularDegrade(chain, orderNames)
		this.logger.Warn(ctx, "ioc build uses circular dependency fallback",
			baselog.Op("ioc.build"), baselog.Result(baselog.ResultDegraded),
			baselog.F("stage", "order"), baselog.Reason("allow_circular_dependency"),
			baselog.F("chain", chain), baselog.F("order", orderNames),
		)
	}
	// 阶段二：按拓扑顺序注入，保证依赖先于被依赖方可用。
	for _, bean := range order {
		if err = this.injectBean(bean); err != nil {
			return nil, err
		}
	}

	initialized := make([]*beanDefinition, 0, len(order))
	// 阶段三：执行 Init，失败时回滚已完成初始化对象。
	for _, bean := range order {
		if !bean.managedLifecycle {
			continue
		}
		initializer, ok := bean.instance.(Initializer)
		if !ok {
			continue
		}
		initErr, panicValue := invokeInitializer(ctx, initializer)
		if panicValue != nil {
			this.metrics.initFailedTotal.Add(1)
			panicErr := errorForIOCBuildInitFailed(
				messagef("op=ioc.build result=failed stage=init reason=init_panic bean=%s", bean.name),
				panicValueAsError(panicValue),
			)
			panicErr = this.rollbackInitialized(ctx, initialized, panicErr)
			this.errorFailure(ctx, "ioc.build", panicErr)
			panic(panicErr)
		}
		if initErr != nil {
			this.metrics.initFailedTotal.Add(1)
			wrappedErr := errorForIOCBuildInitFailed(messagef("op=ioc.build result=failed stage=init reason=init_failed bean=%s", bean.name), initErr)
			return nil, this.rollbackInitialized(ctx, initialized, wrappedErr)
		}
		initialized = append(initialized, bean)
	}
	return order, nil
}

// invokeInitializer 调用单个 Init，并把业务 panic 转换为可回滚的显式结果。
// 阶段角色：Build Init 阶段的异常边界。
func invokeInitializer(ctx context.Context, initializer Initializer) (err error, panicValue any) {
	defer func() {
		panicValue = recover()
	}()
	err = initializer.Init(ctx)
	return err, nil
}

// panicValueAsError 把任意 panic payload 转换为可加入错误链的 cause。
// 阶段角色：Init panic 的错误语义归一化。
func panicValueAsError(value any) error {
	if err, ok := value.(error); ok {
		return err
	}
	return errorForIOCInternal(messagef("op=ioc.build result=failed stage=init reason=panic_payload value=%v", value))
}

// rollbackInitialized 逆序关闭成功 Init 的 bean，并聚合原始失败与回滚失败。
// 阶段角色：Build Init error/panic 的统一资源收口。
func (this *container) rollbackInitialized(ctx context.Context, initialized []*beanDefinition, cause error) error {
	rollbackErr := this.closeDefinitions(ctx, initialized)
	if rollbackErr == nil {
		return cause
	}
	return errors.Join(
		cause,
		errorForIOCBuildInitFailed(messagef("op=ioc.build result=failed stage=rollback_close reason=close_failed"), rollbackErr),
	)
}

// registrationOrder 返回按注册顺序稳定排序的 bean 列表。
// 阶段角色：循环依赖降级模式的执行顺序来源。
func (this *container) registrationOrder(beans []*beanDefinition) []*beanDefinition {
	result := append([]*beanDefinition{}, beans...)
	slices.SortFunc(result, func(left *beanDefinition, right *beanDefinition) int {
		return left.seq - right.seq
	})
	return result
}

// closeDefinitions 按逆序关闭全部给定 bean，并聚合每个失败。
// 阶段角色：Build 失败回滚与正常 Close 共用的资源收口。
func (this *container) closeDefinitions(ctx context.Context, definitions []*beanDefinition) error {
	var errs []error
	// 逆序关闭：保持与初始化相反顺序，降低资源依赖冲突。
	for index := len(definitions) - 1; index >= 0; index-- {
		item := definitions[index]
		if !item.managedLifecycle {
			continue
		}
		closer, ok := item.instance.(Closer)
		if !ok {
			continue
		}
		if err := this.closeOne(ctx, item, closer); err != nil {
			this.metrics.closeFailedTotal.Add(1)
			errs = append(errs, errorForIOCCloseFailed(messagef("op=ioc.close result=failed stage=close_bean reason=close_failed bean=%s", item.name), err))
		}
	}
	return errors.Join(errs...)
}

// closeOne 关闭单个 bean，并在启用时应用 close 超时策略。
// 阶段角色：Close 路径的单点关闭语义封装。
func (this *container) closeOne(ctx context.Context, bean *beanDefinition, closer Closer) error {
	// 阶段一：从调用方 context 派生可选的单 bean timeout。
	closeCtx := ctx
	cancel := func() {}
	if this.closeTimeout > 0 {
		closeCtx, cancel = context.WithTimeout(ctx, this.closeTimeout)
	}
	defer cancel()

	// 阶段二：独立 goroutine 允许 context 结束容器等待；panic 在 goroutine 内转换为 error。
	done := make(chan error, 1)
	go func() {
		done <- this.invokeCloser(closeCtx, bean, closer)
	}()
	// 阶段三：返回真实 Closer 结果，或返回协作式取消/超时结果。
	select {
	case err := <-done:
		return err
	case <-closeCtx.Done():
		if errors.Is(closeCtx.Err(), context.DeadlineExceeded) && this.closeTimeout > 0 {
			this.metrics.closeTimeoutTotal.Add(1)
			return errorForIOCCloseFailed(messagef("op=ioc.close result=failed stage=close_bean reason=close_timeout bean=%s timeout_ms=%d",
				bean.name, this.closeTimeout.Milliseconds(),
			))
		}
		return errorForIOCCloseFailed(messagef("op=ioc.close result=failed stage=close_bean reason=close_canceled bean=%s", bean.name), closeCtx.Err())
	}
}

// invokeCloser 调用单个 Closer，并把 panic 转换为携带 stack 的关闭错误。
// 阶段角色：Closer goroutine 的异常边界，保证 panic 可聚合且不阻断后续资源释放。
func (this *container) invokeCloser(ctx context.Context, bean *beanDefinition, closer Closer) (err error) {
	defer func() {
		panicValue := recover()
		if panicValue == nil {
			return
		}
		this.metrics.closePanicTotal.Add(1)
		stack := strings.TrimSpace(string(debug.Stack()))
		err = errorForIOCCloseFailed(
			messagef("op=ioc.close result=failed stage=close_bean reason=close_panic bean=%s panic_stack=%q", bean.name, stack),
			panicValueAsError(panicValue),
		)
	}()
	return closer.Close(ctx)
}

// buildDependencyGraph 为每个 bean 构造依赖边集合。
// 阶段角色：拓扑排序前的依赖建图步骤。
func (this *container) buildDependencyGraph(beans []*beanDefinition) (map[*beanDefinition][]*beanDefinition, error) {
	graph := make(map[*beanDefinition][]*beanDefinition, len(beans))
	// 逐 bean 收集边：每个节点只关心自身字段依赖。
	for _, bean := range beans {
		dependencies, err := this.collectDependencies(bean)
		if err != nil {
			return nil, err
		}
		graph[bean] = dependencies
	}
	return graph, nil
}

// topologicalOrder 对依赖图做拓扑排序，并检测循环依赖。
//
// 流程：
// 1. DFS 访问依赖图并记录访问状态；
// 2. 命中 in-path 节点即判定循环依赖；
// 3. DFS 回溯后产出稳定初始化顺序。
func (this *container) topologicalOrder(beans []*beanDefinition, graph map[*beanDefinition][]*beanDefinition) ([]*beanDefinition, error) {
	type visitState uint8
	const (
		visitStateIdle visitState = iota
		visitStateInPath
		visitStateDone
	)

	states := make(map[*beanDefinition]visitState, len(beans))
	stack := make([]*beanDefinition, 0, len(beans))
	order := make([]*beanDefinition, 0, len(beans))

	var visit func(bean *beanDefinition) error
	visit = func(bean *beanDefinition) error {
		// 阶段一：状态检查，处理已完成节点和循环节点。
		switch states[bean] {
		case visitStateDone:
			return nil
		case visitStateInPath:
			chain := this.buildCycleChain(stack, bean)
			return errorForIOCBuildCircular(messagef("op=ioc.build result=failed stage=check_dependency reason=circular_dependency chain=%s", chain))
		}

		// 阶段二：进入 DFS 栈，递归访问依赖。
		states[bean] = visitStateInPath
		stack = append(stack, bean)
		dependencies := append([]*beanDefinition{}, graph[bean]...)
		slices.SortFunc(dependencies, func(left *beanDefinition, right *beanDefinition) int {
			return left.seq - right.seq
		})
		for _, dependency := range dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		// 阶段三：回溯出栈，标记完成并写入拓扑序。
		stack = stack[:len(stack)-1]
		states[bean] = visitStateDone
		order = append(order, bean)
		return nil
	}

	items := append([]*beanDefinition{}, beans...)
	// 外层遍历：保证非连通子图也能被完整遍历。
	slices.SortFunc(items, func(left *beanDefinition, right *beanDefinition) int {
		return left.seq - right.seq
	})
	for _, item := range items {
		if err := visit(item); err != nil {
			return nil, err
		}
	}
	return order, nil
}

// buildCycleChain 从当前 DFS 栈构造循环依赖链路文本。
// 阶段角色：循环依赖报错时的诊断信息构建。
func (this *container) buildCycleChain(stack []*beanDefinition, target *beanDefinition) string {
	start := 0
	// 定位循环起点：从 DFS 栈中找到目标节点首次出现位置。
	for index, item := range stack {
		if item == target {
			start = index
			break
		}
	}
	chain := make([]string, 0, len(stack)-start+1)
	// 生成链路：拼接 target 后形成闭环路径文本。
	for _, item := range stack[start:] {
		chain = append(chain, item.name)
	}
	chain = append(chain, target.name)
	return strings.Join(chain, " -> ")
}

// beanNames 返回 bean 列表的名称序列，用于日志与指标记录。
// 阶段角色：循环依赖降级场景的可观测信息构建。
func (this *container) beanNames(beans []*beanDefinition) []string {
	result := make([]string, 0, len(beans))
	for _, bean := range beans {
		result = append(result, bean.name)
	}
	return result
}

// extractCircularChain 从循环依赖错误中提取 chain 字段内容。
// 阶段角色：降级日志与指标记录的链路提取器。
func extractCircularChain(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	index := strings.Index(text, "chain=")
	if index < 0 {
		return ""
	}
	return strings.TrimSpace(text[index+len("chain="):])
}
