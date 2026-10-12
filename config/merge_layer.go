package config

import (
	"fmt"
	"sort"
)

// mergeLayer 描述一次 merge 的输入层。
type mergeLayer struct {
	Name     string
	Source   SourceKind
	Priority int
	Data     map[string]any
	Pinned   bool
}

// mergeEntryMeta 记录某个 key 在最终值中的来源。
type mergeEntryMeta struct {
	Layer    string
	Source   SourceKind
	Priority int
	Pinned   bool
}

// combineLayers 按优先级合并多层配置并产出来源元数据。
func combineLayers(layers ...mergeLayer) (map[string]any, map[string]mergeEntryMeta) {
	ordered := make([]mergeLayer, 0, len(layers))
	ordered = append(ordered, layers...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Priority < ordered[j].Priority
	})

	merged := make(map[string]any)
	// candidates 记录每个叶子 key 最近一次明确写入者；最终仅输出仍存在的 key，
	// 避免 map/标量结构互换后残留已被覆盖子树的来源元数据。
	candidates := make(map[string]mergeEntryMeta)
	for _, layer := range ordered {
		normalized := mergeNormalizeMap(layer.Data)
		merged = mergeDeepMerge(merged, normalized)
		flat := mergeFlattenMap(normalized)
		for key := range flat {
			candidates[key] = mergeEntryMeta{
				Layer:    layer.Name,
				Source:   layer.Source,
				Priority: layer.Priority,
				Pinned:   layer.Pinned,
			}
		}
	}

	finalFlat := mergeFlattenMap(merged)
	meta := make(map[string]mergeEntryMeta, len(finalFlat))
	for key := range finalFlat {
		if item, ok := candidates[key]; ok {
			meta[key] = item
		}
	}
	return merged, meta
}

// mergeNormalizeMap 递归清洗 map，确保 key 是 string。
func mergeNormalizeMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = mergeNormalizeValue(value)
	}
	return result
}

// mergeNormalizeValue 递归转换 YAML 解析后的对象。
func mergeNormalizeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return mergeNormalizeMap(typed)
	case map[any]any:
		converted := make(map[string]any, len(typed))
		for key, inner := range typed {
			converted[mergeToString(key)] = mergeNormalizeValue(inner)
		}
		return converted
	case []any:
		result := make([]any, 0, len(typed))
		for _, item := range typed {
			result = append(result, mergeNormalizeValue(item))
		}
		return result
	default:
		return value
	}
}

// mergeToString 将任意 key 转成字符串。
func mergeToString(value any) string {
	return fmt.Sprint(value)
}

// mergeDeepMerge 将 overlay 覆盖到 base 上并返回新 map。
func mergeDeepMerge(base, overlay map[string]any) map[string]any {
	result := deepCopyMap(base)
	for key, value := range overlay {
		if nestedOverlay, ok := value.(map[string]any); ok {
			nestedBase, _ := result[key].(map[string]any)
			result[key] = mergeDeepMerge(nestedBase, nestedOverlay)
			continue
		}
		result[key] = deepCopyValue(value)
	}
	return result
}

// mergeFlattenMap 将嵌套 map 转为 dotted key。
func mergeFlattenMap(input map[string]any) map[string]any {
	output := make(map[string]any)
	mergeFlattenMapInto("", input, output)
	return output
}

// mergeFlattenMapInto 递归展平 map。
func mergeFlattenMapInto(prefix string, input map[string]any, output map[string]any) {
	for key, value := range input {
		full := key
		if prefix != "" {
			full = prefix + "." + key
		}
		if nested, ok := value.(map[string]any); ok {
			mergeFlattenMapInto(full, nested, output)
			continue
		}
		output[full] = value
	}
}
