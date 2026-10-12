package provider

import "context"

// Provider 定义远端配置中心的扩展契约。
//
// 设计目标：Builder 只依赖该接口，不内置具体厂商解析与建连逻辑。
type Provider interface {
	// Name 返回 provider 稳定标识（例如 nacos/apollo/etcd）。
	Name() string
	// Open 根据 bootstrap map 打开一次独立运行会话。
	//
	// 说明：
	// 1. ctx 控制本次会话建立的取消边界；
	// 2. 入参 values 是完整 bootstrap 合并结果（不是 provider 局部子块）；
	// 3. provider 应只解析自身命名空间（例如 nacos/consul 对应根键）。
	//
	// 返回：
	// - Session: 本次构建独占的连接与 Source 集合；nil 表示未启用。
	// - error: bootstrap 语义非法或建连失败时返回错误。
	//
	// 语义：
	// 1. 未声明该 provider 时返回 nil Session 与 nil 错误；
	// 2. 声明非法时 fail-fast 返回错误；
	// 3. 声明合法且启用时返回非 nil Session，其生命周期移交给 Config Manager。
	Open(ctx context.Context, values map[string]any) (Session, error)
}

// BootstrapSchemaProvider 定义 provider 可选的 bootstrap 键发现能力。
//
// 典型调用场景:
// 1. Config Builder 使用 schema 映射标量环境变量并校验类型；
// 2. App 使用 schema 输出可发现的 bootstrap CLI 契约；
// 3. 解析 bootstrap 的 Provider 必须实现本接口，以声明允许的根命名空间；
// 4. 不解析 bootstrap 的 Provider 无需实现本接口。
type BootstrapSchemaProvider interface {
	// BootstrapSchema 返回该 provider 的 bootstrap 键定义，用于 CLI 可发现性输出。
	BootstrapSchema() []BootstrapKey
}

// BootstrapEnvLoader 定义 provider 可选的 bootstrap env 扩展点。
//
// 语义：
// 1. 该接口是可选能力，Builder 会先按 schema 自动映射 env，再按 type assertion 调用本接口补充覆盖；
// 2. 用于表达 schema 自动映射难以覆盖的场景（例如 indexed list：SOURCE_0/SOURCE_1）；
// 3. 返回值与 config.BootstrapOverrides 语义一致，可使用 dotted key 或嵌套 map。
type BootstrapEnvLoader interface {
	// LoadBootstrapFromEnv 返回 provider 的额外 bootstrap 环境变量映射。
	//
	// 参数：
	// - envPrefix: 已标准化为大写下划线前缀（可为空），例如 APP_。
	LoadBootstrapFromEnv(envPrefix string) map[string]any
}

// BootstrapValidator 定义 provider 可选的 bootstrap 语义校验扩展点。
//
// 语义：
// 1. 该接口是可选能力，Builder 会在运行时按 type assertion 调用；
// 2. 调用时机是 stage-one strict bootstrap 校验；
// 3. 用于“仅校验、不建连”的 fail-fast 预检路径；
// 4. 仅在注册的 provider 实现本接口时生效。
type BootstrapValidator interface {
	// ValidateBootstrap 对完整 bootstrap map 执行语义校验。
	ValidateBootstrap(values map[string]any) error
}
