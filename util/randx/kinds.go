package randx

import "github.com/yangzhuangqiu/gobase/util/errorsx"

const (
	// KindInvalidMax 表示上界参数不合法（<=0）。
	KindInvalidMax errorsx.Kind = "randx.invalid_max"
	// KindRandomSourceFailed 表示操作系统安全随机源不可用，调用方可重试或终止安全操作。
	KindRandomSourceFailed errorsx.Kind = "randx.random_source_failed"
	// KindInvalidRange 表示区间参数不合法（start >= end）。
	KindInvalidRange errorsx.Kind = "randx.invalid_range"
	// KindInvalidLength 表示长度参数不合法（<0）。
	KindInvalidLength errorsx.Kind = "randx.invalid_length"
	// KindEmptyCharset 表示字符集为空。
	KindEmptyCharset errorsx.Kind = "randx.empty_charset"
	// KindInvalidReceiver 表示 Math 实例为空或未正确初始化，调用方应重新创建实例。
	KindInvalidReceiver errorsx.Kind = "randx.invalid_receiver"
)
