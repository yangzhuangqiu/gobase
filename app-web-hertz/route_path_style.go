package appwebhertz

// RoutePathStyle 定义 Controller 方法后缀转换为 URL path segment 的命名风格。
//
// 语义:
// 1. 只影响方法名自动路由，不修改 GroupPath 或 RegisterRoutes 的显式路径；
// 2. 路径始终以 `/` 开头；
// 3. 未声明 Option 时使用 RoutePathStyleKebab。
type RoutePathStyle uint8

const (
	// RoutePathStyleKebab 使用小写中划线，例如 GetHealthCheck 映射为 `/health-check`。
	RoutePathStyleKebab RoutePathStyle = iota
	// RoutePathStyleCamel 使用 lower camel case，例如 GetHealthCheck 映射为 `/healthCheck`。
	RoutePathStyleCamel
	// RoutePathStyleSnake 使用小写下划线，例如 GetHealthCheck 映射为 `/health_check`。
	RoutePathStyleSnake
)
