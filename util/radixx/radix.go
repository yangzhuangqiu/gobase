package radixx

import "github.com/yangzhuangqiu/gobase/util/errorsx"

// defaultBase62Digits 是默认 Base62 byte alphabet，顺序决定对外编码兼容性。
const defaultBase62Digits = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// maxInt64Abs 是 MinInt64 绝对值的 uint64 表达，用于负数解码边界检查。
const maxInt64Abs = uint64(^uint64(0)>>1) + 1

var (
	// defaultBase62Radix 是固定 alphabet 的基数，初始化后只读。
	defaultBase62Radix = len(defaultBase62Digits)
	// defaultBase62IndexMap 是默认解码索引，初始化后并发只读。
	defaultBase62IndexMap = mustBuildDefaultBase62IndexMap()
)

// EncodeInt64 使用自定义 digits 对 int64 编码。
// 参数:
// - value: 待编码数字。
// - digits: 字符集，长度必须 >= 2 且字符不能重复。
// - pad: 非负数编码的最小长度；pad<=0 表示不补齐，负数编码不补齐。
func EncodeInt64(value int64, digits string, pad int) (string, error) {
	if err := validateDigits(digits, "radixx.EncodeInt64"); err != nil {
		return "", err
	}
	return encodeWithDigits(value, digits, len(digits), pad), nil
}

// encodeWithDigits 编码已验证 alphabet 的 int64，并对 MinInt64 使用补码求绝对值。
func encodeWithDigits(value int64, digits string, radix int, pad int) string {
	if value == 0 {
		return leftPad(string(digits[0]), pad, digits[0])
	}

	negative := value < 0
	unsignedValue := uint64(value)
	if negative {
		// 使用补码转换绝对值，避免 MinInt64 取反溢出。
		unsignedValue = uint64(-(value + 1))
		unsignedValue += 1
	}

	buffer := make([]byte, 0, 16)
	for unsignedValue > 0 {
		buffer = append(buffer, digits[unsignedValue%uint64(radix)])
		unsignedValue /= uint64(radix)
	}

	for left, right := 0, len(buffer)-1; left < right; left, right = left+1, right-1 {
		buffer[left], buffer[right] = buffer[right], buffer[left]
	}

	encoded := string(buffer)
	if !negative {
		encoded = leftPad(encoded, pad, digits[0])
	}
	if negative {
		return "-" + encoded
	}
	return encoded
}

// DecodeInt64 使用自定义 digits 将字符串解码为 int64。
func DecodeInt64(value string, digits string) (int64, error) {
	// op 保证 alphabet 校验和数值解码都归属自定义入口。
	const op = "radixx.DecodeInt64"
	radix, indexMap, err := buildDigitMap(digits, op)
	if err != nil {
		return 0, err
	}
	return decodeWithDigitMap(value, radix, indexMap, op)
}

// decodeWithDigitMap 解码已验证 alphabet 的文本，并在每步乘加前检查 int64 边界。
// 阶段角色：统一自定义和 Base62 入口的值校验，仅由 op 区分公开来源。
func decodeWithDigitMap(value string, radix int, indexMap map[byte]int, op string) (int64, error) {
	// 词法阶段：先处理空值和可选负号，再逐 byte 查询 alphabet 索引。
	if value == "" {
		return 0, errorsx.New(op, KindInvalidValue)
	}

	negative := false
	start := 0
	if value[0] == '-' {
		negative = true
		start = 1
		if len(value) == 1 {
			return 0, errorsx.New(op, KindInvalidValue)
		}
	}

	radixValue := uint64(radix)
	maxAbs := maxInt64Abs - 1
	if negative {
		maxAbs = maxInt64Abs
	}

	var result uint64
	// 累加阶段：在乘法前使用反向不等式检查正负 int64 边界。
	for index := start; index < len(value); index++ {
		position, exists := indexMap[value[index]]
		if !exists {
			return 0, errorsx.Newf(op, KindInvalidValue, "ch=%q", value[index])
		}
		digit := uint64(position)
		if result > (maxAbs-digit)/radixValue {
			return 0, errorsx.New(op, KindOverflow)
		}
		result = result*radixValue + digit
	}

	if negative {
		return -int64(result), nil
	}
	return int64(result), nil
}

// Base62EncodeInt64 使用默认 base62 字符集编码。
func Base62EncodeInt64(value int64, pad int) string {
	return encodeWithDigits(value, defaultBase62Digits, defaultBase62Radix, pad)
}

// Base62DecodeInt64 使用默认 base62 字符集解码。
func Base62DecodeInt64(value string) (int64, error) {
	return decodeWithDigitMap(value, defaultBase62Radix, defaultBase62IndexMap, "radixx.Base62DecodeInt64")
}

// buildDigitMap 校验 byte alphabet 并构造 decode 使用的只读索引。
func buildDigitMap(digits string, op string) (int, map[byte]int, error) {
	if err := validateDigits(digits, op); err != nil {
		return 0, nil, err
	}
	indexMap := make(map[byte]int, len(digits))
	for index := 0; index < len(digits); index++ {
		indexMap[digits[index]] = index
	}
	return len(digits), indexMap, nil
}

// validateDigits 校验 alphabet 至少包含两个互不重复的 byte。
func validateDigits(digits string, op string) error {
	if len(digits) < 2 {
		return errorsx.Newf(op, KindInvalidDigits, "reason=too_short len=%d", len(digits))
	}

	seen := [256]bool{}
	for index := 0; index < len(digits); index++ {
		digit := digits[index]
		if digit == '-' {
			return errorsx.Newf(op, KindInvalidDigits, "reason=reserved_sign ch=%q", digit)
		}
		if seen[digit] {
			return errorsx.Newf(op, KindInvalidDigits, "reason=duplicate ch=%q", digit)
		}
		seen[digit] = true
	}
	return nil
}

// mustBuildDefaultBase62IndexMap 在包初始化时构造固定 alphabet 索引；常量失效属于开发错误。
func mustBuildDefaultBase62IndexMap() map[byte]int {
	_, indexMap, err := buildDigitMap(defaultBase62Digits, "radixx.initBase62")
	if err != nil {
		panic(err)
	}
	return indexMap
}

// leftPad 按最小宽度在左侧补 alphabet 的零值 byte。
func leftPad(value string, pad int, padByte byte) string {
	if pad <= len(value) {
		return value
	}
	buffer := make([]byte, pad-len(value))
	for index := range buffer {
		buffer[index] = padByte
	}
	return string(buffer) + value
}
