package ioc

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	baselog "github.com/yangzhuangqiu/gobase/log"
)

const (
	// defaultContainerLoggerName 是容器默认日志名，用于区分模块事件来源。
	defaultContainerLoggerName = "ioc.container"
)

// Container 定义 IoC 容器契约。
//
// 典型调用场景：
// 1. 启动阶段 Register 所有 bean；
// 2. 调用 Build(ctx) 执行依赖注入与 Init；
// 3. 运行期通过 Resolve/ResolveAll 获取实例；
// 4. 退出阶段调用 Close(ctx) 收敛资源。
type Container interface {
	Registry
	// Build 执行依赖注入、循环依赖检测与初始化。
	//
	// 参数：
	// - ctx：构建阶段上下文，控制取消与超时语义。
	Build(ctx context.Context) error
	// Resolve 解析单个实例到 target（按类型，必须唯一）。
	Resolve(target any) error
	// ResolveAll 解析某个声明类型的全部实例到 slice target。
	ResolveAll(target any) error
	// ResolveByName 按 target 声明类型范围内的名称解析单个实例。
	ResolveByName(name string, target any) error
	// Metrics 返回容器运行期指标快照。
	Metrics() MetricsSnapshot
	// Close 关闭容器中实现了 Closer 的实例，并让并发/重复调用共享首次结果。
	//
	// 参数：
	// - ctx：首个调用控制真实关闭执行；并发 waiter 只控制自身等待。
	//
	// 返回：
	// - 正常完成时返回首次关闭结果；waiter context 先结束时返回独立等待错误。
	Close(ctx context.Context) error
}

type containerState uint8

const (
	// stateCreated 表示容器仍接受注册，尚未开始构建。
	stateCreated containerState = iota
	// stateBuilding 表示 Build 正在锁外执行装配和初始化。
	stateBuilding
	// stateBuilt 表示 Build 已提交，可解析 bean 并可发起关闭。
	stateBuilt
	// stateClosing 表示首次 Close 正在执行，后续 Close 只能等待共享结果。
	stateClosing
	// stateClosed 表示关闭事务已完成，closeErr 已稳定发布。
	stateClosed
)

// New 创建默认 IoC 容器实现。
//
// 语义：
// 1. logger 字段默认不初始化，按需回退 gobase/log 标准 logger；
// 2. Init 返回错误时固定停止后续初始化、回滚成功项并返回 error；
// 3. 可通过 WithLogger 注入业务日志实现；
// 4. 关闭后不允许再进入注册或解析阶段。
func New(options ...Option) Container {
	// 阶段一：初始化容器基础状态。
	result := &container{
		state:           stateCreated,
		allowCircular:   false,
		closeTimeout:    0,
		beansByTypeName: map[beanKey]*beanDefinition{},
		beansByType:     map[reflect.Type][]*beanDefinition{},
		registerOrder:   make([]*beanDefinition, 0, 16),
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		// 阶段二：按顺序应用 Option，后者可覆盖前者。
		option(result)
	}
	// 阶段三：Option 应用后补默认 logger，保证后续调用可直接使用 this.logger。
	if result.logger == nil {
		result.logger = baselog.NewLoggerFactory(nil).Get(defaultContainerLoggerName)
	}
	return result
}

type container struct {
	mu sync.RWMutex

	state  containerState
	logger baselog.Logger
	// allowCircular 控制是否允许循环依赖降级执行。
	allowCircular bool
	// closeTimeout 是单个 bean Close 超时阈值；<=0 表示不启用。
	closeTimeout time.Duration
	// closeDone 在首次关闭事务完成后关闭，供并发 Close 等待共享结果。
	closeDone chan struct{}
	// closeErr 保存首次关闭事务的最终结果，closed 状态下稳定返回。
	closeErr        error
	sequence        int
	initOrder       []*beanDefinition
	beansByTypeName map[beanKey]*beanDefinition
	beansByType     map[reflect.Type][]*beanDefinition
	// metrics 是容器运行期可观测计数器集合。
	metrics containerMetrics

	registerOrder []*beanDefinition
}

