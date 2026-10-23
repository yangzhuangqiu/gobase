module github.com/yangzhuangqiu/gobase/app-observe-webhertz

go 1.21

require (
	github.com/cloudwego/hertz v0.10.5
	github.com/prometheus/client_golang v1.19.1
	github.com/prometheus/client_model v0.5.0
	github.com/prometheus/common v0.48.0
	github.com/yangzhuangqiu/gobase/app-web-hertz v0.0.0
	github.com/yangzhuangqiu/gobase/errcode v0.0.0
	github.com/yangzhuangqiu/gobase/ioc v0.0.0
	github.com/yangzhuangqiu/gobase/web-hertz v0.0.0
	go.opentelemetry.io/otel v1.24.0
	go.opentelemetry.io/otel/sdk v1.24.0
	go.opentelemetry.io/otel/trace v1.24.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bytedance/gopkg v0.1.4 // indirect
	github.com/bytedance/sonic v1.15.0 // indirect
	github.com/bytedance/sonic/loader v0.5.0 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/cloudwego/base64x v0.1.6 // indirect
	github.com/cloudwego/gopkg v0.2.0 // indirect
	github.com/cloudwego/netpoll v0.7.3 // indirect
	github.com/fsnotify/fsnotify v1.5.4 // indirect
	github.com/go-logr/logr v1.4.1 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/klauspost/cpuid/v2 v2.2.9 // indirect
	github.com/prometheus/procfs v0.12.0 // indirect
	github.com/spf13/cobra v1.8.1 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/tidwall/gjson v1.14.4 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	github.com/twitchyliquid64/golang-asm v0.15.1 // indirect
	github.com/yangzhuangqiu/gobase/app v0.0.0 // indirect
	github.com/yangzhuangqiu/gobase/app-runtime v0.0.0 // indirect
	github.com/yangzhuangqiu/gobase/config v0.0.0 // indirect
	github.com/yangzhuangqiu/gobase/log v0.0.0 // indirect
	github.com/yangzhuangqiu/gobase/web v0.0.0 // indirect
	go.opentelemetry.io/otel/metric v1.24.0 // indirect
	golang.org/x/arch v0.0.0-20210923205945-b76863e36670 // indirect
	golang.org/x/sys v0.30.0 // indirect
	google.golang.org/protobuf v1.34.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/yangzhuangqiu/gobase/app-web-hertz => ../app-web-hertz

replace github.com/yangzhuangqiu/gobase/app => ../app

replace github.com/yangzhuangqiu/gobase/app-runtime => ../app-runtime

replace github.com/yangzhuangqiu/gobase/config => ../config

replace github.com/yangzhuangqiu/gobase/errcode => ../errcode

replace github.com/yangzhuangqiu/gobase/ioc => ../ioc

replace github.com/yangzhuangqiu/gobase/log => ../log

replace github.com/yangzhuangqiu/gobase/web => ../web

replace github.com/yangzhuangqiu/gobase/web-hertz => ../web-hertz
