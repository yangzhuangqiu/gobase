package appwebhertz

import (
	"context"

	baseapp "github.com/yangzhuangqiu/gobase/app"
)

const (
	// workloadName 是 Hertz 主 Server 在 app 关闭诊断中的稳定语义名称。
	workloadName = "web.hertz"
)

// serverWorkload 把 IOC 中唯一的托管 Server 适配为 app 主动工作负载。
type serverWorkload struct{}

var _ baseapp.Workload = (*serverWorkload)(nil)

// Workload 返回 app-web-hertz 推荐的声明式生命周期适配器。
//
// 典型调用场景：与 NewModule 一起传给 app.WithWorkloads，使 app 显式执行 Run→Shutdown→等待 Run→Resource Close。
func Workload() baseapp.Workload {
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
