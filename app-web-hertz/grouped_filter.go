package appwebhertz

// GroupedFilter 定义绑定到逻辑路由分组的应用 Middleware 合同。
//
// 典型调用场景:
// 1. 同一 GroupPath 下的多个 Controller 需要共享认证、租户或限流策略；
// 2. GroupedFilter 仍通过 Filter 切片或 IOC Filter 声明聚合，构造阶段按接口类型识别作用域；
// 3. 根逻辑分组使用空字符串或 `/`，但不会因此升级为 Hertz 全局 Middleware。
type GroupedFilter interface {
	Filter

	// GroupPath 返回 Filter 所属的逻辑分组路径。
	//
	// 返回:
	// - 空字符串或 `/`: 统一归入根逻辑分组 `/`；
	// - 其他值: 必须以 `/` 开头，只作用于同一逻辑分组的 Controller。
	GroupPath() string
}
