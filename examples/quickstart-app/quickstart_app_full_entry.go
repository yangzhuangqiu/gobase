package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/yangzhuangqiu/gobase/app"
	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/config/provider"
	"github.com/yangzhuangqiu/gobase/errcode"
	"github.com/yangzhuangqiu/gobase/ioc"
	// 如需启用真实远端 provider，可取消注释：
	// configconsul "github.com/yangzhuangqiu/gobase/config-consul"
	// confignacos "github.com/yangzhuangqiu/gobase/config-nacos"
)

type quickstartFullErrorCode = errcode.StringCode

const (
	errCodeQuickstartFullInvalidArg quickstartFullErrorCode = "EXAMPLE_QUICKSTART_FULL_INVALID_ARG"
	errCodeQuickstartFullConfigBad  quickstartFullErrorCode = "EXAMPLE_QUICKSTART_FULL_CONFIG_BAD"
)

// quickstartFullConfig 是全量入口的运行时配置结构。
//
// 语义：
// 1. 同时承接 default/file/remote/pinned 多层 merge；
// 2. root run 通过 Current().Decode("", cfg) 获取最终生效配置。
type quickstartFullConfig struct {
	Service struct {
		Name    string `yaml:"name"`
		Address string `yaml:"address"`
		Timeout string `yaml:"timeout"`
	} `yaml:"service"`
	Worker struct {
		Concurrency int `yaml:"concurrency"`
		MaxQueue    int `yaml:"max_queue"`
	} `yaml:"worker"`
	Feature struct {
		FromProviderRemote bool `yaml:"from_provider_remote"`
		FromFixedPlan      bool `yaml:"from_fixed_plan"`
	} `yaml:"feature"`
}

// quickstartFullRootFlags 演示 CommandFlags 全类型参数声明。
type quickstartFullRootFlags struct {
	// RetryBudget 是仅控制当前命令的重试预算，默认 3。
	RetryBudget uint
	// RequestLimit 是仅控制当前命令的请求限额，默认 1000。
	RequestLimit uint64
	// Debug 控制当前命令是否输出调试信息，默认关闭。
	Debug bool
	// SampleRate 是采样率，约束范围为 [0,1]，默认 1。
	SampleRate float64
	// GracefulTimeout 是资源优雅关闭等待时间，默认 3 秒。
	GracefulTimeout time.Duration
	// Tags 是可重复声明的命令标签集合，Complete 会负责规范化。
	Tags []string
	// Profile 是运行环境标识，空值会在 Complete 阶段补全为 dev。
	Profile string
}

// Bind 声明 root 的配置覆盖 Flag 和普通业务 Flag。
func (this *quickstartFullRootFlags) Bind(flags app.CommandFlags) {
	flags.ConfigString("service.address", "业务服务监听地址，会进入 pinned 层覆盖。")
	flags.ConfigInt("worker.concurrency", "worker 并发度，会进入 pinned 层覆盖。")
	flags.ConfigInt64("worker.max-queue", "worker 队列上限，会进入 pinned 层覆盖。")
	flags.Uint("retry.budget", &this.RetryBudget, 3, "重试预算（示例参数，仅用于演示 uint 绑定）。")
	flags.Uint64("request.limit", &this.RequestLimit, 1000, "请求限额（示例参数，仅用于演示 uint64 绑定）。")
	flags.Bool("debug", &this.Debug, false, "是否启用调试输出。")
	flags.Float64("sample.rate", &this.SampleRate, 1.0, "采样率，范围 [0,1]。")
	flags.Duration("graceful-timeout", &this.GracefulTimeout, 3*time.Second, "优雅退出等待时间。")
	flags.StringArray("tags", &this.Tags, []string{"quickstart", "full"}, "标签列表（可重复指定）。")
	flags.String("profile", &this.Profile, "", "运行 profile（空值时 Complete 自动补全为 dev）。")
}

// Complete 在 Flag 校验前补全 profile 并规范化标签集合。
func (this *quickstartFullRootFlags) Complete() error {
	if strings.TrimSpace(this.Profile) == "" {
		this.Profile = "dev"
	}
	normalized := make([]string, 0, len(this.Tags))
	seen := make(map[string]struct{}, len(this.Tags))
	for _, item := range this.Tags {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; exists {
			continue
		}
		seen[item] = struct{}{}
		normalized = append(normalized, item)
	}
	sort.Strings(normalized)
	this.Tags = normalized
	return nil
}

// ValidateFlags 校验 root 普通业务 Flag 的组合约束。
func (this *quickstartFullRootFlags) ValidateFlags() []error {
	var errs []error
	if this.SampleRate < 0 || this.SampleRate > 1 {
		errs = append(errs, errcode.New[quickstartFullErrorCode](errCodeQuickstartFullInvalidArg, "sample.rate must be in [0,1]"))
	}
	return errs
}