// Register 注册一个 bean 及其声明类型。
//
// 流程：
// 1. 先校验容器状态与 bean 非空约束；
// 2. 再解析名称和声明类型，并构造定义对象；
// 3. 最后写入 name/type 索引与注册顺序。
func (this *container) Register(bean any, options ...RegisterOption) error {
	// 阶段一：进入注册临界区，保证 name/type 索引写入一致性。
	this.mu.Lock()
	defer this.mu.Unlock()

	if this.state == stateClosing || this.state == stateClosed {
		reason := "container_closed"
		if this.state == stateClosing {
			reason = "close_running"
		}
		err := errorForIOCContainerClosed(messagef("op=ioc.register result=failed stage=precheck reason=%s", reason))
		this.warnFailure(nil, "ioc.register", err)
		return err
	}
	if this.state != stateCreated {
		err := errorForIOCInternal(messagef("op=ioc.register result=failed stage=precheck reason=register_only_allowed_before_build state=%d", this.state))
		this.warnFailure(nil, "ioc.register", err)
		return err
	}

	beanValue := reflect.ValueOf(bean)
	if !beanValue.IsValid() {
		err := errorForIOCRegisterInvalidBean(messagef("op=ioc.register result=failed stage=validate_bean reason=invalid_value"))
		this.warnFailure(nil, "ioc.register", err)
		return err
	}
	if beanValue.Kind() == reflect.Ptr && beanValue.IsNil() {
		err := errorForIOCRegisterInvalidBean(messagef("op=ioc.register result=failed stage=validate_bean reason=nil_pointer type=%s", beanValue.Type()))
		this.warnFailure(nil, "ioc.register", err)
		return err
	}

	registerOptions := this.applyRegisterOptions(options...)
	// 阶段二：解析声明类型与名称，构建可索引元数据。
	declares, err := this.resolveDeclares(beanValue.Type(), registerOptions.declares)
	if err != nil {
		this.warnFailure(nil, "ioc.register", err)
		return err
	}
	name, err := this.resolveName(beanValue.Type(), registerOptions.name)
	if err != nil {
		this.warnFailure(nil, "ioc.register", err)
		return err
	}

	definition := &beanDefinition{
		name:             name,
		declares:         declares,
		instance:         bean,
		instanceValue:    beanValue,
		injectEnable:     registerOptions.injectEnable,
		managedLifecycle: registerOptions.managedLifecycle,
		seq:              this.sequence,
	}

	// 阶段三：先检查全部“声明类型 + 名称”键，任一冲突都不写入，保证注册原子性。
	for _, declareType := range declares {
		key := beanKey{declareType: declareType, name: name}
		if _, ok := this.beansByTypeName[key]; ok {
			err = errorForIOCRegisterDuplicate(messagef("op=ioc.register result=failed stage=check_name reason=duplicate_name declare_type=%s name=%s", declareType, name))
			this.warnFailure(nil, "ioc.register", err)
			return err
		}
	}
	this.sequence++
	// 阶段四：原子发布类型名称索引、类型候选索引与注册顺序。
	for _, declareType := range declares {
		key := beanKey{declareType: declareType, name: name}
		this.beansByTypeName[key] = definition
		this.beansByType[declareType] = append(this.beansByType[declareType], definition)
	}
	this.registerOrder = append(this.registerOrder, definition)
	this.metrics.registerTotal.Add(1)
	this.logger.Info(nil, "ioc bean registered",
		baselog.Op("ioc.register"),
		baselog.Result(baselog.ResultOK),
		baselog.F("name", definition.name),
		baselog.F("declare_count", len(definition.declares)),
		baselog.F("inject", definition.injectEnable),
		baselog.F("managed_lifecycle", definition.managedLifecycle),
	)
	return nil
}

// RegisterDefinition 注册模块提供的 Definition，并复用 Register 的状态、声明与冲突校验。
//
// 参数：
// - definition：模块输出的 Bean 与默认注册策略。
//
// 返回：
// - error：完整沿用 Register 的可分支错误语义。
func (this *container) RegisterDefinition(definition Definition) error {
	return this.Register(definition.Bean, append([]RegisterOption(nil), definition.Options...)...)
}

