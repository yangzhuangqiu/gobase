package appwebhertz

import "github.com/cloudwego/hertz/pkg/route"

// RouteRegistrar 定义 Controller 可选的 Hertz 原生路由扩展合同。
//
// 典型调用场景:
// 1. Controller 需要注册 `/:id`、通配符或多 Handler 路由；
// 2. Controller 需要使用 group Middleware 或 Hertz 原生路由能力；
// 3. RegisterRoutes 先于同一 Controller 的方法名自动路由执行，两者是叠加关系。
type RouteRegistrar interface {
	// RegisterRoutes 向当前 Controller 独占且 GroupPath 已生效的 RouterGroup 注册原生路由。
	//
	// 参数:
	// - router: GroupPath 已生效的 Hertz RouterGroup；空路径同样使用独立的 `/` group。
	//
	// 返回:
	// - nil: 手动路由声明成功，继续自动注册；
	// - error: 停止当前及后续 Controller，Server 构建失败并保留 cause。
	RegisterRoutes(router *route.RouterGroup) error
}
