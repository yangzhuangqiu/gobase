package config

import (
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/yangzhuangqiu/gobase/config/provider"
)

// Option 定义 Config Builder 的声明式配置契约。
//
// 典型调用场景：直接传给 New，或由 app-runtime.WithConfig 保存并在每次 Build 时重放。
// Option 只描述稳定装配，不应在应用阶段获取文件、网络或 watcher 资源。
type Option interface {
	apply(builder *Builder)
}

type optionFunc func(builder *Builder)

// apply 把单项声明写入本次独立 Builder。
func (this optionFunc) apply(builder *Builder) {
	this(builder)
}

// FileOption 定义单个文件来源声明的配置契约。
//
// 典型调用场景：传给 File，在同一声明内确定文件路径、启用状态和监听策略。
// FileOption 只修改文件声明对象，不读取文件或启动 watcher。
type FileOption interface {
	applyFile(file *fileOptions)
}

type fileOptionFunc func(file *fileOptions)

// applyFile 把单项文件策略写入本次 File 声明。
func (this fileOptionFunc) applyFile(file *fileOptions) {
	this(file)
}

// fileOptions 保存单个 File Option 归一化前的声明状态。
type fileOptions struct {
	// disabled 控制是否禁止读取显式文件和执行默认发现，默认 false。
	disabled bool
	// paths 是按声明顺序追加的显式文件路径；空值表示继续使用默认发现。
	paths []string
	// pathsSet 表示调用方是否显式声明过 FilePath，用于单次 Input 的字段级覆盖。
	pathsSet bool
	// watch 控制是否启用本地文件轮询监听，默认 false。
	watch bool
	// watchInterval 是显式轮询周期；非正值表示从文件声明读取并最终回退 30s。
	watchInterval time.Duration
}

// Input 描述一次构建才确定的配置输入，不属于可复用应用蓝图。
type Input struct {
	// BootstrapOverrides 是 provider 建连、Sources 和配置文件路径的启动覆盖。
	BootstrapOverrides map[string]any
	// PinnedValues 是最终业务快照的固定覆盖，优先级高于 default/file/remote。
	PinnedValues map[string]any
	// EnvPrefix 是 bootstrap 环境变量前缀；为空时由上层应用身份派生。
	EnvPrefix string
	// File 是本次文件策略覆盖；通常只由 CLI/宿主传入 FileDisabled() 单向收紧。
	File []FileOption
}

// Options 把单次输入转换为可追加到稳定声明后的 Config Options。
//
// 典型调用场景：app-runtime.BuildWithInput 在创建本次 Builder 前调用。
// 返回的 Option 保持 bootstrap/pinned map 的按 Build 归一化语义。
func (this Input) Options() []Option {
	// 阶段一：只把显式单次输入转成 Option，避免空值覆盖稳定声明。
	result := make([]Option, 0, 4)
	if strings.TrimSpace(this.EnvPrefix) != "" {
		result = append(result, BootstrapEnvPrefix(this.EnvPrefix))
	}
	if len(this.BootstrapOverrides) > 0 {
		result = append(result, BootstrapOverrides(this.BootstrapOverrides))
	}
	if len(this.PinnedValues) > 0 {
		result = append(result, PinnedValues(this.PinnedValues))
	}
	if len(this.File) > 0 {
		// 阶段二：单次文件策略最后应用；底层禁用状态仍保持单向收紧。
		options := append([]FileOption{}, this.File...)
		result = append(result, optionFunc(func(builder *Builder) {
			declaration := collectFileOptions(options)
			builder.withInputFile(declaration.disabled, declaration.paths, declaration.pathsSet, declaration.watch, declaration.watchInterval)
		}))
	}
	return result
}

// Defaults 设置最低优先级默认配置层。
// 参数 value 通常为带 yaml tag 的结构体或 map；后声明覆盖此前默认层。
func Defaults(value any) Option {
	return optionFunc(func(builder *Builder) {
		builder.withDefaults(value)
	})
}

// File 创建统一的本地文件来源声明。
//
// 语义：
// 1. 默认启用文件并执行默认发现，不启用监听；
// 2. FilePath 按声明顺序追加显式路径；
// 3. FileDisabled 严格禁用文件读取与默认发现；
// 4. FileWatch 启用轮询监听；禁用状态同时声明 path/watch 会在 Build 前失败。
func File(options ...FileOption) Option {
	return optionFunc(func(builder *Builder) {
		declaration := collectFileOptions(options)
		builder.withFile(declaration.disabled, declaration.paths, declaration.watch, declaration.watchInterval)
	})
}

// collectFileOptions 按声明顺序归一化一组文件策略。
// 阶段角色：稳定 File 声明与单次 Input 共用同一组选项语义。
func collectFileOptions(options []FileOption) *fileOptions {
	result := &fileOptions{}
	for _, option := range options {
		if option != nil {
			option.applyFile(result)
		}
	}
	return result
}

