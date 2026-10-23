package applogzap

import (
	"context"
	"strings"

	"github.com/yangzhuangqiu/gobase/config"
	baselog "github.com/yangzhuangqiu/gobase/log"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
)

const (
	// DefaultManagerScope 是 app-log-zap 从 ConfigManager 读取启动配置并订阅动态 level 的默认路径。
	DefaultManagerScope = "log.zap"
	// defaultLogScope 是包内默认值别名，保持 Option 归一化只有一个事实源。
	defaultLogScope = DefaultManagerScope
)

// Option 定义 app-log-zap bootstrap 可选参数。
type Option func(*Bootstrap)

// Bootstrap 定义 app-runtime 的 zap 日志装配实现。
//
// 语义：
// 1. 启动阶段从 config.Manager 的指定 scope 读取 log-zap.Config；
// 2. 运行期由 app-log-zap 持有 manager subscription，并只向核心回推动态 level；
// 3. 订阅与 Zap 核心组合为单个 Provider，由 app-runtime 统一收口生命周期。
type Bootstrap struct {
	// logScope 是 manager 中读取 Zap 配置的 dotted scope。
	logScope string
	// providerOptions 在每次 Build 时转发给新建 Provider；构造后按只读约定使用。
	providerOptions []logzap.Option
}

// WithScope 覆盖日志配置 scope，默认值是 "log.zap"。
func WithScope(scope string) Option {
	return func(bootstrap *Bootstrap) {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			return
		}
		bootstrap.logScope = scope
	}
}

// WithProviderOptions 为每次 Runtime Build 创建的 Zap Provider 注册构造期策略。
//
// 典型调用场景：注入 typed ContextExtractor 或 Redactor，同时保留 app-runtime 生命周期托管。
func WithProviderOptions(options ...logzap.Option) Option {
	return func(bootstrap *Bootstrap) {
		bootstrap.providerOptions = append(bootstrap.providerOptions, options...)
	}
}

// New 创建一个 app-runtime Logging 实现。
func New(options ...Option) *Bootstrap {
	result := &Bootstrap{logScope: defaultLogScope}
	for _, option := range options {
		if option == nil {
			continue
		}
		option(result)
	}
	if strings.TrimSpace(result.logScope) == "" {
		result.logScope = defaultLogScope
	}
	return result
}

// Build 在 Runtime config 构建完成后创建并组合配置订阅与 Zap 核心。
func (this *Bootstrap) Build(ctx context.Context, manager config.Manager) (baselog.Provider, error) {
	if manager == nil {
		provider, err := logzap.NewProvider(logzap.Config{}, this.providerOptions...)
		if err != nil {
			return nil, errorForBootstrapBuildFailed("op=app_log_zap.bootstrap result=failed stage=new_provider reason=%w", err)
		}
		return &runtimeProvider{core: provider}, nil
	}
	provider, err := newRuntimeProvider(ctx, manager, this.logScope, this.providerOptions)
	if err != nil {
		return nil, errorForBootstrapBuildFailed("op=app_log_zap.bootstrap result=failed stage=build_from_manager scope=%s reason=%w", this.logScope, err)
	}
	return provider, nil
}