// Build 执行依赖注入、循环依赖检测与初始化。
//
// 流程：
// 1. 状态切换到 building 并快照当前注册集；
// 2. 执行依赖图构建、注入与 Init；
// 3. 成功后切换到 built，失败则回滚到 created。
//
// 参数：
// - ctx：构建阶段上下文；传 nil 时会自动回退 context.Background。
func (this *container) Build(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	this.metrics.buildTotal.Add(1)
	// 阶段一：抢占构建状态，阻断并发 Build/Close 冲突。
	this.mu.Lock()
	switch this.state {
	case stateClosing:
		this.mu.Unlock()
		err := errorForIOCContainerClosed(messagef("op=ioc.build result=failed stage=precheck reason=close_running"))
		this.metrics.buildFailedTotal.Add(1)
		this.warnFailure(ctx, "ioc.build", err)
		return err
	case stateClosed:
		this.mu.Unlock()
		err := errorForIOCContainerClosed(messagef("op=ioc.build result=failed stage=precheck reason=container_closed"))
		this.metrics.buildFailedTotal.Add(1)
		this.warnFailure(ctx, "ioc.build", err)
		return err
	case stateBuilt:
		this.mu.Unlock()
		this.logger.Info(ctx, "ioc build skipped",
			baselog.Op("ioc.build"), baselog.Result(baselog.ResultSkipped),
			baselog.F("stage", "skip"), baselog.Reason("already_built"),
		)
		return nil
	case stateBuilding:
		this.mu.Unlock()
		err := errorForIOCInternal(messagef("op=ioc.build result=failed stage=precheck reason=build_is_running"))
		this.metrics.buildFailedTotal.Add(1)
		this.warnFailure(ctx, "ioc.build", err)
		return err
	}
	this.state = stateBuilding
	beans := append([]*beanDefinition{}, this.registerOrder...)
	this.mu.Unlock()
	// panic 保护：当策略为 panic 时，仍保证容器状态回滚且指标可观测。
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		this.mu.Lock()
		if this.state == stateBuilding {
			this.state = stateCreated
		}
		this.mu.Unlock()
		this.metrics.buildFailedTotal.Add(1)
		panic(recovered)
	}()

	// 阶段二：在锁外执行重操作（建图、注入、初始化），缩短锁持有时间。
	this.logger.Info(ctx, "ioc build started",
		baselog.Op("ioc.build"), baselog.Result(baselog.ResultRunning),
		baselog.F("stage", "start"), baselog.F("bean_count", len(beans)),
	)
	order, err := this.buildBeans(ctx, beans)
	if err != nil {
		// 失败回滚：恢复到 created，允许修复后重新 Build。
		this.mu.Lock()
		this.state = stateCreated
		this.mu.Unlock()
		this.metrics.buildFailedTotal.Add(1)
		this.errorFailure(ctx, "ioc.build", err)
		return err
	}

	// 阶段三：发布构建结果，切换到 built 状态。
	this.mu.Lock()
	this.initOrder = order
	this.state = stateBuilt
	this.mu.Unlock()
	this.logger.Info(ctx, "ioc build completed",
		baselog.Op("ioc.build"), baselog.Result(baselog.ResultOK),
		baselog.F("stage", "finish"), baselog.F("bean_count", len(order)),
	)
	return nil
}

// Resolve 按类型解析一个唯一实例并写入 target。
//
// 流程：
// 1. 校验容器状态与 target 指针语义；
// 2. 按声明类型查找候选；
// 3. 要求候选唯一并执行赋值。
func (this *container) Resolve(target any) error {
	// 阶段一：读取锁保护状态与索引快照一致性。
	this.mu.RLock()
	defer this.mu.RUnlock()

	if this.state == stateClosing || this.state == stateClosed {
		reason := "container_closed"
		if this.state == stateClosing {
			reason = "close_running"
		}
		err := errorForIOCContainerClosed(messagef("op=ioc.resolve result=failed stage=precheck reason=%s", reason))
		this.warnFailure(nil, "ioc.resolve", err)
		return err
	}
	if this.state != stateBuilt {
		err := errorForIOCContainerNotBuilt(messagef("op=ioc.resolve result=failed stage=precheck reason=container_not_built"))
		this.warnFailure(nil, "ioc.resolve", err)
		return err
	}
	targetValue, targetType, err := resolveSingleTarget(target)
	if err != nil {
		this.warnFailure(nil, "ioc.resolve", err)
		return err
	}

	// 阶段二：按声明类型执行唯一候选决策。
	definitions := this.beansByType[targetType]
	if len(definitions) == 0 {
		err = errorForIOCResolveNotFound(messagef("op=ioc.resolve result=failed stage=lookup reason=type_not_found type=%s", targetType))
		this.warnFailure(nil, "ioc.resolve", err)
		return err
	}
	if len(definitions) > 1 {
		err = errorForIOCResolveAmbiguous(messagef("op=ioc.resolve result=failed stage=lookup reason=type_ambiguous type=%s count=%d", targetType, len(definitions)))
		this.warnFailure(nil, "ioc.resolve", err)
		return err
	}
	return assignInstance(targetValue, definitions[0].instanceValue, targetType, "ioc.resolve")
}

