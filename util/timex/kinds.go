package timex

import "github.com/yangzhuangqiu/gobase/util/errorsx"

const (
	// KindInvalidTimezone 表示时区名称为空或无法加载，调用方应修正时区配置。
	KindInvalidTimezone errorsx.Kind = "timex.invalid_timezone"
	// KindParseError 表示文本不符合指定 layout，调用方应修正输入格式。
	KindParseError errorsx.Kind = "timex.parse_error"
)
