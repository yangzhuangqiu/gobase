package appruntime

import (
	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/ioc"
)

// BuildInput 描述一次 Build 才确定的外部输入。
// 普通调用使用 Application.Build；CLI、测试和嵌入式宿主才使用 BuildWithInput。
type BuildInput struct {
	// Config 是本次 config bootstrap、pinned、env prefix 和文件策略输入。
	Config config.Input
	// BeanDefinitions 是仅本次 Build 注册的局部完整 Bean 声明；不修改 Application 稳定蓝图。
	BeanDefinitions []ioc.Definition
	// Modules 是仅本次 Build 注册的局部 bean 模块；不修改 Application 稳定蓝图。
	Modules []Module
}

// ApplicationInfo 是注入容器的稳定应用元信息。
type ApplicationInfo struct {
	// Name 是应用稳定名称，用于日志、环境变量前缀与可观测标识。
	Name string
}
