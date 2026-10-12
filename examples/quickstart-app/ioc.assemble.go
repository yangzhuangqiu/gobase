package main

import (
	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	"github.com/yangzhuangqiu/gobase/ioc"
)

// quickstartIOCModule 返回 quickstart 的显式 bean 模块。
//
// 语义：
// 1. 该文件扮演“手动总装配”入口，调用方只维护 bean 注册清单；
// 2. 各单元注册函数由同目录或子目录的 ioc.gen.go 提供。
func quickstartIOCModule() appruntime.Module {
	return appruntime.NewModule("quickstart.ioc", func(registry ioc.Registry) error {
		return registerQuickstartIOCWorker(registry)
	})
}
