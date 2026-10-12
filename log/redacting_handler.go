package log

import (
	"context"
	"log/slog"
	"strings"
)

// redactingHandler 在统一事件出口递归脱敏全部 Attr。
// 阶段角色：位于 ContextExtractor 之后、schema/provider 之前，覆盖所有字段来源。
type redactingHandler struct {
	// next 接收已脱敏字段。
	next slog.Handler
	// redactor 是 Provider 构造期固定的并发安全策略。
	redactor Redactor
	// groups 保存 WithGroup 形成的当前字段路径。
	groups []string
}

// newRedactingHandler 仅在存在策略时增加脱敏层。
func newRedactingHandler(next slog.Handler, redactor Redactor) slog.Handler {
	if redactor == nil {
		return next
	}
	return &redactingHandler{next: next, redactor: redactor}
}

// Enabled 透传 level 判定，确保禁用事件不执行脱敏策略。
func (this *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return this.next.Enabled(ctx, level)
}

// Handle 重建 Record 并递归替换其中的敏感字段值。
func (this *redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	result := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		result.AddAttrs(this.redactAttr(attr, this.groups))
		return true
	})
	return this.next.Handle(ctx, result)
}

// WithAttrs 在固定字段绑定阶段完成一次脱敏，避免每条事件重复处理。
func (this *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, 0, len(attrs))
	for _, attr := range attrs {
		redacted = append(redacted, this.redactAttr(attr, this.groups))
	}
	return &redactingHandler{
		next:     this.next.WithAttrs(redacted),
		redactor: this.redactor,
		groups:   append([]string{}, this.groups...),
	}
}

// WithGroup 派生字段路径；空名称不改变当前 Handler。
func (this *redactingHandler) WithGroup(name string) slog.Handler {
	name = strings.TrimSpace(name)
	if name == "" {
		return this
	}
	return &redactingHandler{
		next:     this.next.WithGroup(name),
		redactor: this.redactor,
		groups:   append(append([]string{}, this.groups...), name),
	}
}

// redactAttr 递归处理 Group，并把完整字段路径交给脱敏策略。
func (this *redactingHandler) redactAttr(attr slog.Attr, parentPath []string) slog.Attr {
	// 第一步：解析值并构造当前字段的完整路径。
	attr.Key = strings.TrimSpace(attr.Key)
	attr.Value = attr.Value.Resolve()
	path := append([]string{}, parentPath...)
	if attr.Key != "" {
		path = append(path, attr.Key)
	}
	redactedValue := this.redactor.Redact(path, attr.Value.Any())
	children, preserveGroup := redactedValue.([]slog.Attr)
	if attr.Value.Kind() != slog.KindGroup || !preserveGroup {
		return slog.Any(attr.Key, redactedValue)
	}
	// 第二步：策略保留 Group 时，继续递归处理其中的叶子字段。
	redacted := make([]slog.Attr, 0, len(children))
	for _, child := range children {
		redacted = append(redacted, this.redactAttr(child, path))
	}
	return slog.Attr{Key: attr.Key, Value: slog.GroupValue(redacted...)}
}
