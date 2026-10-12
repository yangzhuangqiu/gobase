package filex

import "github.com/yangzhuangqiu/gobase/util/errorsx"

const (
	// KindInvalidArgument 表示路径、批大小或回调参数不满足公开契约。
	KindInvalidArgument errorsx.Kind = "filex.invalid_argument"
	// KindIOError 表示文件系统操作失败；调用方通过 errors.Is/As 继续判断不存在或权限等 cause。
	KindIOError errorsx.Kind = "filex.io_error"
	// KindLineTooLong 表示单行内容超过默认上限，调用方应拒绝文件或改用专用流式协议。
	KindLineTooLong errorsx.Kind = "filex.line_too_long"
	// KindHandleError 表示批处理回调拒绝当前批次，调用方应检查包装的业务 cause。
	KindHandleError errorsx.Kind = "filex.handle_error"
)
