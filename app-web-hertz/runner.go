package appwebhertz

import (
	"context"

	baseapp "github.com/yangzhuangqiu/gobase/app"
	"github.com/yangzhuangqiu/gobase/ioc"
	baseweb "github.com/yangzhuangqiu/gobase/web"
)

// Runner 返回由 app 统一取消、等待和关闭的单 Web Server 业务入口。
//
// 典型调用场景：兼容调用方通过 NewModule 发布唯一 web.Server，并用 app.Runner 包装本函数结果。
//
// 返回：
// - func(context.Context, app.Runtime) error：Runtime Build 后解析唯一 Server 的一次性函数合同。
func Runner() func(context.Context, baseapp.Runtime) error {
	return func(ctx context.Context, runtime baseapp.Runtime) error {
		server, err := resolveServer(runtime, "app_web_hertz.runner")
		if err != nil {
			return err
		}
		return server.Run(ctx)
	}
}

// resolveServer 从本次 app Runtime 解析唯一 web.Server，并保留分层错误链。
// 阶段角色：兼容 Runner 与推荐 Workload 适配器共用的 IOC 解析边界。
func resolveServer(runtime baseapp.Runtime, operation string) (baseweb.Server, error) {
	if isNilInterface(runtime) {
		return nil, errorForServerResolveFailed("op=" + operation + " result=failed stage=precheck reason=runtime_required")
	}
	container := runtime.Container()
	if isNilInterface(container) {
		return nil, errorForServerResolveFailed("op=" + operation + " result=failed stage=precheck reason=container_required")
	}
	server, err := ioc.ResolveAs[baseweb.Server](container)
	if err != nil {
		return nil, errorForServerResolveFailed(
			"op="+operation+" result=failed stage=resolve_server reason=ioc_resolve_failed",
			err,
		)
	}
	return server, nil
}
