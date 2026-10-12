package log

import "strings"

const (
	// RedactedValue 是内建键脱敏器使用的固定替换文本，避免输出原始敏感值。
	RedactedValue = "[REDACTED]"
)

// Redactor 定义日志字段值脱敏契约。
//
// 典型调用场景:
// 1. Provider 构造期注册一次并应用到其全部 Logger；
// 2. 按字段路径识别 token、password 等敏感字段并返回安全替代值。
//
// 语义：实现必须并发安全，不得记录或持久化传入的原始值。
type Redactor interface {
	// Redact 返回字段的安全输出值。
	//
	// 参数：
	// - path: 从顶层到当前字段的完整路径；调用期间只读；
	// - value: 字段解析后的原始值。
	//
	// 返回：
	// - any: 可安全交给 provider 编码的值；返回 nil 表示保留字段但输出 null。
	Redact(path []string, value any) any
}

// RedactorFunc 把函数适配为并发安全的 Redactor；并发安全责任由函数实现者承担。
type RedactorFunc func(path []string, value any) any

// Redact 调用脱敏函数；nil 函数保持原值，便于可选装配。
func (this RedactorFunc) Redact(path []string, value any) any {
	if this == nil {
		return value
	}
	return this(path, value)
}

// keyRedactor 按叶子键或 dotted 完整路径执行大小写不敏感匹配。
type keyRedactor struct {
	// keys 保存已标准化的敏感字段键；构造完成后只读。
	keys map[string]struct{}
}

// NewKeyRedactor 创建固定替换为 RedactedValue 的字段键脱敏器。
//
// 参数：
// - keys: 支持叶子键（如 token）或 dotted 路径（如 auth.token）；忽略大小写与首尾空白。
//
// 返回：
// - Redactor: 无有效键时仍返回安全的无操作实现。
func NewKeyRedactor(keys ...string) Redactor {
	result := &keyRedactor{keys: make(map[string]struct{}, len(keys))}
	for _, key := range keys {
		normalized := normalizeRedactionKey(key)
		if normalized != "" {
			result.keys[normalized] = struct{}{}
		}
	}
	return result
}

// Redact 在叶子键或完整路径命中时返回固定遮罩，否则保持原值。
func (this *keyRedactor) Redact(path []string, value any) any {
	if this == nil || len(path) == 0 {
		return value
	}
	leaf := normalizeRedactionKey(path[len(path)-1])
	fullPath := normalizeRedactionKey(strings.Join(path, "."))
	if _, exists := this.keys[leaf]; exists {
		return RedactedValue
	}
	if _, exists := this.keys[fullPath]; exists {
		return RedactedValue
	}
	return value
}

// normalizeRedactionKey 统一敏感键比较格式。
func normalizeRedactionKey(key string) string {
	return strings.ToLower(strings.TrimSpace(key))
}
