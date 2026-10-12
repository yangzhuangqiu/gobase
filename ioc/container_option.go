package ioc

import (
	"time"

	baselog "github.com/yangzhuangqiu/gobase/log"
)

// Option 定义 New 的容器级可选参数。
//
// 语义：
// 1. Option 只影响容器运行行为，不影响 bean 注入语义；
// 2. 典型能力是日志注入、单 bean Close timeout 与循环依赖策略控制。
type Option func(this *container)

// WithLogger 覆盖容器日志实现；传 nil 会回退到默认 logger。
func WithLogger(logger baselog.Logger) Option {
	return func(this *container) {
		this.logger = logger
	}
}

// WithCloseTimeout 设置单个 bean Close 超时时间；小于等于 0 表示不启用超时控制。
func WithCloseTimeout(timeout time.Duration) Option {
	return func(this *container) {
		this.closeTimeout = timeout
	}
}

// WithAllowCircularDependency 设置是否允许循环依赖。
//
// 语义：
// 1. 默认 false：保持强语义，Build 检测到循环依赖立即失败；
// 2. true：切换弱约束模式，循环依赖时降级为“按注册顺序”执行注入与 Init。
func WithAllowCircularDependency(enable bool) Option {
	return func(this *container) {
		this.allowCircular = enable
	}
}
