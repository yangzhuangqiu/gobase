package log

import (
	"log/slog"
	"strings"
)

// Field 表示结构化日志事件中的单个业务字段。
type Field struct {
	// Key 是稳定、可检索的字段名；空白名称会在构造阶段归一化为空字段。
	Key string
	// Value 是字段原始业务值；provider 负责最终编码，不应包含未脱敏凭据。
	Value any
}

// fieldGroup 表示一组具有共同父键的嵌套结构化字段。
// 元素顺序与声明顺序一致，便于输出和测试保持确定性。
type fieldGroup []Field

// F 创建普通结构化字段。
// 阶段角色：业务调用点声明可检索维度，避免把机器字段拼入 message。
func F(key string, value any) Field {
	return Field{Key: strings.TrimSpace(key), Value: value}
}

// Err 创建固定键为 error 的错误字段。
// nil error 返回零值字段，Logger 会忽略该字段，便于调用方直接透传可选错误。
func Err(err error) Field {
	if err == nil {
		return Field{}
	}
	return Field{Key: "error", Value: err}
}

// Group 创建嵌套字段组。
// 输入字段切片会复制为结构快照；Field.Value 中的引用值不做深复制，仍由调用方管理。
func Group(key string, fields ...Field) Field {
	result := make(fieldGroup, len(fields))
	copy(result, fields)
	return Field{Key: strings.TrimSpace(key), Value: result}
}

// fieldsToSlogAttrs 把业务字段批量转换为标准 Handler 属性。
// 阶段角色：仅在 level 已启用后进入事件编码准备阶段。
func fieldsToSlogAttrs(fields []Field) []slog.Attr {
	if len(fields) == 0 {
		return nil
	}
	result := make([]slog.Attr, 0, len(fields))
	for _, field := range fields {
		attr, ok := fieldToSlogAttr(field)
		if !ok {
			continue
		}
		result = append(result, attr)
	}
	return result
}

// fieldToSlogAttr 转换单个字段并递归处理 Group。
// 阶段角色：保持根契约 Field 与标准 slog.Record 的唯一映射规则。
func fieldToSlogAttr(field Field) (slog.Attr, bool) {
	key := strings.TrimSpace(field.Key)
	if key == "" {
		return slog.Attr{}, false
	}
	group, ok := field.Value.(fieldGroup)
	if !ok {
		return slog.Any(key, field.Value), true
	}
	attrs := fieldsToSlogAttrs(group)
	return slog.Attr{Key: key, Value: slog.GroupValue(attrs...)}, true
}
