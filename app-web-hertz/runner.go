package appwebhertz

import (
	"context"

	baseapp "github.com/yangzhuangqiu/gobase/app"
	"github.com/yangzhuangqiu/gobase/ioc"
	baseweb "github.com/yangzhuangqiu/gobase/web"
)

// Runner 返回由 app 统一取消、等待和关闭的单 Web Server 业务入口。
//
// 典型调用场景：应用通过 NewModule 发布唯一 web.Server，并把本函数结果传给 app.WithRunner。
//
// 返回：
// - app.Runner：Runtime Build 后解析唯一 web.Server，Run 返回后 app 才进入 Runtime Close。
func Runner() baseapp.Runner {
	return func(ctx context.Context, runtime baseapp.Runtime) error {
		if isNilInterface(runtime) {
			return errorForServerResolveFailed("op=app_web_hertz.runner result=failed stage=precheck reason=runtime_required")
		}
		container := runtime.Container()
		if isNilInterface(container) {
			return errorForServerResolveFailed("op=app_web_hertz.runner result=failed stage=precheck reason=container_required")
		}
		server, err := ioc.ResolveAs[baseweb.Server](container)
		if err != nil {
			return errorForServerResolveFailed(
				"op=app_web_hertz.runner result=failed stage=resolve_server reason=ioc_resolve_failed",
				err,
			)
		}
		return server.Run(ctx)
	}
}
