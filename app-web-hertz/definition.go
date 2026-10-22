package appwebhertz

import "github.com/yangzhuangqiu/gobase/ioc"

// DefineController 把 Controller 与其 IOC 接口声明绑定为原子 Bean Definition。
//
// 参数：
// - controller：必须实现 Controller，nil 或 typed nil 在 Runtime 声明或 IOC 注册阶段失败；
// - options：可追加名称、注入和生命周期所有权策略。
func DefineController(controller Controller, options ...ioc.RegisterOption) ioc.Definition {
	resolved := append([]ioc.RegisterOption{}, options...)
	resolved = append(resolved, ioc.WithDeclare[Controller]())
	return ioc.Define(controller, resolved...)
}

// DefineFilter 把 Filter 与其 IOC 接口声明绑定为原子 Bean Definition。
//
// 参数：
// - filter：必须实现 Filter；同时实现 GroupedFilter 时，Server 装配阶段按逻辑分组处理；
// - options：可追加名称、注入和生命周期所有权策略。
func DefineFilter(filter Filter, options ...ioc.RegisterOption) ioc.Definition {
	resolved := append([]ioc.RegisterOption{}, options...)
	resolved = append(resolved, ioc.WithDeclare[Filter]())
	return ioc.Define(filter, resolved...)
}

// DefineNoRouteHandler 把 404 Handler 与其 IOC 接口声明绑定为原子 Bean Definition。
//
// 参数：
// - handler：必须实现 NoRouteHandler，nil 或 typed nil 在 Runtime 声明或 IOC 注册阶段失败；
// - options：可追加名称、注入和生命周期所有权策略。
func DefineNoRouteHandler(handler NoRouteHandler, options ...ioc.RegisterOption) ioc.Definition {
	resolved := append([]ioc.RegisterOption{}, options...)
	resolved = append(resolved, ioc.WithDeclare[NoRouteHandler]())
	return ioc.Define(handler, resolved...)
}

// DefineNoMethodHandler 把 405 Handler 与其 IOC 接口声明绑定为原子 Bean Definition。
//
// 参数：
// - handler：必须实现 NoMethodHandler，nil 或 typed nil 在 Runtime 声明或 IOC 注册阶段失败；
// - options：可追加名称、注入和生命周期所有权策略。
func DefineNoMethodHandler(handler NoMethodHandler, options ...ioc.RegisterOption) ioc.Definition {
	resolved := append([]ioc.RegisterOption{}, options...)
	resolved = append(resolved, ioc.WithDeclare[NoMethodHandler]())
	return ioc.Define(handler, resolved...)
}
