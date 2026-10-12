package provider

import (
	"os"
	"strconv"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// LoadBootstrapFromEnvBySchema 按 schema 自动把环境变量映射为 bootstrap patch。
//
// 语义：
// 1. 每个 schema key 默认映射为 `<PREFIX><KEY>` 形式的 env key；
// 2. key 中 `.`/`-` 会转为 `_`，并统一大写；
// 3. camelCase 会转为 SNAKE_CASE（例如 timeoutMs -> TIMEOUT_MS）；
// 4. 含 `[]` 的键（数组项）不会自动映射，建议由 BootstrapEnvLoader 补充。
func LoadBootstrapFromEnvBySchema(envPrefix string, schema []BootstrapKey) map[string]any {
	result := make(map[string]any)
	prefix := normalizeEnvPrefix(envPrefix)
	for _, item := range schema {
		dottedKey := strings.TrimSpace(item.Key)
		if dottedKey == "" {
			continue
		}
		if strings.Contains(dottedKey, "[]") {
			continue
		}

		envKey := prefix + bootstrapKeyToEnvToken(dottedKey)
		raw := strings.TrimSpace(os.Getenv(envKey))
		if raw == "" {
			continue
		}

		value := parseBootstrapEnvValue(raw, item.Type)
		setByDottedKey(result, dottedKey, value)
	}
	return result
}

// normalizeEnvPrefix 把前缀统一转换为大写下划线形式。
func normalizeEnvPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return ""
	}
	prefix = strings.ReplaceAll(prefix, "-", "_")
	prefix = strings.ReplaceAll(prefix, ".", "_")
	prefix = strings.ToUpper(prefix)
	if strings.HasSuffix(prefix, "_") {
		return prefix
	}
	return prefix + "_"
}

// bootstrapKeyToEnvToken 把 dotted key 转换为 env token（不含前缀）。
func bootstrapKeyToEnvToken(dottedKey string) string {
	parts := strings.Split(dottedKey, ".")
	normalized := make([]string, 0, len(parts))
	for _, item := range parts {
		token := normalizeEnvSegment(item)
		if token == "" {
			continue
		}
		normalized = append(normalized, token)
	}
	return strings.Join(normalized, "_")
}

// normalizeEnvSegment 把单段键名转换为 SNAKE_CASE。
func normalizeEnvSegment(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	runes := []rune(input)
	var builder strings.Builder
	for index, current := range runes {
		if unicode.IsLetter(current) || unicode.IsDigit(current) {
			if unicode.IsUpper(current) {
				if shouldInsertUnderscoreBeforeUpper(runes, index) {
					appendUnderscoreIfNeeded(&builder)
				}
				builder.WriteRune(unicode.ToUpper(current))
				continue
			}
			builder.WriteRune(unicode.ToUpper(current))
			continue
		}
		appendUnderscoreIfNeeded(&builder)
	}
	return strings.Trim(builder.String(), "_")
}

// shouldInsertUnderscoreBeforeUpper 判断大写字符前是否需要插入分隔符。
func shouldInsertUnderscoreBeforeUpper(runes []rune, index int) bool {
	if index <= 0 || index >= len(runes) {
		return false
	}
	previous := runes[index-1]
	if unicode.IsLower(previous) || unicode.IsDigit(previous) {
		return true
	}
	if !unicode.IsUpper(previous) {
		return false
	}
	if index+1 >= len(runes) {
		return false
	}
	return unicode.IsLower(runes[index+1])
}

// appendUnderscoreIfNeeded 在 builder 尾部非下划线时追加 `_`。
func appendUnderscoreIfNeeded(builder *strings.Builder) {
	if builder == nil || builder.Len() == 0 {
		return
	}
	current := builder.String()
	if current[len(current)-1] == '_' {
		return
	}
	builder.WriteByte('_')
}

// parseBootstrapEnvValue 按 schema 类型把 env 文本转换为目标值。
//
// 说明：
// 1. 解析失败时保留原始字符串，让后续校验路径输出一致的语义错误；
// 2. list/object/map 支持 YAML/JSON 文本输入。
func parseBootstrapEnvValue(raw string, schemaType BootstrapSchemaType) any {
	schemaType = NormalizeBootstrapSchemaType(schemaType)
	switch schemaType {
	case "":
		return raw
	case BootstrapSchemaTypeString:
		return raw
	case BootstrapSchemaTypeBool:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return raw
		}
		return value
	case BootstrapSchemaTypeInt:
		value, err := strconv.Atoi(raw)
		if err != nil {
			return raw
		}
		return value
	case BootstrapSchemaTypeInt64:
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return raw
		}
		return value
	case BootstrapSchemaTypeUint:
		value, err := strconv.ParseUint(raw, 10, strconv.IntSize)
		if err != nil {
			return raw
		}
		return uint(value)
	case BootstrapSchemaTypeUint64:
		value, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return raw
		}
		return value
	case BootstrapSchemaTypeFloat32:
		value, err := strconv.ParseFloat(raw, 32)
		if err != nil {
			return raw
		}
		return float32(value)
	case BootstrapSchemaTypeFloat64:
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return raw
		}
		return value
	case BootstrapSchemaTypeList, BootstrapSchemaTypeMap:
		var value any
		if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
			return raw
		}
		return value
	default:
		return raw
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
