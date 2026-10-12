package log

import (
	"context"
	"log/slog"
)

// ContextExtractor 定义从调用链安全提取结构化字段的契约。
// 应用装配层负责使用 typed context key，并确保不返回 token、凭据等敏感值。
type ContextExtractor func(context.Context) []Field

// contextHandler 在统一 Handler 边界应用 context 字段提取器。
// 阶段角色：保证标准降级与所有外部 provider 共享相同 context 语义。
type contextHandler struct {
	// next 是接收已补充调用链字段的下游 Handler。
	next slog.Handler
	// extractors 按装配顺序执行，且仅在事件 level 已启用后执行。
	extractors []ContextExtractor
}

// newContextHandler 创建 context 字段装饰器。
// 无有效 extractor 时直接返回原 Handler，避免无意义热路径层级。
func newContextHandler(next slog.Handler, extractors []ContextExtractor) slog.Handler {
	// 第一步：过滤空提取器，避免运行期调用 nil 函数。
	filtered := make([]ContextExtractor, 0, len(extractors))
	for _, extractor := range extractors {
		if extractor != nil {
			filtered = append(filtered, extractor)
		}
	}
	if len(filtered) == 0 {
		return next
	}
	// 第二步：仅在确有提取规则时增加一层 Handler 装饰。
	return &contextHandler{next: next, extractors: filtered}
}

// Enabled 透传 level 判定，避免禁用事件执行 extractor。
func (this *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return this.next.Enabled(ctx, level)
}

// Handle 提取 context 字段并附加到 Record 后交给下游。
// Record 在修改前 Clone，避免共享底层状态污染其它 Handler。
func (this *contextHandler) Handle(ctx context.Context, record slog.Record) error {
	result := record.Clone()
	for _, extractor := range this.extractors {
		result.AddAttrs(fieldsToSlogAttrs(extractor(ctx))...)
	}
	return this.next.Handle(ctx, result)
}

// WithAttrs 派生携带固定字段且保留 extractor 的 Handler。
func (this *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{next: this.next.WithAttrs(attrs), extractors: this.extractors}
}

// WithGroup 派生字段组且保留 extractor 的 Handler。
func (this *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{next: this.next.WithGroup(name), extractors: this.extractors}
}
