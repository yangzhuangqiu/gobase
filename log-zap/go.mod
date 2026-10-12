module github.com/yangzhuangqiu/gobase/log-zap

go 1.21

require (
	github.com/yangzhuangqiu/gobase/errcode v0.0.0
	github.com/yangzhuangqiu/gobase/log v0.0.0
	go.uber.org/zap v1.27.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
)

require go.uber.org/multierr v1.10.0 // indirect

replace github.com/yangzhuangqiu/gobase/log => ../log

replace github.com/yangzhuangqiu/gobase/errcode => ../errcode
