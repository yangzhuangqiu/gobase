package appwebhertz

import (
	"github.com/cloudwego/hertz/pkg/app"
	hertzconfig "github.com/cloudwego/hertz/pkg/common/config"
)

// InstrumentationBundle 定义必须原子安装的 Hertz 插桩合同。
//
// 典型调用场景:
// 1. Trace 适配器成对提供 Hertz 原生 Tracer Option 与 Server Middleware；
// 2. app-web-hertz 在 provider 构造前验证完整 Bundle，并把 Middleware 放入固定插桩阶段。
//
// 说明:
// - 合同只依赖 Hertz，不暴露 OTel、Prometheus 或具体 exporter 类型；
// - 实现必须返回可并发复用的不可变声明，不得在方法调用时获取外部资源。
type InstrumentationBundle interface {
	// HertzOptions 返回必须与 Middleware 同时安装的 Hertz 原生构造声明。
	// 返回 slice 的所有权属于调用方；实现不得依赖调用方保留或修改该 slice。
	HertzOptions() []hertzconfig.Option
	// Middleware 返回插桩阶段的全局 Middleware，按 slice 顺序执行。
	// 返回 slice 的所有权属于调用方；nil Handler 会使 Server 构造 fail-fast。
	Middleware() []app.HandlerFunc
}
