package appruntime

import "github.com/yangzhuangqiu/gobase/config"

// BuildInput 描述一次 Build 才确定的外部输入。
// 普通调用使用 Application.Build；CLI、测试和嵌入式宿主才使用 BuildWithInput。
type BuildInput struct {
	// Config 是本次 config bootstrap、pinned、env prefix 和文件策略输入。
	Config config.Input
}

// ApplicationInfo 是注入容器的稳定应用元信息。
type ApplicationInfo struct {
	// Name 是应用稳定名称，用于日志、环境变量前缀与可观测标识。
	Name string
}
