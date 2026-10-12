package randx

import (
	mrand "math/rand"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

// Math 是基于 math/rand 的伪随机生成器。
// 说明: 本类型不保证并发安全，由调用方控制共享策略。
type Math struct {
	// rng 保存调用方 seed 对应的可复现伪随机状态，不保证并发安全。
	rng *mrand.Rand
}

// NewMath 创建伪随机生成器。
func NewMath(seed int64) *Math {
	return &Math{rng: mrand.New(mrand.NewSource(seed))}
}

// IntN 生成 [0, max) 范围内的伪随机整数。
func (this *Math) IntN(max int) (int, error) {
	if this == nil || this.rng == nil {
		return 0, errorsx.New("randx.Math.IntN", KindInvalidReceiver)
	}
	if max <= 0 {
		return 0, errorsx.Newf("randx.Math.IntN", KindInvalidMax, "max=%d", max)
	}
	return this.rng.Intn(max), nil
}

// IntRange 生成 [startInclusive, endExclusive) 范围内的伪随机整数。
func (this *Math) IntRange(startInclusive, endExclusive int) (int, error) {
	if this == nil || this.rng == nil {
		return 0, errorsx.New("randx.Math.IntRange", KindInvalidReceiver)
	}
	if startInclusive >= endExclusive {
		return 0, errorsx.Newf(
			"randx.Math.IntRange",
			KindInvalidRange,
			"start=%d end=%d",
			startInclusive,
			endExclusive,
		)
	}
	// 使用 uint 模运算表达当前架构完整 int 区间，避免 end-start 的有符号溢出。
	rangeWidth := uint(endExclusive) - uint(startInclusive)
	offset := this.uintN(rangeWidth)
	return int(uint(startInclusive) + offset), nil
}

// Alphanumeric 生成指定长度的伪随机字母数字串。
func (this *Math) Alphanumeric(length int) (string, error) {
	// 前置阶段：公开实例允许 nil receiver，但以稳定 Kind 返回而不是 panic。
	if this == nil || this.rng == nil {
		return "", errorsx.New("randx.Math.Alphanumeric", KindInvalidReceiver)
	}
	if length < 0 {
		return "", errorsx.Newf("randx.Math.Alphanumeric", KindInvalidLength, "length=%d", length)
	}
	if length == 0 {
		return "", nil
	}

	output := make([]byte, length)
	// 生成阶段：默认 alphabet 唯一且固定，逐 byte 取样不存在字符权重歧义。
	for index := 0; index < length; index++ {
		randomIndex := this.rng.Intn(len(alphanumericCharset))
		output[index] = alphanumericCharset[randomIndex]
	}
	return string(output), nil
}

// Alphabetic 生成指定长度的伪随机纯字母串。
func (this *Math) Alphabetic(length int) (string, error) {
	return this.stringFromCharset(length, alphabeticCharset, "Alphabetic")
}

// Numeric 生成指定长度的伪随机纯数字串。
func (this *Math) Numeric(length int) (string, error) {
	return this.stringFromCharset(length, numericCharset, "Numeric")
}

// StringFromCharset 使用指定字符集生成伪随机字符串。
func (this *Math) StringFromCharset(length int, charset string) (string, error) {
	return this.stringFromCharset(length, charset, "StringFromCharset")
}

// Int64Range 生成 [startInclusive, endExclusive) 范围内的伪随机 int64。
func (this *Math) Int64Range(startInclusive, endExclusive int64) (int64, error) {
	if this == nil || this.rng == nil {
		return 0, errorsx.New("randx.Math.Int64Range", KindInvalidReceiver)
	}
	if startInclusive >= endExclusive {
		return 0, errorsx.Newf(
			"randx.Math.Int64Range",
			KindInvalidRange,
			"start=%d end=%d",
			startInclusive,
			endExclusive,
		)
	}
	// 使用 uint64 模运算表达最多 2^64-1 的合法区间宽度，避免有符号减法溢出。
	rangeWidth := uint64(endExclusive) - uint64(startInclusive)
	offset := this.uint64N(rangeWidth)
	return int64(uint64(startInclusive) + offset), nil
}

// uintN 生成 [0,max) 的均匀 uint，支持当前架构完整 int 区间宽度。
// 前置条件：max 必须大于 0，由 IntRange 的 start<end 校验保证。
func (this *Math) uintN(max uint) uint {
	return uint(this.uint64N(uint64(max)))
}

// uint64N 生成 [0,max) 的均匀 uint64，并用拒绝采样消除取模偏差。
// 前置条件：max 必须大于 0，由 Int64Range 的 start<end 校验保证。
func (this *Math) uint64N(max uint64) uint64 {
	threshold := -max % max
	for {
		value := this.rng.Uint64()
		if value >= threshold {
			return value % max
		}
	}
}

// Bytes 生成指定长度的伪随机字节数组。
func (this *Math) Bytes(length int) ([]byte, error) {
	if this == nil || this.rng == nil {
		return nil, errorsx.New("randx.Math.Bytes", KindInvalidReceiver)
	}
	if length < 0 {
		return nil, errorsx.Newf("randx.Math.Bytes", KindInvalidLength, "length=%d", length)
	}
	output := make([]byte, length)
	for index := 0; index < len(output); index++ {
		output[index] = byte(this.rng.Intn(256))
	}
	return output, nil
}

// Bool 生成伪随机布尔值。
func (this *Math) Bool() (bool, error) {
	if this == nil || this.rng == nil {
		return false, errorsx.New("randx.Math.Bool", KindInvalidReceiver)
	}
	return this.rng.Intn(2) == 1, nil
}

// stringFromCharset 生成 byte alphabet 随机串，并把错误归属到具体公开方法。
// 阶段角色：统一 Alphabetic、Numeric 和 StringFromCharset 的输入边界与分配策略。
func (this *Math) stringFromCharset(length int, charset string, opName string) (string, error) {
	// 前置阶段：统一实例状态、长度和空 alphabet 校验。
	if this == nil || this.rng == nil {
		return "", errorsx.New("randx.Math."+opName, KindInvalidReceiver)
	}
	if length < 0 {
		return "", errorsx.Newf("randx.Math."+opName, KindInvalidLength, "length=%d", length)
	}
	if length == 0 {
		return "", nil
	}
	if charset == "" {
		return "", errorsx.New("randx.Math."+opName, KindEmptyCharset)
	}

	output := make([]byte, length)
	// 生成阶段：charset 按 byte 索引，多字节 Unicode 不属于本 API 契约。
	for index := 0; index < length; index++ {
		randomIndex := this.rng.Intn(len(charset))
		output[index] = charset[randomIndex]
	}
	return string(output), nil
}
