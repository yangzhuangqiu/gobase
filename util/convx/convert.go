package convx

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

// ToInt 将任意输入按十进制语义转换为 int。
func ToInt(value any) (int, error) {
	int64Value, err := toInt64(value, "convx.ToInt")
	if err != nil {
		return 0, err
	}
	if int64Value > int64(maxInt()) || int64Value < int64(minInt()) {
		return 0, errorsx.Newf("convx.ToInt", KindOverflow, "value=%d", int64Value)
	}
	return int(int64Value), nil
}

// ToInt64 将任意输入按十进制语义转换为 int64。
func ToInt64(value any) (int64, error) {
	return toInt64(value, "convx.ToInt64")
}

// toInt64 执行整数输入归一化，并把失败归属到调用入口 op。
// 阶段角色：供 ToInt、ToInt64、ToBool 和 ToDuration 复用严格十进制转换语义。
func toInt64(value any, op string) (int64, error) {
	// 原生整数阶段：保持精确值；无符号输入先验证 int64 上界。
	switch typedValue := value.(type) {
	case int:
		return int64(typedValue), nil
	case int8:
		return int64(typedValue), nil
	case int16:
		return int64(typedValue), nil
	case int32:
		return int64(typedValue), nil
	case int64:
		return typedValue, nil
	case uint:
		if uint64(typedValue) > math.MaxInt64 {
			return 0, errorsx.Newf(op, KindOverflow, "type=%T value=%d", value, typedValue)
		}
		return int64(typedValue), nil
	case uint8:
		return int64(typedValue), nil
	case uint16:
		return int64(typedValue), nil
	case uint32:
		return int64(typedValue), nil
	case uint64:
		if typedValue > math.MaxInt64 {
			return 0, errorsx.Newf(op, KindOverflow, "type=%T value=%d", value, typedValue)
		}
		return int64(typedValue), nil
	case string:
		parsed, err := strconv.ParseInt(typedValue, 10, 64)
		if err != nil {
			return 0, errorsx.Wrapf(err, op, KindParseError, "source=string value=%q", typedValue)
		}
		return parsed, nil
	case []byte:
		parsed, err := strconv.ParseInt(string(typedValue), 10, 64)
		if err != nil {
			return 0, errorsx.Wrapf(err, op, KindParseError, "source=bytes value=%q", string(typedValue))
		}
		return parsed, nil
	default:
		return 0, errorsx.Newf(op, KindUnsupportedType, "type=%T", value)
	}
}

// ToFloat64 将任意输入转换为 float64。
func ToFloat64(value any) (float64, error) {
	// 数值阶段：遵循 Go 原生浮点转换；uint64 可能发生精度损失但不会伪造溢出错误。
	switch typedValue := value.(type) {
	case float32:
		return float64(typedValue), nil
	case float64:
		return typedValue, nil
	case int, int8, int16, int32, int64:
		int64Value, err := ToInt64(typedValue)
		if err != nil {
			return 0, err
		}
		return float64(int64Value), nil
	case uint:
		return float64(typedValue), nil
	case uint8:
		return float64(typedValue), nil
	case uint16:
		return float64(typedValue), nil
	case uint32:
		return float64(typedValue), nil
	case uint64:
		return float64(typedValue), nil
	case string:
		parsed, err := strconv.ParseFloat(typedValue, 64)
		if err != nil {
			return 0, errorsx.Wrapf(err, "convx.ToFloat64", KindParseError, "source=string value=%q", typedValue)
		}
		return parsed, nil
	case []byte:
		parsed, err := strconv.ParseFloat(string(typedValue), 64)
		if err != nil {
			return 0, errorsx.Wrapf(err, "convx.ToFloat64", KindParseError, "source=bytes value=%q", string(typedValue))
		}
		return parsed, nil
	default:
		return 0, errorsx.Newf("convx.ToFloat64", KindUnsupportedType, "type=%T", value)
	}
}

