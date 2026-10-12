package webhertz

import "github.com/cloudwego/hertz/pkg/app"

// WithNoMethod 追加路由存在但 HTTP Method 不匹配时执行的 Hertz Handler。
//
// 典型调用场景:
// 1. 应用统一输出结构化 405 响应；
// 2. 上层适配器记录 Method 不匹配的稳定失败原因。
//
// 参数:
// - handlers: 按 Option 和参数声明顺序追加；任一 nil 在构造阶段 fail-fast。
//
// 语义:
// 1. 声明后自动启用 HandleMethodNotAllowed，保证 Handler 必达；
// 2. Handler 继承全部全局 Middleware；
// 3. 多次调用采用追加而非覆盖；未声明时保留 Config/Hertz 默认行为。
func WithNoMethod(handlers ...app.HandlerFunc) Option {
	return func(target *buildOptions) error {
		for index, handler := range handlers {
			if handler == nil {
				return errorForDeclarationInvalid(messagef("op=web_hertz.option result=failed stage=declare_no_method reason=handler_required handler_index=%d", index))
			}
			target.noMethod = append(target.noMethod, handler)
		}
		return nil
	}
}
