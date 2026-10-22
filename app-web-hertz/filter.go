package appwebhertz

import (
	"context"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
)

// Filter 定义应用级 Hertz Middleware 的直接执行和稳定排序合同。
//
// 典型调用场景:
// 1. 调用方把认证、recovery、request-id 或 access-log 适配为 Filter；
// 2. Filter 作为 IOC bean 显式声明本接口，由 Module 在 Build 阶段聚合；
// 3. 普通 Filter 注册为 Hertz 全局 Middleware；同时实现 GroupedFilter 时改为逻辑分组 Middleware。
type Filter interface {
	// Order 返回所属作用域内的 Middleware 顺序；数值越小越先进入 DoFilter 链。
	Order() int

	// DoFilter 处理一次 Hertz 请求，并自行决定是否调用 request.Next 继续执行链。
	//
	// 参数:
	// - ctx: Hertz 为当前请求传递的上下文；
	// - request: 当前 Hertz RequestContext，可读写响应并控制 Middleware 链。
	DoFilter(ctx context.Context, request *hertzapp.RequestContext)
}
