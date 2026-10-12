package main

import "github.com/yangzhuangqiu/gobase/ioc"

// registerQuickstartIOCWorker 注册 quickstart worker bean。
//
// 语义：
// 1. 该函数代表模块侧 ioc.gen.go 产物；
// 2. 仅负责当前模块 bean 声明，不承载运行时编排逻辑。
func registerQuickstartIOCWorker(container ioc.Registry) error {
	return container.Register(&quickstartFullIOCWorker{}, ioc.WithName("QuickstartIOCWorker"))
}
