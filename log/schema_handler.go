package log

import (
	"context"
	"log/slog"
	"strings"
)

// reservedEnvelopeKeys 是 provider 不得被业务顶层字段覆盖的标准事件键集合。
var reservedEnvelopeKeys = map[string]struct{}{
	"caller":     {},
	"func":       {},
	"level":      {},
	"msg":        {},
	"name":       {},
	"stacktrace": {},
	"ts":         {},
}

// schemaHandler 在 provider 之前执行顶层字段键约束与确定性去重。
// 阶段角色：保护标准事件 envelope，避免 JSON 出现语义不确定的重复键。
type schemaHandler struct {
	// next 接收已经通过 schema 约束的 Attr。
	next slog.Handler
	// attrs 保存已转换的固定字段，事件出口统一参与递归归一化。
	attrs []slog.Attr
	// groups 保存 WithGroup 路径；非顶层不应用 envelope 保留键规则。
	groups []string
}

// newSchemaHandler 创建根字段 schema 边界。
func newSchemaHandler(next slog.Handler) slog.Handler {
	return &schemaHandler{next: next}
}

// Enabled 透传 level 判定。
func (this *schemaHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return this.next.Enabled(ctx, level)
}

// Handle 合并固定字段与事件字段，递归保护 envelope 并执行 first-wins 去重。
func (this *schemaHandler) Handle(ctx context.Context, record slog.Record) error {
	// 第一步：按声明顺序汇集固定字段和当前 Record 字段。
	attrs := make([]slog.Attr, 0, len(this.attrs)+record.NumAttrs())
	attrs = append(attrs, this.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, wrapSchemaAttrWithGroups(attr, this.groups))
		return true
	})
	// 第二步：递归归一化后重建 Record，确保下游只观察到 canonical 字段。
	result := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	result.AddAttrs(normalizeSchemaAttrs(attrs, true)...)
	return this.next.Handle(ctx, result)
}

// WithAttrs 保存已转换固定字段，由事件出口统一处理跨来源冲突。
func (this *schemaHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	result := &schemaHandler{next: this.next}
	result.attrs = append(result.attrs, this.attrs...)
	for _, attr := range attrs {
		result.attrs = append(result.attrs, wrapSchemaAttrWithGroups(attr, this.groups))
	}
	result.groups = append(result.groups, this.groups...)
	return result
}

// WithGroup 派生字段组路径，固定字段仍保持绑定时所在的原路径。
func (this *schemaHandler) WithGroup(name string) slog.Handler {
	name = strings.TrimSpace(name)
	if name == "" {
		return this
	}
	result := &schemaHandler{next: this.next}
	result.attrs = append(result.attrs, this.attrs...)
	result.groups = append(result.groups, this.groups...)
	result.groups = append(result.groups, name)
	return result
}

// wrapSchemaAttrWithGroups 把 Handler 当前组路径固化到单个 Attr。
func wrapSchemaAttrWithGroups(attr slog.Attr, groups []string) slog.Attr {
	result := attr
	for index := len(groups) - 1; index >= 0; index-- {
		result = slog.Attr{Key: groups[index], Value: slog.GroupValue(result)}
	}
	return result
}

// normalizeSchemaAttrs 递归合并同名 Group，并让同层标量字段保持 first-wins。
func normalizeSchemaAttrs(attrs []slog.Attr, root bool) []slog.Attr {
	result := make([]slog.Attr, 0, len(attrs))
	indexes := make(map[string]int, len(attrs))
	for _, attr := range attrs {
		attr.Key = strings.TrimSpace(attr.Key)
		if attr.Key == "" {
			continue
		}
		if root {
			if _, reserved := reservedEnvelopeKeys[attr.Key]; reserved {
				continue
			}
		}
		attr.Value = attr.Value.Resolve()
		if attr.Value.Kind() == slog.KindGroup {
			children := normalizeSchemaAttrs(attr.Value.Group(), false)
			if len(children) == 0 {
				continue
			}
			attr.Value = slog.GroupValue(children...)
		}
		if index, duplicate := indexes[attr.Key]; duplicate {
			// 只有 Group 与 Group 可以合并；其它冲突保留最早声明值。
			if result[index].Value.Kind() == slog.KindGroup && attr.Value.Kind() == slog.KindGroup {
				children := append([]slog.Attr{}, result[index].Value.Group()...)
				children = append(children, attr.Value.Group()...)
				result[index].Value = slog.GroupValue(normalizeSchemaAttrs(children, false)...)
			}
			continue
		}
		indexes[attr.Key] = len(result)
		result = append(result, attr)
	}
	return result
}
