package appwebhertz

// Controller 定义应用组件的路由分组和自动路由发现合同。
//
// 典型调用场景:
// 1. 调用方把 Controller 切片传给 New，按输入顺序完成分组和路由装配；
// 2. Controller 作为 IOC bean 显式声明本接口，由 Module 在 Build 阶段按注册顺序聚合；
// 3. 简单静态路由由 Get/Post/GetOrPost 等方法名自动注册，动态路由通过 RouteRegistrar 显式注册。
type Controller interface {
	// GroupPath 返回当前 Controller 的公共路由前缀。
	//
	// 返回:
	// - 空字符串或 `/`: 不增加 URL 前缀，但仍创建当前 Controller 独占的根路径 RouterGroup；
	// - 其他值: 必须以 `/` 开头，作为当前 Controller 独占的 RouterGroup 前缀。
	GroupPath() string
}
