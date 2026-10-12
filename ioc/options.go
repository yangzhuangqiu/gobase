package ioc

import (
	"reflect"
	"strings"
)

// RegisterOption 描述 Register 的可选参数。
type RegisterOption func(options *registerOptions)

// WithName 设置 bean 在各声明类型范围内共享的唯一名称，供按名解析和多候选字段分流。
//
// 语义：
// 1. 未设置或传入空白名称时，由容器根据具体类型生成默认名称；
// 2. 同一次 Register 中重复调用时，后设置的名称覆盖前者，不会形成多个名称索引。
// 3. 不同声明类型可以使用相同名称；同一声明类型下名称冲突时注册失败。
func WithName(name string) RegisterOption {
	return func(options *registerOptions) {
		options.name = strings.TrimSpace(name)
	}
}

// WithDeclare 追加 bean 的声明类型，供字段注入和按类型解析建立索引。
//
// 语义：
// 1. 可重复调用，声明类型会累积并去重；
// 2. 所有声明类型指向同一个 bean，不会创建副本或重复生命周期；
// 3. T 可以是接口、指针或其他 bean 可赋值到的具体类型。
func WithDeclare[T any]() RegisterOption {
	return func(options *registerOptions) {
		options.declares = append(options.declares, reflect.TypeOf((*T)(nil)).Elem())
	}
}

// WithInject 显式控制 bean 是否参与 Build 阶段字段注入。
//
// 语义：
// 1. managed bean 默认启用字段注入；
// 2. borrowed bean 默认禁用字段注入，只有显式传入 true 才启用；
// 3. 显式配置优先于生命周期所有权推导出的默认值，且不受 Option 顺序影响。
func WithInject(enable bool) RegisterOption {
	return func(options *registerOptions) {
		options.injectEnable = enable
		options.injectConfigured = true
	}
}

// WithBorrowed 声明 bean 的生命周期由容器外部所有者管理。
//
// 典型调用场景：
// 1. 注册由 app-runtime、父容器或第三方 SDK 创建并关闭的对象；
// 2. 对象需要被其他 bean 注入或解析，但当前容器不应调用它的 Init/Close。
//
// 语义：
// 1. borrowed bean 仍参与注册、类型索引和解析；
// 2. 容器跳过该 bean 的 Initializer/Closer，并默认不修改其字段；
// 3. 如需字段注入，必须另外显式设置 WithInject(true)；
// 4. 未使用本 Option 的 bean 默认由容器托管生命周期。
func WithBorrowed() RegisterOption {
	return func(options *registerOptions) {
		options.managedLifecycle = false
	}
}
