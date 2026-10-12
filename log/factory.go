package log

import (
	"log/slog"
	"strings"
)

// LoggerFactory 定义“已绑定日志出口”的 logger 工厂契约。
//
// 典型调用场景:
// 1. 装配层创建一次 factory 并注入业务组件；
// 2. 业务组件通过 Get(name) 获取稳定、低基数的组件 logger。
type LoggerFactory interface {
	// Get 按稳定名称返回 logger。
	//
	// 参数：
	// - name: 组件语义名；trim 后为空时归一化为 root。
	//
	// 返回：
	// - Logger: 始终非 nil；同名只保证行为一致，不保证实例身份。
	Get(name string) Logger
}

type handlerLoggerFactory struct {
	// handler 是已完成 provider 降级与 context 装饰的共享事件出口。
	handler slog.Handler
}

// factoryOptions 是 factory option 聚合后的内部配置快照。
type factoryOptions struct {
	// contextExtractors 按注册顺序从调用链提取低风险字段。
	contextExtractors []ContextExtractor
	// redactor 在统一 Handler 边界处理全部字段来源。
	redactor Redactor
}

// FactoryOption 定义 LoggerFactory 的可选装配策略。
type FactoryOption func(*factoryOptions)

// WithContextExtractors 注册类型安全的调用链字段提取器。
// 提取器在 level 已启用后执行，并对标准降级与所有 provider 一致生效。
func WithContextExtractors(extractors ...ContextExtractor) FactoryOption {
	return func(options *factoryOptions) {
		options.contextExtractors = append(options.contextExtractors, extractors...)
	}
}

// WithRedactor 注册 Provider 级字段脱敏策略。
// 固定字段在 With/Get 阶段处理；单次事件与 ContextExtractor 字段仅在 level 启用后处理。
func WithRedactor(redactor Redactor) FactoryOption {
	return func(options *factoryOptions) {
		options.redactor = redactor
	}
}

// NewLoggerFactory 基于标准 Handler 创建日志工厂。
//
// 语义：
// 1. 该入口主要供 provider 实现者装配，业务应使用 Provider.Factory；
// 2. handler 为 nil 时回退标准 text Handler；
// 3. context extractor 在统一 Handler 边界装饰；
// 4. factory 不缓存 Logger；同一名称只保证相同行为，不保证实例身份。
func NewLoggerFactory(handler slog.Handler, options ...FactoryOption) LoggerFactory {
	// 第一步：聚合非空 option，形成 factory 的不可变装配参数。
	resolved := factoryOptions{}
	for _, option := range options {
		if option != nil {
			option(&resolved)
		}
	}
	resolvedHandler := handler
	// 第二步：Handler 缺失时使用标准降级出口，保证 factory 永不返回 nil Logger。
	if resolvedHandler == nil {
		resolvedHandler = newFallbackHandler()
	}
	// 第三步：依次应用 schema、脱敏与调用链字段装饰，再创建命名 logger 工厂。
	resolvedHandler = newSchemaHandler(resolvedHandler)
	resolvedHandler = newRedactingHandler(resolvedHandler, resolved.redactor)
	resolvedHandler = newContextHandler(resolvedHandler, resolved.contextExtractors)
	return &handlerLoggerFactory{handler: resolvedHandler}
}

// Get 按归一化名称创建 Logger。
// 阶段角色：统一名称与固定 logger 字段，provider 不再参与命名策略。
func (this *handlerLoggerFactory) Get(name string) Logger {
	loggerName := normalizeLoggerName(name)
	return newEventLogger(this.handler.WithAttrs([]slog.Attr{slog.String("logger", loggerName)}))
}

// normalizeLoggerName 归一化显式 logger 名称并兜底 root。
func normalizeLoggerName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return "root"
	}
	return trimmed
}
