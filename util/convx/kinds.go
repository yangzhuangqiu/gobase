package convx

import "github.com/yangzhuangqiu/gobase/util/errorsx"

const (
	// KindOverflow 表示数值超出目标类型可表示范围，调用方应缩小输入或改用更宽类型。
	KindOverflow errorsx.Kind = "convx.overflow"
	// KindParseError 表示文本输入无法按目标类型解析，调用方应修正文本格式。
	KindParseError errorsx.Kind = "convx.parse_error"
	// KindUnsupportedType 表示入参类型不受支持，调用方应先完成显式类型归一化。
	KindUnsupportedType errorsx.Kind = "convx.unsupported_type"
	// KindInvalidValue 表示类型受支持但值不满足目标语义，例如布尔整数不是 0/1。
	KindInvalidValue errorsx.Kind = "convx.invalid_value"
)
