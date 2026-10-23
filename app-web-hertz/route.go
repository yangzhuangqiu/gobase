package appwebhertz

import (
	"github.com/cloudwego/hertz/pkg/route"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

// controllerRoutePlan 保存一个 Controller 的分组、手动扩展与自动路由计划。
type controllerRoutePlan struct {
	// controllerIndex 是输入或 IOC 聚合顺序，用于错误定位。
	controllerIndex int
	// groupPath 是归一化后的 Controller RouterGroup 前缀；空声明统一表示为 `/`。
	groupPath string
	// registrar 是 Controller 可选实现的原生路由扩展。
	registrar RouteRegistrar
	// automaticRoutes 按导出方法名顺序保存自动路由。
	automaticRoutes []automaticRoute
}

// prepareControllers 校验 Controller 并构建保持输入顺序的不可变路由计划。
// 阶段角色：只做反射扫描，不执行 RegisterRoutes 或修改 Hertz Router。
func prepareControllers(controllers []Controller, style RoutePathStyle) ([]*controllerRoutePlan, error) {
	plans := make([]*controllerRoutePlan, 0, len(controllers))
	for index, controller := range controllers {
		if isNilInterface(controller) {
			return nil, errorForDeclarationInvalid(messagef("op=app_web_hertz.build result=failed stage=validate_controllers reason=controller_required controller_index=%d", index))
		}
		plan, err := buildControllerRoutePlan(controller, index, style)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan)
	}
	return plans, nil
}

// prepareRoutes 合并 Controller 与分组 Filter，生成一次性的 Hertz 路由装配入口。
// 阶段角色：共享基础分组只由框架创建和缓存；Controller 只接收无路径子分组。
func prepareRoutes(controllers []Controller, groupedFilters []groupedFilterPlan, style RoutePathStyle) ([]webhertz.RouteFunc, error) {
	plans, err := prepareControllers(controllers, style)
	if err != nil {
		return nil, err
	}
	if len(plans) == 0 && len(groupedFilters) == 0 {
		return nil, nil
	}

	register := func(router *route.RouterGroup) error {
		groups := make(map[string]*route.RouterGroup, len(groupedFilters)+len(plans))

		// 第一阶段：每个逻辑路径只创建一个框架持有的基础分组，并一次性安装分组 Filter。
		for _, filterGroup := range groupedFilters {
			groups[filterGroup.groupPath] = router.Group(filterGroup.groupPath, filterGroup.handlers...)
		}
		// 第二阶段：Controller 从共享基础分组派生无路径子分组，隔离自身通过 Use 增加的 Middleware。
		for _, plan := range plans {
			baseGroup, exists := groups[plan.groupPath]
			if !exists {
				baseGroup = router.Group(plan.groupPath)
				groups[plan.groupPath] = baseGroup
			}
			if err := plan.register(baseGroup.Group("")); err != nil {
				return err
			}
		}
		return nil
	}
	return []webhertz.RouteFunc{register}, nil
}

// buildControllerRoutePlan 解析单个 Controller 的分组、可选手动注册和自动路由。
// 阶段角色：Server 构造前置校验；没有任何路由能力的 Controller 会 fail-fast。
func buildControllerRoutePlan(controller Controller, index int, style RoutePathStyle) (*controllerRoutePlan, error) {
	groupPath, valid := normalizeGroupPath(controller.GroupPath())
	if !valid {
		return nil, errorForDeclarationInvalid(messagef("op=app_web_hertz.build result=failed stage=validate_controller_group reason=leading_slash_required controller_index=%d group_path=%q", index, groupPath))
	}

	// 手动注册是可选能力；自动扫描始终执行，两种声明最终叠加到同一个分组。
	registrar, _ := controller.(RouteRegistrar)
	automaticRoutes, err := scanAutomaticRoutes(controller, index, style)
	if err != nil {
		return nil, err
	}
	if registrar == nil && len(automaticRoutes) == 0 {
		return nil, errorForDeclarationInvalid(messagef("op=app_web_hertz.build result=failed stage=validate_controller_routes reason=route_required controller_index=%d controller_type=%T", index, controller))
	}
	return &controllerRoutePlan{
		controllerIndex: index,
		groupPath:       groupPath,
		registrar:       registrar,
		automaticRoutes: automaticRoutes,
	}, nil
}

// register 在 Hertz 构造期把一个 Controller 计划注册到其独占子 RouterGroup。
// 阶段角色：先执行可选 RegisterRoutes，再按稳定顺序叠加方法名自动路由。
func (this *controllerRoutePlan) register(target *route.RouterGroup) error {
	// 手动声明优先，使动态路由和 group Middleware 在自动静态路由前完成装配。
	if this.registrar != nil {
		if err := this.registrar.RegisterRoutes(target); err != nil {
			return errorForRouteRegistrationFailed(
				messagef("op=app_web_hertz.register_routes result=failed stage=manual controller_index=%d group_path=%q reason=registrar_failed", this.controllerIndex, this.groupPath),
				err,
			)
		}
	}
	for _, item := range this.automaticRoutes {
		for _, verb := range item.verbs {
			if verb.any {
				target.Any(item.path, item.handler)
				continue
			}
			target.Handle(verb.method, item.path, item.handler)
		}
	}
	return nil
}