// ToBool 将任意输入按布尔语义转换。
func ToBool(value any) (bool, error) {
	// 文本阶段：直接采用 strconv.ParseBool 的标准 true/false 兼容集合。
	switch typedValue := value.(type) {
	case bool:
		return typedValue, nil
	case string:
		parsed, err := strconv.ParseBool(typedValue)
		if err != nil {
			return false, errorsx.Wrapf(err, "convx.ToBool", KindParseError, "source=string value=%q", typedValue)
		}
		return parsed, nil
	case []byte:
		parsed, err := strconv.ParseBool(string(typedValue))
		if err != nil {
			return false, errorsx.Wrapf(err, "convx.ToBool", KindParseError, "source=bytes value=%q", string(typedValue))
		}
		return parsed, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		// 整数阶段：仅接受协议中无歧义的 0/1，避免任意非零值被静默视为 true。
		int64Value, err := toInt64(typedValue, "convx.ToBool")
		if err != nil {
			return false, err
		}
		if int64Value == 0 {
			return false, nil
		}
		if int64Value == 1 {
			return true, nil
		}
		return false, errorsx.Newf("convx.ToBool", KindInvalidValue, "value=%d expected=0_or_1", int64Value)
	default:
		return false, errorsx.Newf("convx.ToBool", KindUnsupportedType, "type=%T", value)
	}
}

// ToDuration 将任意输入转换为 time.Duration。
// 语义:
// - string: 支持 "1h/1min/1s/1ms"（同时兼容 Go 原生 "1m"）。
// - int/int64/uint...: 按毫秒解释。
func ToDuration(value any) (time.Duration, error) {
	// 类型分派阶段：整数统一按毫秒解释，文本统一进入带单位解析器。
	switch typedValue := value.(type) {
	case time.Duration:
		return typedValue, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		milli, err := toInt64(typedValue, "convx.ToDuration")
		if err != nil {
			return 0, err
		}
		return durationFromMilliseconds(milli)
	case string:
		return parseDurationString(typedValue)
	case []byte:
		return parseDurationString(string(typedValue))
	default:
		return 0, errorsx.Newf("convx.ToDuration", KindUnsupportedType, "type=%T", value)
	}
}

// maxInt 返回当前平台 int 的最大值，供 ToInt 做跨架构范围校验。
func maxInt() int {
	return int(^uint(0) >> 1)
}

// minInt 返回当前平台 int 的最小值，供 ToInt 做跨架构范围校验。
func minInt() int {
	return -maxInt() - 1
}

// parseDurationString 解析带单位 duration 或按毫秒解释的纯数字文本。
// 阶段角色：统一 string 与 []byte 的 duration 语义和错误分类。
func parseDurationString(raw string) (time.Duration, error) {
	// 归一化阶段：纯数字优先按毫秒处理，其他文本只扩展 min 后交给标准库。
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, errorsx.New("convx.ToDuration", KindInvalidValue)
	}
	milli, intErr := strconv.ParseInt(value, 10, 64)
	if intErr == nil {
		return durationFromMilliseconds(milli)
	}
	if numberError, ok := intErr.(*strconv.NumError); ok && numberError.Err == strconv.ErrRange {
		return 0, errorsx.Wrapf(intErr, "convx.ToDuration", KindOverflow, "source=milliseconds value=%q", raw)
	}
	normalized := value
	if strings.HasSuffix(normalized, "min") {
		normalized = strings.TrimSuffix(normalized, "min") + "m"
	}
	parsed, err := time.ParseDuration(normalized)
	if err != nil {
		return 0, errorsx.Wrapf(err, "convx.ToDuration", KindParseError, "source=string value=%q", raw)
	}
	return parsed, nil
}

// durationFromMilliseconds 把毫秒转换为 time.Duration，并在乘法前阻止静默溢出。
// 阶段角色：保证整数和纯数字文本共享同一范围契约。
func durationFromMilliseconds(milli int64) (time.Duration, error) {
	nanosPerMilli := int64(time.Millisecond)
	if milli > math.MaxInt64/nanosPerMilli || milli < math.MinInt64/nanosPerMilli {
		return 0, errorsx.Newf("convx.ToDuration", KindOverflow, "milliseconds=%d", milli)
	}
	return time.Duration(milli) * time.Millisecond, nil
}