type quickstartFullExplainFlags struct {
	// Key 是需要查询来源信息的 dotted 配置键，默认 service.address。
	Key string
}

// Bind 声明 explain 子命令的业务 Flag。
func (this *quickstartFullExplainFlags) Bind(flags app.CommandFlags) {
	flags.String("key", &this.Key, "service.address", "需要 explain 的配置键（dotted key）。")
}

// ValidateFlags 校验 explain 查询键非空。
func (this *quickstartFullExplainFlags) ValidateFlags() []error {
	if strings.TrimSpace(this.Key) == "" {
		return []error{errcode.New[quickstartFullErrorCode](errCodeQuickstartFullInvalidArg, "--key is required")}
	}
	return nil
}

// quickstartDirectDemoProvider 演示 app.WithConfig(config.Provider(...)) 的 Provider 注册入口。
type quickstartDirectDemoProvider struct{}

func (this *quickstartDirectDemoProvider) Name() string {
	return "directdemo"
}

// Open 返回 nil Session；本 Provider 仅用于演示 schema 发现能力。
func (this *quickstartDirectDemoProvider) Open(_ context.Context, _ map[string]any) (provider.Session, error) {
	return nil, nil
}

func (this *quickstartDirectDemoProvider) ValidateBootstrap(_ map[string]any) error {
	return nil
}

func (this *quickstartDirectDemoProvider) BootstrapSchema() []provider.BootstrapKey {
	return []provider.BootstrapKey{
		{
			Key:          "directdemo.token",
			Type:         provider.BootstrapSchemaTypeString,
			Required:     false,
			DefaultValue: "",
			Description:  "演示 direct provider 的 schema 暴露。",
		},
	}
}

// quickstartBootstrapDemoProvider 演示根据 bootstrap 动态打开 Session 的 Provider。
type quickstartBootstrapDemoProvider struct{}

func (this *quickstartBootstrapDemoProvider) Name() string {
	return "mockdemo"
}

// Open 在 bootstrap 启用时创建包含静态远端内容的演示 Session。
func (this *quickstartBootstrapDemoProvider) Open(_ context.Context, values map[string]any) (provider.Session, error) {
	enabled, _ := readBootstrapBool(values, "mockdemo", "enabled")
	if !enabled {
		return nil, nil
	}
	return &quickstartStaticRemoteSession{
		client: &quickstartStaticRemoteClient{
			content: []byte("feature:\n  from_provider_remote: true\nservice:\n  timeout: 2s\n"),
		},
		source: quickstartStaticRemoteSource{name: "main", priority: 80, optional: true},
	}, nil
}

func (this *quickstartBootstrapDemoProvider) ValidateBootstrap(values map[string]any) error {
	root, exists := values["mockdemo"]
	if !exists {
		return nil
	}
	block, ok := root.(map[string]any)
	if !ok {
		return errcode.New[quickstartFullErrorCode](errCodeQuickstartFullInvalidArg, "bootstrap.mockdemo must be object")
	}
	if raw, exists := block["enabled"]; exists {
		if _, ok = raw.(bool); !ok {
			return errcode.New[quickstartFullErrorCode](errCodeQuickstartFullInvalidArg, "bootstrap.mockdemo.enabled must be bool")
		}
	}
	return nil
}

func (this *quickstartBootstrapDemoProvider) BootstrapSchema() []provider.BootstrapKey {
	return []provider.BootstrapKey{
		{
			Key:          "mockdemo.enabled",
			Type:         provider.BootstrapSchemaTypeBool,
			Required:     false,
			DefaultValue: "false",
			Description:  "是否启用 mockdemo 远端计划。",
		},
	}
}

// quickstartStaticRemoteClient 用于示例中的“本地静态远端内容”。
type quickstartStaticRemoteClient struct {
	content []byte
}

// quickstartStaticRemoteSource 是示例 Provider 暴露给 Config 内核的中立 Source。
type quickstartStaticRemoteSource struct {
	name     string
	priority int
	optional bool
}

// Info 返回示例 Source 的稳定元信息。
func (this quickstartStaticRemoteSource) Info() provider.SourceInfo {
	return provider.SourceInfo{Name: this.name, Priority: this.priority, Optional: this.optional}
}

// quickstartStaticRemoteSession 聚合静态 Client、Source 和会话生命周期。
type quickstartStaticRemoteSession struct {
	client *quickstartStaticRemoteClient
	source quickstartStaticRemoteSource
}

// Sources 返回静态演示 Source。
func (this *quickstartStaticRemoteSession) Sources() []provider.Source {
	return []provider.Source{this.source}
}

