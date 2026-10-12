package webhertz

import "github.com/cloudwego/hertz/pkg/app"

// WithNoRoute 追加全局路由未匹配时执行的 Hertz Handler。
//
// 典型调用场景:
// 1. 应用统一输出结构化 404 响应；
// 2. 上层适配器安装未匹配请求的审计或错误码映射。
//
// 参数:
// - handlers: 按 Option 和参数声明顺序追加；任一 nil 在构造阶段 fail-fast。
//
// 语义:
// 1. Handler 继承全部全局 Middleware；
// 2. 多次调用采用追加而非覆盖；
// 3. 未声明时保留 Hertz 默认 404 行为。
func WithNoRoute(handlers ...app.HandlerFunc) Option {
	return func(target *buildOptions) error {
		for index, handler := range handlers {
			if handler == nil {
				return errorForDeclarationInvalid(messagef("op=web_hertz.option result=failed stage=declare_no_route reason=handler_required handler_index=%d", index))
			}
			target.noRoute = append(target.noRoute, handler)
		}
		return nil
	}
}
