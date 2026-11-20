package appwebhertz

import (
	"context"

	baseapp "github.com/yangzhuangqiu/gobase/app"
	"github.com/yangzhuangqiu/gobase/ioc"
	baseweb "github.com/yangzhuangqiu/gobase/web"
)

const (
	// workloadName 是 Hertz 主 Server 在 app 关闭诊断中的稳定语义名称。
	workloadName = "web.hertz"
)

// serverWorkload 把 IOC 中唯一的托管 Server 适配为 app 主动工作负载。
type serverWorkload struct{}

var _ baseapp.Workload = (*serverWorkload)(nil)

// newServerWorkload 创建当前 Runtime 唯一 Hertz Server 的纯内存生命周期适配器。
// 阶段角色：Module.Workloads 收集阶段，不解析 IOC 或启动监听。
func newServerWorkload() baseapp.Workload {
	return &serverWorkload{}
}

// Name 返回 Hertz 主 Server 的稳定工作负载名称。
func (this *serverWorkload) Name() string {
	return workloadName
}

// Run 解析唯一 web.Server，并阻塞到其运行周期完全收敛。
func (this *serverWorkload) Run(ctx context.Context, runtime baseapp.Runtime, _ baseapp.CommandContext) error {
	server, err := resolveServer(runtime, "app_web_hertz.workload.run")
	if err != nil {
		return err
	}
	return server.Run(ctx)
}

// Shutdown 解析同一 web.Server，并显式请求其优雅停止。
// 阶段角色：只发出语义化停止请求；app 监督器仍会等待 Run 返回后才释放 IOC Resource。
func (this *serverWorkload) Shutdown(ctx context.Context, runtime baseapp.Runtime, _ baseapp.CommandContext) error {
	server, err := resolveServer(runtime, "app_web_hertz.workload.shutdown")
	if err != nil {
		return err
	}
	return server.Shutdown(ctx)
}

// resolveServer 从本次 app Runtime 解析唯一 web.Server，并保留分层错误链。
// 阶段角色：Workload Run/Shutdown 共用的 IOC 解析边界。
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