// ResolveAll 按类型解析所有实例并写入 target slice。
//
// 流程：
// 1. 校验容器状态与 slice 指针目标；
// 2. 获取该声明类型的全部候选并按注册顺序稳定排序；
// 3. 构造新切片并批量写入。
func (this *container) ResolveAll(target any) error {
	// 阶段一：读取锁保护状态与类型索引读取一致性。
	this.mu.RLock()
	defer this.mu.RUnlock()

	if this.state == stateClosing || this.state == stateClosed {
		reason := "container_closed"
		if this.state == stateClosing {
			reason = "close_running"
		}
		err := errorForIOCContainerClosed(messagef("op=ioc.resolve_all result=failed stage=precheck reason=%s", reason))
		this.warnFailure(nil, "ioc.resolve_all", err)
		return err
	}
	if this.state != stateBuilt {
		err := errorForIOCContainerNotBuilt(messagef("op=ioc.resolve_all result=failed stage=precheck reason=container_not_built"))
		this.warnFailure(nil, "ioc.resolve_all", err)
		return err
	}

	targetValue, elementType, err := resolveSliceTarget(target)
	if err != nil {
		this.warnFailure(nil, "ioc.resolve_all", err)
		return err
	}
	definitions := append([]*beanDefinition{}, this.beansByType[elementType]...)
	// 阶段二：按注册顺序稳定排序，保证可预测输出。
	slices.SortFunc(definitions, func(left *beanDefinition, right *beanDefinition) int {
		return left.seq - right.seq
	})
	// 阶段三：构造结果切片并完成类型安全赋值。
	items := reflect.MakeSlice(targetValue.Type(), 0, len(definitions))
	for _, definition := range definitions {
		if !definition.instanceValue.Type().AssignableTo(elementType) {
			err = errorForIOCResolveInvalidTarget(messagef("op=ioc.resolve_all result=failed stage=assign reason=type_not_assignable target=%s bean=%s",
				elementType, definition.instanceValue.Type(),
			))
			this.warnFailure(nil, "ioc.resolve_all", err)
			return err
		}
		items = reflect.Append(items, definition.instanceValue)
	}
	targetValue.Set(items)
	return nil
}

