module github.com/yangzhuangqiu/gobase/hlog-zap

go 1.21

require (
	github.com/cloudwego/hertz v0.10.5
	github.com/hertz-contrib/logger/zap v1.1.0
	github.com/yangzhuangqiu/gobase/config v0.0.0
	github.com/yangzhuangqiu/gobase/errcode v0.0.0
	github.com/yangzhuangqiu/gobase/log v0.0.0
	github.com/yangzhuangqiu/gobase/log-zap v0.0.0
	go.uber.org/zap v1.27.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
)

require (
	github.com/rogpeppe/go-internal v1.9.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/yangzhuangqiu/gobase/config => ../config

replace github.com/yangzhuangqiu/gobase/errcode => ../errcode

replace github.com/yangzhuangqiu/gobase/log-zap => ../log-zap

replace github.com/yangzhuangqiu/gobase/log => ../log