// Load 拉取静态演示内容。
func (this *quickstartStaticRemoteSession) Load(ctx context.Context, _ provider.Source) ([]byte, error) {
	return this.client.GetConfig(ctx, "", "")
}

// Watch 注册静态演示 watcher；当前 Client 不产生变化事件。
func (this *quickstartStaticRemoteSession) Watch(ctx context.Context, _ provider.Source, onChange func()) (io.Closer, error) {
	return this.client.WatchConfig(ctx, "", "", func(_ []byte) { onChange() })
}

// Close 结束静态 Session；示例 Client 无独立资源。
func (this *quickstartStaticRemoteSession) Close() error { return nil }

// quickstartStaticRemoteProvider 演示调用方直接提供固定远端内容的 Provider。
type quickstartStaticRemoteProvider struct {
	name   string
	client *quickstartStaticRemoteClient
	source quickstartStaticRemoteSource
}

// Name 返回静态 Provider 的稳定名称。
func (this *quickstartStaticRemoteProvider) Name() string { return this.name }

// Open 为每次 Runtime 构建创建 Session。
func (this *quickstartStaticRemoteProvider) Open(context.Context, map[string]any) (provider.Session, error) {
	return &quickstartStaticRemoteSession{client: this.client, source: this.source}, nil
}

func (this *quickstartStaticRemoteClient) GetConfig(ctx context.Context, groupID, dataID string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result := make([]byte, len(this.content))
	copy(result, this.content)
	return result, nil
}

func (this *quickstartStaticRemoteClient) WatchConfig(_ context.Context, _ string, _ string, _ func(content []byte)) (io.Closer, error) {
	return nil, nil
}

type quickstartFullIOCWorker struct {
	// Config 是 app-runtime 自动注册并注入的 typed 配置视图。
	Config *appruntime.ConfigValue[quickstartFullConfig]
}

func (this *quickstartFullIOCWorker) Init(ctx context.Context) error {
	return ctx.Err()
}

func (this *quickstartFullIOCWorker) Close(context.Context) error {
	return nil
}

func (this *quickstartFullIOCWorker) Status() string {
	return "ready"
}