// ResolveByName 按 target 声明类型范围内的名称解析单个实例。
//
// 流程：
// 1. 校验容器状态与 name 非空；
// 2. 从 target 得到精确声明类型；
// 3. 通过“声明类型 + 名称”索引查 bean 并完成赋值。
func (this *container) ResolveByName(name string, target any) error {
	// 阶段一：读取锁保护类型名称索引的一致性。
	this.mu.RLock()
	defer this.mu.RUnlock()

	if this.state == stateClosing || this.state == stateClosed {
		reason := "container_closed"
		if this.state == stateClosing {
			reason = "close_running"
		}
		err := errorForIOCContainerClosed(messagef("op=ioc.resolve_by_name result=failed stage=precheck reason=%s", reason))
		this.warnFailure(nil, "ioc.resolve_by_name", err)
		return err
	}
	if this.state != stateBuilt {
		err := errorForIOCContainerNotBuilt(messagef("op=ioc.resolve_by_name result=failed stage=precheck reason=container_not_built"))
		this.warnFailure(nil, "ioc.resolve_by_name", err)
		return err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		err := errorForIOCResolveInvalidTarget(messagef("op=ioc.resolve_by_name result=failed stage=validate_name reason=empty_name"))
		this.warnFailure(nil, "ioc.resolve_by_name", err)
		return err
	}
	targetValue := reflect.ValueOf(target)
	if !targetValue.IsValid() || targetValue.Kind() != reflect.Ptr || targetValue.IsNil() {
		err := errorForIOCResolveInvalidTarget(messagef("op=ioc.resolve_by_name result=failed stage=validate_target reason=target_must_be_non_nil_pointer"))
		this.warnFailure(nil, "ioc.resolve_by_name", err)
		return err
	}
	targetType := targetValue.Elem().Type()
	// 阶段二：名称只在 target 声明类型候选范围内生效。
	definition, ok := this.beansByTypeName[beanKey{declareType: targetType, name: name}]
	if !ok {
		err := errorForIOCResolveNotFound(messagef("op=ioc.resolve_by_name result=failed stage=lookup reason=type_name_not_found type=%s name=%s", targetType, name))
		this.warnFailure(nil, "ioc.resolve_by_name", err)
		return err
	}
	return assignInstance(targetValue.Elem(), definition.instanceValue, targetType, "ioc.resolve_by_name")
}

// Metrics 返回容器运行期指标快照。
func (this *container) Metrics() MetricsSnapshot {
	return this.metrics.snapshot()
}

