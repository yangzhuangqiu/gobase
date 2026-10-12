package randx

import (
	crand "crypto/rand"
	"io"
	"math/big"
	"sync"
	"time"

	"github.com/yangzhuangqiu/gobase/util/errorsx"
)

const (
	// alphanumericCharset 是默认 ASCII 字母数字 alphabet，顺序只影响伪随机序列兼容性。
	alphanumericCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	// alphabeticCharset 是默认 ASCII 纯字母 alphabet。
	alphabeticCharset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	// numericCharset 是默认 ASCII 十进制数字 alphabet。
	numericCharset = "0123456789"
	// cryptoReadChunkSize 是安全随机字符串批量读取大小（单位：byte），用于减少随机源调用。
	cryptoReadChunkSize = 256
)

var (
	// defaultMath 是包级便捷伪随机实例，由 defaultMathMu 串行保护。
	defaultMath = NewMath(time.Now().UnixNano())
	// defaultMathMu 保证包级 math API 并发安全；显式 Math 实例仍由调用方同步。
	defaultMathMu sync.Mutex
)

// IntN 使用默认 math 随机实例生成 [0, max) 范围整数。
func IntN(max int) (int, error) {
	defaultMathMu.Lock()
	defer defaultMathMu.Unlock()
	return defaultMath.IntN(max)
}

// IntRange 使用默认 math 随机实例生成 [startInclusive, endExclusive) 范围整数。
func IntRange(startInclusive, endExclusive int) (int, error) {
	defaultMathMu.Lock()
	defer defaultMathMu.Unlock()
	return defaultMath.IntRange(startInclusive, endExclusive)
}

// Int64Range 使用默认 math 随机实例生成 [startInclusive, endExclusive) 范围整数。
func Int64Range(startInclusive, endExclusive int64) (int64, error) {
	defaultMathMu.Lock()
	defer defaultMathMu.Unlock()
	return defaultMath.Int64Range(startInclusive, endExclusive)
}

// Alphanumeric 使用默认 math 随机实例生成指定长度字母数字串。
func Alphanumeric(length int) (string, error) {
	defaultMathMu.Lock()
	defer defaultMathMu.Unlock()
	return defaultMath.Alphanumeric(length)
}

// Alphabetic 使用默认 math 随机实例生成指定长度纯字母串。
func Alphabetic(length int) (string, error) {
	defaultMathMu.Lock()
	defer defaultMathMu.Unlock()
	return defaultMath.Alphabetic(length)
}

// Numeric 使用默认 math 随机实例生成指定长度纯数字串。
func Numeric(length int) (string, error) {
	defaultMathMu.Lock()
	defer defaultMathMu.Unlock()
	return defaultMath.Numeric(length)
}

// StringFromCharset 使用默认 math 随机实例按字符集生成字符串。
func StringFromCharset(length int, charset string) (string, error) {
	defaultMathMu.Lock()
	defer defaultMathMu.Unlock()
	return defaultMath.StringFromCharset(length, charset)
}

// Bytes 使用默认 math 随机实例生成指定长度字节数组。
func Bytes(length int) ([]byte, error) {
	defaultMathMu.Lock()
	defer defaultMathMu.Unlock()
	return defaultMath.Bytes(length)
}

// Bool 使用默认 math 随机实例生成布尔值。
func Bool() (bool, error) {
	defaultMathMu.Lock()
	defer defaultMathMu.Unlock()
	return defaultMath.Bool()
}

// IntNByCrypto 生成 [0, max) 范围内的安全随机整数。
func IntNByCrypto(max int) (int, error) {
	if max <= 0 {
		return 0, errorsx.Newf("randx.IntNByCrypto", KindInvalidMax, "max=%d", max)
	}
	randomInt, err := crand.Int(crand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, errorsx.Wrap(err, "randx.IntNByCrypto", KindRandomSourceFailed)
	}
	return int(randomInt.Int64()), nil
}

// IntRangeByCrypto 生成 [startInclusive, endExclusive) 范围内的安全随机整数。
func IntRangeByCrypto(startInclusive, endExclusive int) (int, error) {
	if startInclusive >= endExclusive {
		return 0, errorsx.Newf(
			"randx.IntRangeByCrypto",
			KindInvalidRange,
			"start=%d end=%d",
			startInclusive,
			endExclusive,
		)
	}
	// 使用 big.Int 表达最多 2^64-1 的合法 int 区间宽度，避免有符号减法溢出。
	start := big.NewInt(int64(startInclusive))
	rangeWidth := new(big.Int).Sub(big.NewInt(int64(endExclusive)), start)
	offset, err := crand.Int(crand.Reader, rangeWidth)
	if err != nil {
		return 0, errorsx.Wrap(err, "randx.IntRangeByCrypto", KindRandomSourceFailed)
	}
	return int(new(big.Int).Add(start, offset).Int64()), nil
}

