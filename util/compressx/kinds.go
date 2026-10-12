package compressx

import "github.com/yangzhuangqiu/gobase/util/errorsx"

const (
	// KindCompressFailed 表示 gzip 压缩无法完成，调用方应终止或重试当前操作。
	KindCompressFailed errorsx.Kind = "compressx.compress_failed"
	// KindInvalidData 表示输入不是有效 gzip/Base64 数据，调用方应拒绝或修正载荷。
	KindInvalidData errorsx.Kind = "compressx.invalid_data"
	// KindInvalidOutputLimit 表示解压输出上限不是正数字节数，调用方应修正配置。
	KindInvalidOutputLimit errorsx.Kind = "compressx.invalid_output_limit"
	// KindOutputLimitExceeded 表示解压结果超过声明上限，调用方应拒绝载荷或显式调整限额。
	KindOutputLimitExceeded errorsx.Kind = "compressx.output_limit_exceeded"
)