// Close 按成功构建顺序的逆序关闭容器托管的 Closer，并共享首次关闭结果。
//
// 流程：
// 1. 首个调用切换到 closing，并在锁外逆序执行资源收敛；
// 2. 并发调用等待 closeDone，或只因自身 context 结束而返回；
// 3. 首个调用发布 closeErr 和 closed，后续调用稳定返回同一结果。
//
// 参数：
// - ctx：首个调用用于控制关闭执行；并发 waiter 仅用于控制自身等待；nil 回退 Background。
//
// 返回：
// - 首个事务及正常 waiter 返回共享 closeErr；waiter context 先结束时返回独立等待错误。
func (this *container) Close(ctx context.Context) (result error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// 阶段一：在锁内选出唯一 executor；closing 调用只加入等待，不重复执行 Closer。
	this.mu.Lock()
	switch this.state {
	case stateClosed:
		result = this.closeErr
		this.mu.Unlock()
		return result
	case stateClosing:
		done := this.closeDone
		this.metrics.closeJoinedTotal.Add(1)
		this.mu.Unlock()
		this.logger.Info(ctx, "ioc close joined",
			baselog.Op("ioc.close"), baselog.Result(baselog.ResultRunning),
			baselog.F("stage", "wait"), baselog.Reason("close_running"),
		)
		return this.waitClose(ctx, done)
	case stateBuilding:
		this.mu.Unlock()
		err := errorForIOCInternal(messagef("op=ioc.close result=failed stage=precheck reason=build_is_running"))
		this.warnFailure(ctx, "ioc.close", err)
		return err
	}
	this.metrics.closeTotal.Add(1)
	definitions := append([]*beanDefinition{}, this.initOrder...)
	this.closeDone = make(chan struct{})
	this.state = stateClosing
	this.mu.Unlock()

	// 兜底发布：即使容器内部出现非 Closer panic，也先解除 waiter 阻塞，再向 executor 继续抛出。
	defer func() {
		panicValue := recover()
		if panicValue != nil {
			result = errors.Join(result, errorForIOCCloseFailed(
				messagef("op=ioc.close result=failed stage=execute reason=close_executor_panic"),
				panicValueAsError(panicValue),
			))
		}
		this.completeClose(result)
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	// 阶段二：executor 逆序关闭全部生命周期对象，固定继续并聚合每个失败。
	this.logger.Info(ctx, "ioc close started",
		baselog.Op("ioc.close"), baselog.Result(baselog.ResultRunning),
		baselog.F("stage", "start"), baselog.F("bean_count", len(definitions)),
	)
	result = this.closeDefinitions(ctx, definitions)
	if result != nil {
		this.errorFailure(ctx, "ioc.close", result)
		return result
	}
	this.logger.Info(ctx, "ioc close completed",
		baselog.Op("ioc.close"), baselog.Result(baselog.ResultOK),
		baselog.F("stage", "finish"), baselog.F("bean_count", len(definitions)),
	)
	return nil
}

// waitClose 等待首次关闭事务发布结果，waiter 取消不改变共享 closeErr。
// 阶段角色：并发 Close 的独立等待边界。
func (this *container) waitClose(ctx context.Context, done <-chan struct{}) error {
	// 快速路径：事务已完成时优先返回共享结果，避免已取消 context 覆盖真实关闭结果。
	select {
	case <-done:
		return this.closeResult()
	default:
	}

	select {
	case <-done:
		return this.closeResult()
	case <-ctx.Done():
		// 竞争收口：若完成与取消同时发生，仍优先已发布的共享结果。
		select {
		case <-done:
			return this.closeResult()
		default:
		}
		this.metrics.closeWaitCanceledTotal.Add(1)
		err := errorForIOCCloseFailed(
			messagef("op=ioc.close result=failed stage=wait reason=wait_canceled"),
			ctx.Err(),
		)
		this.warnFailure(ctx, "ioc.close", err)
		return err
	}
}

// completeClose 原子发布首次关闭结果和 closed 终态，再唤醒全部 waiter。
// 阶段角色：Close executor 的唯一提交点。
func (this *container) completeClose(result error) {
	this.mu.Lock()
	this.closeErr = result
	this.state = stateClosed
	close(this.closeDone)
	this.mu.Unlock()
}

// closeResult 读取已经由 closeDone 发布的首次关闭结果。
// 阶段角色：waiter 与重复 Close 的共享结果读取入口。
func (this *container) closeResult() error {
	this.mu.RLock()
	defer this.mu.RUnlock()
	return this.closeErr
}

// applyRegisterOptions 归并 RegisterOption，并根据最终生命周期所有权补齐注入默认值。
// 阶段角色：注册入口的参数标准化。
func (this *container) applyRegisterOptions(options ...RegisterOption) registerOptions {
	result := registerOptions{injectEnable: true, managedLifecycle: true}
	for _, option := range options {
		if option == nil {
			continue
		}
		option(&result)
	}
	// borrowed 对象由外部构造，未明确授权时不修改其字段；显式 WithInject 始终优先。
	if !result.managedLifecycle && !result.injectConfigured {
		result.injectEnable = false
	}
	return result
}

// resolveName 生成 bean 的唯一注册名称。
// 阶段角色：注册阶段构造按名解析和多候选字段分流使用的稳定标识。
func (this *container) resolveName(beanType reflect.Type, name string) (string, error) {
	// 显式名称优先，缺省时按具体类型推导。
	name = strings.TrimSpace(name)
	if name == "" {
		name = this.defaultName(beanType)
	}
	if name == "" {
		return "", errorForIOCRegisterInvalidBean(messagef("op=ioc.register result=failed stage=resolve_name reason=empty_default_name type=%s", beanType))
	}
	return name, nil
}

// resolveDeclares 解析声明类型并验证 bean 可赋值语义。
//
// 流程：
// 1. 默认把 bean 自身类型作为声明；
// 2. 对每个泛型声明按接口实现或 assignable 规则校验；
// 3. 追加并去重全部有效声明类型。
func (this *container) resolveDeclares(beanType reflect.Type, declares []reflect.Type) ([]reflect.Type, error) {
	// 阶段一：默认声明包含 bean 自身类型。
	result := []reflect.Type{beanType}
	for _, declareType := range declares {
		// 阶段二：逐项校验泛型声明的可赋值语义。
		if declareType.Kind() == reflect.Interface {
			if !beanType.Implements(declareType) {
				return nil, errorForIOCRegisterInvalidDecl(messagef("op=ioc.register result=failed stage=validate_declare reason=bean_not_implements bean=%s declare=%s",
					beanType, declareType,
				))
			}
			result = append(result, declareType)
			continue
		}
		if !beanType.AssignableTo(declareType) {
			return nil, errorForIOCRegisterInvalidDecl(messagef("op=ioc.register result=failed stage=validate_declare reason=bean_not_assignable bean=%s declare=%s",
				beanType, declareType,
			))
		}
		result = append(result, declareType)
	}
	return uniqueTypes(result), nil
}

// defaultName 返回类型默认名称（去掉指针、包路径和泛型参数）。
// 阶段角色：为未显式命名的 bean 提供可与 Go 字段名匹配的稳定名称。
func (this *container) defaultName(beanType reflect.Type) string {
	// 命名类型优先使用 Name，避免泛型参数中的包限定符干扰外层类型名称。
	for beanType.Kind() == reflect.Ptr {
		beanType = beanType.Elem()
	}
	name := beanType.Name()
	if name == "" {
		// 未命名类型沿用反射字符串，保持原有注册兼容性。
		name = beanType.String()
	}
	if index := strings.IndexByte(name, '['); index >= 0 {
		name = name[:index]
	}
	return strings.TrimSpace(name)
}

// resolveSingleTarget 校验 Resolve 目标，并返回可写目标值与声明类型。
// 阶段角色：解析入口的参数语义校验。
func resolveSingleTarget(target any) (reflect.Value, reflect.Type, error) {
	// 阶段一：校验 target 必须是非空指针。
	targetValue := reflect.ValueOf(target)
	if !targetValue.IsValid() || targetValue.Kind() != reflect.Ptr || targetValue.IsNil() {
		return reflect.Value{}, nil, errorForIOCResolveInvalidTarget(messagef("op=ioc.resolve result=failed stage=validate_target reason=target_must_be_non_nil_pointer"))
	}
	result := targetValue.Elem()
	targetType := result.Type()
	// 阶段二：限制解析目标只允许 interface/ptr。
	if targetType.Kind() != reflect.Interface && targetType.Kind() != reflect.Ptr {
		return reflect.Value{}, nil, errorForIOCResolveInvalidTarget(messagef("op=ioc.resolve result=failed stage=validate_target reason=target_type_must_interface_or_pointer type=%s", targetType))
	}
	return result, targetType, nil
}

// resolveSliceTarget 校验 ResolveAll 目标，并返回可写切片和值类型。
// 阶段角色：批量解析入口的参数语义校验。
func resolveSliceTarget(target any) (reflect.Value, reflect.Type, error) {
	// 阶段一：校验 target 必须是非空切片指针。
	targetValue := reflect.ValueOf(target)
	if !targetValue.IsValid() || targetValue.Kind() != reflect.Ptr || targetValue.IsNil() {
		return reflect.Value{}, nil, errorForIOCResolveInvalidTarget(messagef("op=ioc.resolve_all result=failed stage=validate_target reason=target_must_be_non_nil_pointer"))
	}
	result := targetValue.Elem()
	if result.Kind() != reflect.Slice {
		return reflect.Value{}, nil, errorForIOCResolveInvalidTarget(messagef("op=ioc.resolve_all result=failed stage=validate_target reason=target_must_be_slice_pointer type=%s", result.Type()))
	}
	elementType := result.Type().Elem()
	// 阶段二：限制切片元素只允许 interface/ptr。
	if elementType.Kind() != reflect.Interface && elementType.Kind() != reflect.Ptr {
		return reflect.Value{}, nil, errorForIOCResolveInvalidTarget(messagef("op=ioc.resolve_all result=failed stage=validate_target reason=slice_elem_must_interface_or_pointer type=%s", elementType))
	}
	return result, elementType, nil
}

// assignInstance 把 bean 值写入 target，并校验 assignable 语义。
// 阶段角色：Resolve 家族的最终赋值收口。
func assignInstance(targetValue reflect.Value, instanceValue reflect.Value, targetType reflect.Type, op string) error {
	if !instanceValue.Type().AssignableTo(targetType) {
		return errorForIOCResolveInvalidTarget(messagef("op=%s result=failed stage=assign reason=type_not_assignable target=%s bean=%s",
			op, targetType, instanceValue.Type(),
		))
	}
	targetValue.Set(instanceValue)
	return nil
}

// uniqueTypes 按出现顺序去重声明类型。
// 阶段角色：保证 type 索引稳定且无重复写入。
func uniqueTypes(input []reflect.Type) []reflect.Type {
	result := make([]reflect.Type, 0, len(input))
	seen := make(map[reflect.Type]struct{}, len(input))
	for _, item := range input {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}
