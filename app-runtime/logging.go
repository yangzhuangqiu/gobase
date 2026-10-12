package appruntime

import (
	"context"

	"github.com/yangzhuangqiu/gobase/config"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

// Logging 定义基于已发布配置构建日志 Provider 的扩展契约。
//
// 典型调用场景：app-log-zap 等适配模块实现该接口，Application 在 config 后、IOC 前调用。
type Logging interface {
	// Build 创建本次 Runtime 独占的日志 Provider。
	//
	// 参数：
	// - ctx：应用运行生命周期 context；
	// - manager：已完成首次发布的 ConfigManager。
	//
	// 返回：
	// - Provider：成功后由 Runtime 唯一拥有并在 IOC 之后关闭；
	// - error：实现必须先清理由内部创建但尚未交付的半成品。
	Build(ctx context.Context, manager config.Manager) (baselog.Provider, error)
}
