package appwebhertz

import (
	"context"
	"sort"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
)

// NoMethodHandler 定义路由存在但 HTTP Method 不匹配时的应用处理合同。
//
// 典型调用场景：应用统一输出结构化 405 响应，或在最终响应前记录 Method 不匹配原因。
type NoMethodHandler interface {
	// Order 返回 fallback 链中的稳定顺序；数值越小越先执行。
	Order() int
	// HandleNoMethod 处理 Method 不匹配请求，并自行决定是否调用 request.Next 继续后续 Handler。
	//
	// 参数：
	// - ctx：Hertz 为当前请求传递的上下文；
	// - request：当前 Hertz RequestContext，可读取请求并写入最终 405 响应。
	HandleNoMethod(ctx context.Context, request *hertzapp.RequestContext)
}

// noMethodHandlerEntry 保存 NoMethodHandler 的稳定排序信息。
type noMethodHandlerEntry struct {
	// order 是声明的 fallback 优先级，数值越小越先执行。
	order int
	// inputIndex 是 IOC 注册顺序，用于相同 order 的稳定排序。
	inputIndex int
	// handler 是直接交给 web-hertz 的原生方法值。
	handler hertzapp.HandlerFunc
}

// prepareNoMethodHandlers 校验并按 Order 把应用 405 合同转换为 Hertz Handler。
// 阶段角色：IOC 依赖已初始化后、provider 组装前冻结全局 NoMethod 链。
func prepareNoMethodHandlers(handlers []NoMethodHandler) ([]hertzapp.HandlerFunc, error) {
	// 第一阶段：在 provider 构造前拒绝 typed nil，并保存 IOC 注册顺序作为稳定次序。
	entries := make([]noMethodHandlerEntry, 0, len(handlers))
	for index, handler := range handlers {
		if isNilInterface(handler) {
			return nil, errorForDeclarationInvalid(messagef("op=app_web_hertz.server.init result=failed stage=validate_no_method_handlers reason=handler_required handler_index=%d", index))
		}
		entries = append(entries, noMethodHandlerEntry{order: handler.Order(), inputIndex: index, handler: handler.HandleNoMethod})
	}
	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].order == entries[right].order {
			return entries[left].inputIndex < entries[right].inputIndex
		}
		return entries[left].order < entries[right].order
	})
	// 第二阶段：排序完成后只向 web-hertz 暴露原生方法值，不泄漏应用组件合同。
	result := make([]hertzapp.HandlerFunc, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.handler)
	}
	return result, nil
}
