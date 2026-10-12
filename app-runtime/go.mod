module github.com/yangzhuangqiu/gobase/app-runtime

go 1.21

require (
	github.com/yangzhuangqiu/gobase/config v0.0.0
	github.com/yangzhuangqiu/gobase/errcode v0.0.0
	github.com/yangzhuangqiu/gobase/ioc v0.0.0
	github.com/yangzhuangqiu/gobase/log v0.0.0
)

require (
	github.com/kr/text v0.2.0 // indirect
	github.com/rogpeppe/go-internal v1.9.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/yangzhuangqiu/gobase/config => ../config

replace github.com/yangzhuangqiu/gobase/errcode => ../errcode

replace github.com/yangzhuangqiu/gobase/ioc => ../ioc

replace github.com/yangzhuangqiu/gobase/log => ../log
