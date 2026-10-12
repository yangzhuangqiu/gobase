module github.com/yangzhuangqiu/gobase/app

go 1.21

require (
	github.com/spf13/cobra v1.8.1
	github.com/spf13/pflag v1.0.5
	github.com/yangzhuangqiu/gobase/app-runtime v0.0.0
	github.com/yangzhuangqiu/gobase/config v0.0.0
	github.com/yangzhuangqiu/gobase/errcode v0.0.0
	github.com/yangzhuangqiu/gobase/ioc v0.0.0
	github.com/yangzhuangqiu/gobase/log v0.0.0
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/yangzhuangqiu/gobase/app-runtime => ../app-runtime

replace github.com/yangzhuangqiu/gobase/config => ../config

replace github.com/yangzhuangqiu/gobase/errcode => ../errcode

replace github.com/yangzhuangqiu/gobase/ioc => ../ioc

replace github.com/yangzhuangqiu/gobase/log => ../log