// AlphanumericByCrypto 生成指定长度的安全随机字母数字串。
func AlphanumericByCrypto(length int) (string, error) {
	return StringFromCharsetByCrypto(length, alphanumericCharset)
}

// AlphabeticByCrypto 生成指定长度的安全随机纯字母串。
func AlphabeticByCrypto(length int) (string, error) {
	return StringFromCharsetByCrypto(length, alphabeticCharset)
}

// NumericByCrypto 生成指定长度的安全随机纯数字串。
func NumericByCrypto(length int) (string, error) {
	return StringFromCharsetByCrypto(length, numericCharset)
}

// Int64RangeByCrypto 生成 [startInclusive, endExclusive) 范围内的安全随机 int64。
func Int64RangeByCrypto(startInclusive, endExclusive int64) (int64, error) {
	if startInclusive >= endExclusive {
		return 0, errorsx.Newf(
			"randx.Int64RangeByCrypto",
			KindInvalidRange,
			"start=%d end=%d",
			startInclusive,
			endExclusive,
		)
	}

	// 使用 big.Int 计算区间宽度，避免跨越正负边界时发生 int64 减法溢出。
	start := big.NewInt(startInclusive)
	rangeWidth := new(big.Int).Sub(big.NewInt(endExclusive), start)
	offset, err := crand.Int(crand.Reader, rangeWidth)
	if err != nil {
		return 0, errorsx.Wrap(err, "randx.Int64RangeByCrypto", KindRandomSourceFailed)
	}
	return new(big.Int).Add(start, offset).Int64(), nil
}

// BytesByCrypto 生成指定长度的安全随机字节数组。
func BytesByCrypto(length int) ([]byte, error) {
	if length < 0 {
		return nil, errorsx.Newf("randx.BytesByCrypto", KindInvalidLength, "length=%d", length)
	}
	if length == 0 {
		return []byte{}, nil
	}
	output := make([]byte, length)
	if _, err := io.ReadFull(crand.Reader, output); err != nil {
		return nil, errorsx.Wrap(err, "randx.BytesByCrypto", KindRandomSourceFailed)
	}
	return output, nil
}

// BoolByCrypto 生成安全随机布尔值。
func BoolByCrypto() (bool, error) {
	value, err := IntNByCrypto(2)
	if err != nil {
		return false, err
	}
	return value == 1, nil
}

// StringFromCharsetByCrypto 使用给定字符集生成指定长度的安全随机字符串。
func StringFromCharsetByCrypto(length int, charset string) (string, error) {
	if length < 0 {
		return "", errorsx.Newf("randx.StringFromCharsetByCrypto", KindInvalidLength, "length=%d", length)
	}
	if charset == "" {
		return "", errorsx.New("randx.StringFromCharsetByCrypto", KindEmptyCharset)
	}
	if length == 0 {
		return "", nil
	}

	// 仅在字符集超过 byte 域时回退 big.Int 路径，常规场景使用批量随机字节降低开销。
	if len(charset) > 256 {
		return stringFromLargeCharsetByCrypto(length, charset)
	}

	out := make([]byte, length)
	if len(charset) == 1 {
		for index := range out {
			out[index] = charset[0]
		}
		return string(out), nil
	}

	acceptLimit := (256 / len(charset)) * len(charset)
	randomBuffer := make([]byte, cryptoReadChunkSize)
	outIndex := 0
	for outIndex < length {
		if _, err := io.ReadFull(crand.Reader, randomBuffer); err != nil {
			return "", errorsx.Wrap(err, "randx.StringFromCharsetByCrypto", KindRandomSourceFailed)
		}
		for _, randomByte := range randomBuffer {
			if int(randomByte) >= acceptLimit {
				continue
			}
			out[outIndex] = charset[int(randomByte)%len(charset)]
			outIndex++
			if outIndex == length {
				break
			}
		}
	}

	return string(out), nil
}

// stringFromLargeCharsetByCrypto 为超过单字节拒绝采样范围的 byte alphabet 逐位取样。
// 阶段角色：保持自定义 charset 的位置均匀性，代价是每个输出 byte 调用一次 crypto/rand.Int。
func stringFromLargeCharsetByCrypto(length int, charset string) (string, error) {
	out := make([]byte, length)
	maxIdx := big.NewInt(int64(len(charset)))
	for index := 0; index < length; index++ {
		randomIdx, err := crand.Int(crand.Reader, maxIdx)
		if err != nil {
			return "", errorsx.Wrap(err, "randx.stringFromLargeCharsetByCrypto", KindRandomSourceFailed)
		}
		out[index] = charset[randomIdx.Int64()]
	}
	return string(out), nil
}