// FilePath 创建有序显式文件路径声明。
// 空白路径被忽略；多次调用或一次传入多个路径都会按输入顺序追加。
func FilePath(paths ...string) FileOption {
	return fileOptionFunc(func(file *fileOptions) {
		file.pathsSet = true
		for _, path := range paths {
			path = strings.TrimSpace(path)
			if path != "" {
				file.paths = append(file.paths, path)
			}
		}
	})
}

// FileDisabled 创建文件能力严格禁用声明。
// 禁用与任意显式路径或监听策略同时存在时，Build 获取资源前失败。
func FileDisabled() FileOption {
	return fileOptionFunc(func(file *fileOptions) {
		file.disabled = true
	})
}

// FileWatch 创建本地文件轮询监听声明。
//
// 参数：
// - interval：首个正值作为固定间隔；未传或非正值时读取文件声明并回退 30s。
func FileWatch(interval ...time.Duration) FileOption {
	return fileOptionFunc(func(file *fileOptions) {
		file.watch = true
		file.watchInterval = 0
		if len(interval) > 0 && interval[0] > 0 {
			file.watchInterval = interval[0]
		}
	})
}

// BootstrapEnvPrefix 设置 bootstrap 环境变量前缀。
// 该入口主要面向 app-runtime 和嵌入式适配层；普通应用通常由应用名派生。
func BootstrapEnvPrefix(prefix string) Option {
	return optionFunc(func(builder *Builder) {
		builder.withBootstrapEnvPrefix(prefix)
	})
}

// BootstrapOverrides 合并启动阶段覆盖值。
// dotted key 与嵌套 map 均受支持；后声明同 key 覆盖前声明。
func BootstrapOverrides(values map[string]any) Option {
	return optionFunc(func(builder *Builder) {
		builder.withBootstrapOverrides(values)
	})
}

// PinnedValues 合并最终业务快照的固定覆盖。
// 该层优先级高于 default/file/remote，通常只用于 CLI 或测试输入。
func PinnedValues(values map[string]any) Option {
	return optionFunc(func(builder *Builder) {
		builder.withPinnedValues(values)
	})
}

// Provider 创建单个远端 Provider 注册声明。
// nil 被忽略；名称为空或重复会在 Build 获取资源前 fail-fast。
func Provider(item provider.Provider) Option {
	return optionFunc(func(builder *Builder) {
		builder.withProvider(item)
	})
}

// OnRefreshError 设置异步 refresh 失败观测回调。
// 回调应快速、非阻塞且不得记录凭据；后声明覆盖此前回调。
func OnRefreshError(handler func(error)) Option {
	return optionFunc(func(builder *Builder) {
		builder.withRefreshErrorHandler(handler)
	})
}

// SnapshotValidators 追加发布前原始快照校验器。
// 校验器按声明顺序执行，任一失败都会拒绝首次或刷新快照。
func SnapshotValidators(validators ...SnapshotValidator) Option {
	return optionFunc(func(builder *Builder) {
		builder.withSnapshotValidator(validators...)
	})
}

// Validate 把完整快照解码为 T，并在首次发布和 refresh 发布前执行类型化校验。
//
// 参数：
// - validator：接收本次候选快照的独立 T；nil 表示不声明校验。
//
// 返回：
// - Option：解码失败使用 cfg.decode.failed，业务错误通过 errors.Join 保留。
func Validate[T any](validator func(*T) []error) Option {
	return optionFunc(func(builder *Builder) {
		if validator == nil {
			return
		}
		targetType := reflect.TypeOf((*T)(nil)).Elem()
		builder.withSnapshotValidator(func(snapshot Snapshot) error {
			target := new(T)
			if err := snapshot.Decode("", target); err != nil {
				return errorForConfigDecodeFailed(messagef("op=config.validate result=failed stage=decode scope= target_type=%s", targetType), err)
			}
			return errors.Join(validator(target)...)
		})
	})
}

// BootstrapSchemas 返回 Options 声明的 Provider bootstrap schema 副本。
// 典型调用场景：CLI 适配层在 Build 前生成可发现 flags，不触发 Provider.Open。
func BootstrapSchemas(options ...Option) map[string][]provider.BootstrapKey {
	// 阶段一：只应用内存声明，不执行 Open 或建立连接。
	builder := New(options...)
	result := make(map[string][]provider.BootstrapKey)
	// 阶段二：仅收集声明可发现能力的 Provider，并复制返回 schema。
	for name, item := range builder.providers {
		schemaProvider, ok := item.(provider.BootstrapSchemaProvider)
		if !ok {
			continue
		}
		schema := schemaProvider.BootstrapSchema()
		if len(schema) > 0 {
			result[strings.TrimSpace(name)] = append([]provider.BootstrapKey{}, schema...)
		}
	}
	return result
}
