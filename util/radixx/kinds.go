package radixx

import "github.com/yangzhuangqiu/gobase/util/errorsx"

const (
	// KindInvalidDigits 表示 digits 长度不足、包含重复 byte 或占用保留负号，调用方应修正 alphabet。
	KindInvalidDigits errorsx.Kind = "radixx.invalid_digits"
	// KindInvalidValue 表示待解码文本为空、负号非法或包含 alphabet 之外的 byte。
	KindInvalidValue errorsx.Kind = "radixx.invalid_value"
	// KindOverflow 表示解码结果超出 int64 范围，调用方应改用更宽数值协议。
	KindOverflow errorsx.Kind = "radixx.overflow"
)
