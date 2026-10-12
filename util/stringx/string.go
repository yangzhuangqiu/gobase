package stringx

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

// StringOf 将任意值转换为字符串。
func StringOf(value any) string {
	// 快路径阶段：常见基础类型直接使用 strconv，避免 fmt 的反射和额外分配。
	switch typedValue := value.(type) {
	case nil:
		return ""
	case string:
		return typedValue
	case []byte:
		return string(typedValue)
	case fmt.Stringer:
		return typedValue.String()
	case int:
		return strconv.FormatInt(int64(typedValue), 10)
	case int8:
		return strconv.FormatInt(int64(typedValue), 10)
	case int16:
		return strconv.FormatInt(int64(typedValue), 10)
	case int32:
		return strconv.FormatInt(int64(typedValue), 10)
	case int64:
		return strconv.FormatInt(typedValue, 10)
	case uint:
		return strconv.FormatUint(uint64(typedValue), 10)
	case uint8:
		return strconv.FormatUint(uint64(typedValue), 10)
	case uint16:
		return strconv.FormatUint(uint64(typedValue), 10)
	case uint32:
		return strconv.FormatUint(uint64(typedValue), 10)
	case uint64:
		return strconv.FormatUint(typedValue, 10)
	case float32:
		return strconv.FormatFloat(float64(typedValue), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(typedValue, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typedValue)
	default:
		return fmt.Sprint(typedValue)
	}
}

// Before 返回第一次出现 sep 之前的子串；不存在时返回空串。
func Before(value, sep string) string {
	position := strings.Index(value, sep)
	if position == -1 {
		return ""
	}
	return value[:position]
}

// After 返回第一次出现 sep 之后的子串；不存在或 sep 在末尾时返回空串。
func After(value, sep string) string {
	position := strings.Index(value, sep)
	if position == -1 {
		return ""
	}
	next := position + len(sep)
	if next >= len(value) {
		return ""
	}
	return value[next:]
}

// AfterLast 返回最后一次出现 sep 之后的子串；不存在或 sep 在末尾时返回空串。
func AfterLast(value, sep string) string {
	position := strings.LastIndex(value, sep)
	if position == -1 {
		return ""
	}
	next := position + len(sep)
	if next >= len(value) {
		return ""
	}
	return value[next:]
}

// BeforeLast 返回最后一次出现 sep 之前的子串；不存在时返回空串。
func BeforeLast(value, sep string) string {
	position := strings.LastIndex(value, sep)
	if position == -1 {
		return ""
	}
	return value[:position]
}

// HasAnyEmpty 判断参数中是否存在空字符串。
func HasAnyEmpty(values ...string) bool {
	for _, value := range values {
		if value == "" {
			return true
		}
	}
	return false
}

// IsBlank 判断字符串是否为空或仅空白字符。
func IsBlank(value string) bool {
	return strings.TrimSpace(value) == ""
}

// DefaultIfBlank 当 value 为空白字符串时返回默认值。
func DefaultIfBlank(value, defaultValue string) string {
	if IsBlank(value) {
		return defaultValue
	}
	return value
}

// JoinNonEmpty 过滤空字符串后按分隔符拼接。
func JoinNonEmpty(values []string, sep string) string {
	if len(values) == 0 {
		return ""
	}
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		filtered = append(filtered, value)
	}
	return strings.Join(filtered, sep)
}

// CamelCaseToSep 将驼峰命名转换为指定分隔符的小写串。
// 示例: "MailSendID" + '_' -> "mail_send_i_d"。
func CamelCaseToSep(value string, sep rune) string {
	if value == "" {
		return ""
	}
	// 转换阶段：每个大写 rune 独立形成新词，因此连续缩写会逐字拆分。
	result := make([]rune, 0, len(value)+4)
	for index, current := range value {
		if index == 0 {
			result = append(result, unicode.ToLower(current))
			continue
		}
		if unicode.IsUpper(current) {
			result = append(result, sep)
		}
		result = append(result, unicode.ToLower(current))
	}
	return string(result)
}

// CopyStrings 返回一个新的字符串切片副本。
func CopyStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

// ToJSON 将值序列化为 JSON 字符串，失败时返回空字符串。
// 典型调用场景：调用方已保证可序列化，仅需要日志或低风险便捷文本。
func ToJSON(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

// ToJSONBytes 将值序列化为 JSON bytes，失败时返回 nil。
// 典型调用场景：调用方已保证可序列化，且 nil 能明确表达便捷转换失败。
func ToJSONBytes(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}

// ParseInt 解析十进制 int。
func ParseInt(raw string) (int, error) {
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, errorsx.Wrapf(err, "stringx.ParseInt", KindParseError, "raw=%q", raw)
	}
	return value, nil
}

// ParseInt64 解析十进制 int64。
func ParseInt64(raw string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, errorsx.Wrapf(err, "stringx.ParseInt64", KindParseError, "raw=%q", raw)
	}
	return value, nil
}

// ParseFloat64 解析 float64。
func ParseFloat64(raw string) (float64, error) {
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, errorsx.Wrapf(err, "stringx.ParseFloat64", KindParseError, "raw=%q", raw)
	}
	return value, nil
}