// runQuickstartAppFull 运行全量入口，覆盖 app.Run 主要参数与扩展点。
func runQuickstartAppFull() error {
	rootFlags := &quickstartFullRootFlags{}
	explainFlags := &quickstartFullExplainFlags{}

	return app.Run(
		// [必须][推荐] app.WithName：命令名，决定 CLI 入口和默认 bootstrap env 前缀。
		app.WithName("quickstart-full"),
		// [可选][推荐] app.WithDescription：帮助文案，便于新人理解用途。
		app.WithDescription("Comprehensive quickstart for gobase/app"),
		// [可选][推荐] app.WithVersion：统一版本输出。
		app.WithVersion("v0.1.0"),

		// [可选][推荐] 高频 Runtime 能力通过 app 门面声明。
		app.WithConfig(
			config.Defaults(defaultFullConfig()),
			config.OnRefreshError(func(err error) {
				_, _ = fmt.Fprintf(os.Stdout, "[refresh-error] %v\n", err)
			}),
			config.Provider(&quickstartDirectDemoProvider{}),
			config.Provider(&quickstartBootstrapDemoProvider{}),
			// [可选][按场景推荐] 真实远端 Provider；启用时增加独立 config.Provider 声明。
			// config.Provider(confignacos.New()),
			// config.Provider(configconsul.New()),
			config.Provider(&quickstartStaticRemoteProvider{
				name: "fixed-remote",
				client: &quickstartStaticRemoteClient{
					content: []byte("feature:\n  from_fixed_plan: true\nservice:\n  name: from-fixed-plan\n"),
				},
				source: quickstartStaticRemoteSource{name: "main", priority: 20},
			}),
		),
		app.WithConfigBinding("", validateFullConfig),
		app.WithModules(quickstartIOCModule()),

		// [可选][高级] IOC 策略通过显式逃生口声明。
		app.WithIOCOptions(
			ioc.WithCloseTimeout(3*time.Second),
		),

		// [可选][推荐] app.WithFlagBinder：root Flag 声明与字段绑定入口（建议统一集中在一个结构里）。
		app.WithFlagBinder(rootFlags),

		// [可选][推荐] app.WithBeforeRunHook：业务前置切面（审计、追踪、打印上下文）。
		app.WithBeforeRunHook(func(_ context.Context, _ app.Runtime, command app.CommandContext) error {
			_, _ = fmt.Fprintf(os.Stdout, "[hook before] command=%s path=%s\n", command.Name(), command.Path())
			return nil
		}),
		// [可选][推荐] app.WithCommands：扩展 explain/doctor 两类典型子命令。
		app.WithCommands(
			app.Command{
				Use:        "explain",
				Short:      "Explain one key metadata from latest snapshot",
				FlagBinder: explainFlags,
				Runner: func(_ context.Context, runtime app.Runtime, _ app.CommandContext) error {
					meta, ok := runtime.ConfigManager().Current().Explain(explainFlags.Key)
					if !ok {
						_, _ = fmt.Fprintf(os.Stdout, "key=%s not found\n", explainFlags.Key)
						return nil
					}
					_, _ = fmt.Fprintf(os.Stdout, "key=%s source=%s layer=%s priority=%d pinned=%v version=%d\n",
						explainFlags.Key,
						meta.Source,
						meta.Layer,
						meta.Priority,
						meta.Pinned,
						meta.Version,
					)
					return nil
				},
			},
			app.Command{
				Use:         "doctor",
				Short:       "Run diagnostics without loading runtime config",
				RuntimeMode: app.RuntimeModeDisabled,
				Runner: func(_ context.Context, _ app.Runtime, _ app.CommandContext) error {
					_, _ = fmt.Fprintln(os.Stdout, "doctor: ok (RuntimeModeDisabled)")
					return nil
				},
			},
		),

		// [必须][推荐] app.WithRunner：root 极简业务入口。
		app.WithRunner(func(ctx context.Context, runtime app.Runtime) error {
			iocContainer := runtime.Container()
			worker, err := ioc.ResolveByNameAs[*quickstartFullIOCWorker](iocContainer, "QuickstartIOCWorker")
			if err != nil {
				return err
			}

			cfg := worker.Config.Load()

			latest := runtime.ConfigManager().Current()
			version := uint64(0)
			if latest != nil {
				version = latest.Version()
			}

			_, _ = fmt.Fprintf(os.Stdout, "service.name=%s service.address=%s timeout=%s worker.concurrency=%d worker.max_queue=%d version=%d\n",
				cfg.Service.Name,
				cfg.Service.Address,
				cfg.Service.Timeout,
				cfg.Worker.Concurrency,
				cfg.Worker.MaxQueue,
				version,
			)
			_, _ = fmt.Fprintf(os.Stdout, "flags profile=%s debug=%v sample.rate=%.2f graceful-timeout=%s tags=%v retry.budget=%d request.limit=%d\n",
				rootFlags.Profile,
				rootFlags.Debug,
				rootFlags.SampleRate,
				rootFlags.GracefulTimeout,
				rootFlags.Tags,
				rootFlags.RetryBudget,
				rootFlags.RequestLimit,
			)
			_, _ = fmt.Fprintf(os.Stdout, "feature.from_provider_remote=%v feature.from_fixed_plan=%v\n",
				cfg.Feature.FromProviderRemote,
				cfg.Feature.FromFixedPlan,
			)
			_, _ = fmt.Fprintf(os.Stdout, "ioc.worker=%s\n", worker.Status())
			return nil
		}),
	)
}

func defaultFullConfig() quickstartFullConfig {
	cfg := quickstartFullConfig{}
	cfg.Service.Name = "quickstart-full"
	cfg.Service.Address = ":18080"
	cfg.Service.Timeout = "1s"
	cfg.Worker.Concurrency = 4
	cfg.Worker.MaxQueue = 128
	return cfg
}

func validateFullConfig(cfg *quickstartFullConfig) []error {
	var errs []error
	if cfg == nil {
		return []error{errcode.New[quickstartFullErrorCode](errCodeQuickstartFullConfigBad, "config is nil")}
	}
	if strings.TrimSpace(cfg.Service.Name) == "" {
		errs = append(errs, errcode.New[quickstartFullErrorCode](errCodeQuickstartFullConfigBad, "service.name is required"))
	}
	if strings.TrimSpace(cfg.Service.Address) == "" {
		errs = append(errs, errcode.New[quickstartFullErrorCode](errCodeQuickstartFullConfigBad, "service.address is required"))
	}
	if cfg.Worker.Concurrency <= 0 {
		errs = append(errs, errcode.New[quickstartFullErrorCode](errCodeQuickstartFullConfigBad, "worker.concurrency must be greater than 0"))
	}
	return errs
}

func readBootstrapBool(values map[string]any, rootKey string, fieldKey string) (bool, bool) {
	root, exists := values[rootKey]
	if !exists {
		return false, false
	}
	block, ok := root.(map[string]any)
	if !ok {
		return false, false
	}
	raw, exists := block[fieldKey]
	if !exists {
		return false, false
	}
	result, ok := raw.(bool)
	return result, ok
}
