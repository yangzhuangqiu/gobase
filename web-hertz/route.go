package webhertz

import "github.com/cloudwego/hertz/pkg/route"

// RouteFunc 定义构造阶段使用 Hertz 原生 Router 注册路由的函数合同。
//
// 典型调用场景:
// 1. 独立应用通过 WithRoutes 显式声明路由；
// 2. 上层应用适配器把 Controller 转换为 RouteFunc；
// 3. 路由函数直接使用 Hertz Group/GET/POST/Use，不经过平行路由抽象。
//
// 参数:
// - router: 生命周期隔离的 Hertz 根路径 RouterGroup，只用于构造阶段注册路由。
//
// 返回:
// - nil: 当前函数拥有的路由全部注册成功；
// - error: 停止后续 RouteFunc，New 不返回半初始化 Server。
type RouteFunc func(router *route.RouterGroup) error
