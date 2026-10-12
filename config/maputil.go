package config

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"strings"
)

// toMapFromAny 将任意可 YAML 编码对象转换为 map，便于统一 merge。
func toMapFromAny(value any) (result map[string]any, err error) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		err = errorForConfigInternal(messagef(fmt.Sprintf("marshal input failed: %v", recovered)))
		result = nil
	}()
	bs, err := yaml.Marshal(value)
	if err != nil {
		return nil, errorForConfigMapConvertFailed(messagef("yaml marshal to map failed"), err)
	}
	result = make(map[string]any)
	if err = yaml.Unmarshal(bs, &result); err != nil {
		return nil, errorForConfigMapConvertFailed(messagef("yaml unmarshal to map failed"), err)
	}
	return normalizeMap(result), nil
}

// decodeAnyToTarget 将任意对象通过 YAML 反序列化到目标对象。
func decodeAnyToTarget(input any, target any) error {
	if target == nil {
		return errorForConfigDecodeTargetNil(messagef("target is nil"))
	}
	bs, err := yaml.Marshal(input)
	if err != nil {
		return errorForConfigDecodeFailed(messagef("yaml marshal decode input failed"), err)
	}
	if err = yaml.Unmarshal(bs, target); err != nil {
		return errorForConfigDecodeFailed(messagef("yaml unmarshal decode target failed"), err)
	}
	return nil
}

// lookupByDottedKey 在嵌套 map 中按 dotted key 查找值。
func lookupByDottedKey(input map[string]any, dottedKey string) (any, bool) {
	dottedKey = strings.TrimSpace(dottedKey)
	if dottedKey == "" {
		return input, true
	}
	parts := strings.Split(dottedKey, ".")
	var cursor any = input
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		child, ok := cursor.(map[string]any)
		if !ok {
			return nil, false
		}
		next, exists := child[part]
		if !exists {
			return nil, false
		}
		cursor = next
	}
	return cursor, true
}

// normalizeMap 递归清洗 map，保证 key 均为 string。
func normalizeMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = normalizeValue(value)
	}
	return result
}

// normalizeValue 递归清洗 YAML 反序列化结果中的 map/list。
func normalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return normalizeMap(typed)
	case map[any]any:
		converted := make(map[string]any, len(typed))
		for key, inner := range typed {
			converted[fmt.Sprint(key)] = normalizeValue(inner)
		}
		return converted
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, normalizeValue(item))
		}
		return result
	default:
		return value
	}
}

// deepCopyMap 递归复制 map，避免外部修改内部快照。
func deepCopyMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = deepCopyValue(value)
	}
	return result
}

// deepCopyValue 递归复制值。
func deepCopyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return deepCopyMap(typed)
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, deepCopyValue(item))
		}
		return result
	case []byte:
		return append([]byte(nil), typed...)
	default:
		return typed
	}
}

// flattenMap 将嵌套 map 展平为 dotted key 形式。
func flattenMap(input map[string]any) map[string]any {
	result := make(map[string]any)
	flattenMapInto("", input, result)
	return result
}

// flattenMapInto 递归展平 map。
func flattenMapInto(prefix string, input map[string]any, output map[string]any) {
	for key, value := range input {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}
		if inner, ok := value.(map[string]any); ok {
			flattenMapInto(fullKey, inner, output)
			continue
		}
		output[fullKey] = value
	}
}

// setByDottedKey 按 dotted key 写入嵌套 map。
func setByDottedKey(target map[string]any, dottedKey string, value any) {
	parts := strings.Split(dottedKey, ".")
	if len(parts) == 0 {
		return
	}
	cursor := target
	for index := 0; index < len(parts)-1; index++ {
		part := strings.TrimSpace(parts[index])
		if part == "" {
			return
		}
		next, ok := cursor[part]
		if !ok {
			child := make(map[string]any)
			cursor[part] = child
			cursor = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			child = make(map[string]any)
			cursor[part] = child
		}
		cursor = child
	}
	last := strings.TrimSpace(parts[len(parts)-1])
	if last == "" {
		return
	}
	cursor[last] = value
}
