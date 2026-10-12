package appruntime

import (
	"errors"
	"reflect"
	"strings"
	"sync/atomic"

	"github.com/yangzhuangqiu/gobase/config"
)

// ConfigValue 提供 app-runtime IOC 场景的 typed 配置视图。
//
// 典型调用场景：业务 bean 注入 *ConfigValue[T]，并在请求路径调用 Load 读取最新合法配置。
// ConfigValue 通过 Observer pull 最新 Snapshot；读取异常时保留最后一次成功值。
type ConfigValue[T any] struct {
	// observer 绑定本次 Runtime 的 Manager 与 scope，不持有独立订阅资源。
	observer *config.Observer[T]
	// current 保存最近一次成功读取值，保证 Load 不向业务传播瞬时解码错误。
	current atomic.Pointer[T]
}

// Load 返回最新一次成功读取的 typed 配置浅拷贝。
// 配置候选已在发布前通过 scoped validator；异常实现导致读取失败时保留旧值。
func (this *ConfigValue[T]) Load() T {
	if this == nil {
		var zero T
		return zero
	}
	if this.observer != nil {
		if current, err := this.observer.Current(); err == nil {
			this.current.Store(&current)
		}
	}
	current := this.current.Load()
	if current == nil {
		var zero T
		return zero
	}
	return *current
}

// configBinding 定义 app-runtime 内部 typed 配置装配契约。
type configBinding interface {
	// snapshotValidator 返回发布前 scope 解码和业务校验器。
	snapshotValidator() config.SnapshotValidator
	// build 为单次 Runtime 创建独立 typed 配置视图。
	build(manager config.Manager) (boundConfigValue, error)
}

// boundConfigValue 描述待注册 IOC 的单次 Runtime typed 配置 bean。
type boundConfigValue struct {
	// Name 是 IOC 注册和诊断使用的稳定名称。
	Name string
	// Scope 是该值绑定的 dotted 配置路径。
	Scope string
	// Value 是具体的 *ConfigValue[T]。
	Value any
}

// typedConfigBinding 保存可跨 Build 重放的 typed 配置声明。
type typedConfigBinding[T any] struct {
	// name 是由 scope 和 T 生成的稳定 IOC bean 名称。
	name string
	// scope 是绑定的 dotted 配置路径，空字符串表示完整 Snapshot。
	scope string
	// validators 是按声明顺序执行的 typed 业务约束。
	validators []func(*T) []error
}

// WithConfigBinding 声明由 app-runtime 创建并注入 IOC 的 typed 配置视图。
//
// 参数：
// - scope：dotted 配置路径；空字符串表示完整 Snapshot；
// - validators：可选 typed 校验器，按声明顺序全部执行并聚合错误。
//
// 返回：
// - Option：每次 Application.Build 创建独立 *ConfigValue[T]。
func WithConfigBinding[T any](scope string, validators ...func(*T) []error) Option {
	scope = strings.TrimSpace(scope)
	targetType := reflect.TypeOf((*T)(nil)).Elem()
	declaration := &typedConfigBinding[T]{
		name:  "ConfigValue[" + scope + ":" + targetType.String() + "]",
		scope: scope,
	}
	for _, validator := range validators {
		if validator != nil {
			declaration.validators = append(declaration.validators, validator)
		}
	}
	return func(application *Application) {
		application.configBindings = append(application.configBindings, declaration)
	}
}

// snapshotValidator 在 config 发布前验证 scope 可解码，并执行全部 typed 业务约束。
func (this *typedConfigBinding[T]) snapshotValidator() config.SnapshotValidator {
	return func(snapshot config.Snapshot) error {
		var value T
		if err := snapshot.Decode(this.scope, &value); err != nil {
			return err
		}
		var errs []error
		for _, validator := range this.validators {
			errs = append(errs, validator(&value)...)
		}
		return errors.Join(errs...)
	}
}

// build 从本次 Runtime Manager 创建 Observer，并固化初始合法值。
func (this *typedConfigBinding[T]) build(manager config.Manager) (boundConfigValue, error) {
	observer, err := config.Observe[T](manager, this.scope)
	if err != nil {
		return boundConfigValue{}, err
	}
	initial, err := observer.Current()
	if err != nil {
		return boundConfigValue{}, err
	}
	value := &ConfigValue[T]{observer: observer}
	value.current.Store(&initial)
	return boundConfigValue{Name: this.name, Scope: this.scope, Value: value}, nil
}

// buildConfigValues 创建本次 Runtime 的全部 typed 配置 bean。
// 阶段角色：Manager 首次 Snapshot 发布后、logging 和 IOC 构建前完成 pull 视图初始化。
func (this *Application) buildConfigValues(manager config.Manager) ([]boundConfigValue, error) {
	result := make([]boundConfigValue, 0, len(this.configBindings))
	for index, binding := range this.configBindings {
		value, err := binding.build(manager)
		if err != nil {
			return nil, errorForBuildFailed(messagef("op=app_runtime.config_binding result=failed stage=build binding_index=%d", index), err)
		}
		result = append(result, value)
	}
	return result, nil
}
