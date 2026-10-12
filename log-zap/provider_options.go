package zap

import baselog "github.com/yangzhuangqiu/gobase/log"

// providerOptions 是 NewProvider 的内部可选参数容器。
// 阶段角色：完成 Option 聚合后的不可变快照输入。
type providerOptions struct {
	// contextExtractors 在 Factory 构造期固化，按注册顺序提取调用链字段。
	contextExtractors []baselog.ContextExtractor
	// redactor 在 Factory 统一边界脱敏全部业务字段。
	redactor baselog.Redactor
}

// Option 定义 NewProvider 的可选参数。
type Option func(*providerOptions)

// WithContextExtractors 为 provider 的 LoggerFactory 注册类型安全调用链字段提取器。
// 提取器仅在目标日志级别启用时执行，并在 Provider 生命周期内保持不变。
func WithContextExtractors(extractors ...baselog.ContextExtractor) Option {
	return func(options *providerOptions) {
		options.contextExtractors = append(options.contextExtractors, extractors...)
	}
}

// WithRedactor 为 provider 注册字段脱敏策略。
// 策略必须并发安全，并在 Provider 生命周期内保持不变。
func WithRedactor(redactor baselog.Redactor) Option {
	return func(options *providerOptions) {
		options.redactor = redactor
	}
}

// resolveProviderOptions 归一化可选参数并补齐默认值。
//
// 语义：
// 1. 过滤 nil option；
// 2. 按声明顺序叠加到最终参数，后声明 option 可覆盖前值。
func resolveProviderOptions(options []Option) providerOptions {
	result := providerOptions{}
	for _, option := range options {
		if option == nil {
			continue
		}
		option(&result)
	}
	return result
}
