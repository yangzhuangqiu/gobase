package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yangzhuangqiu/gobase/app"
	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/errcode"
)

type ErrorCode = errcode.StringCode

const (
	ErrCodeExampleInvalidArgument ErrorCode = "EXAMPLE_ADMIN_INVALID_ARGUMENT"
	ErrCodeExampleRuntimeNotReady ErrorCode = "EXAMPLE_ADMIN_RUNTIME_NOT_READY"
	ErrCodeExampleConfigInvalid   ErrorCode = "EXAMPLE_ADMIN_CONFIG_INVALID"
)

// AppConfig 是示例应用的运行时配置结构。
type AppConfig struct {
	Service ServiceConfig `yaml:"service"`
	Worker  WorkerConfig  `yaml:"worker"`
}

// ServiceConfig 存放服务级配置项。
type ServiceConfig struct {
	Name    string `yaml:"name"`
	Address string `yaml:"address"`
}

// WorkerConfig 存放后台 worker 相关配置项。
type WorkerConfig struct {
	Concurrency int `yaml:"concurrency"`
}

// RootFlags 定义 root 命令自定义 Flag。
type RootFlags struct {
	// Debug 控制当前进程是否输出调试信息，默认关闭。
	Debug bool
}

// Bind 注册 root 命令自定义参数。
func (this *RootFlags) Bind(flags app.CommandFlags) {
	flags.Bool("debug", &this.Debug, false, "Enable debug mode.")
}

// ValidateFlags 校验 root 命令自定义 flags。
func (this *RootFlags) ValidateFlags() []error {
	return nil
}

// Complete 负责补全 root 选项中的派生值。
func (this *RootFlags) Complete() error {
	return nil
}

// ExplainFlags 定义 `explain` 子命令参数。
type ExplainFlags struct {
	// Key 是需要查询来源信息的 dotted 配置键，默认 service.address。
	Key string
}

// Bind 注册 explain 子命令参数。
func (this *ExplainFlags) Bind(flags app.CommandFlags) {
	flags.String("key", &this.Key, "service.address", "Config key to inspect, dotted format.")
}

// ValidateFlags 校验 explain 子命令参数。
func (this *ExplainFlags) ValidateFlags() []error {
	if strings.TrimSpace(this.Key) == "" {
		return []error{errcode.New[ErrorCode](ErrCodeExampleInvalidArgument, "--key is required")}
	}
	return nil
}

// main 组装应用：包含 hooks 与子命令。
func main() {
	rootFlags := &RootFlags{}
	explainFlags := &ExplainFlags{}

	app.Main(
		app.WithName("admin"),
		app.WithDescription("Mail admin example powered by App"),
		app.WithVersion("v0.2.0"),
		app.WithConfig(
			config.Defaults(defaultConfig()),
			config.Validate(validateConfig),
		),
		app.WithFlagBinder(rootFlags),
		app.WithBeforeRunHook(func(_ context.Context, _ app.Runtime, cmd app.CommandContext) error {
			_, _ = fmt.Fprintf(os.Stdout, "hook(before) command=%s\n", cmd.Name())
			return nil
		}),
		app.WithCommands(
			app.Command{
				Use:        "explain",
				Short:      "Explain config source for one key",
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
				Short:       "Run quick diagnostics without loading runtime config",
				RuntimeMode: app.RuntimeModeDisabled,
				Runner: func(_ context.Context, _ app.Runtime, _ app.CommandContext) error {
					_, _ = fmt.Fprintln(os.Stdout, "doctor: ok")
					return nil
				},
			},
		),
		app.WithRunner(func(_ context.Context, runtime app.Runtime) error {
			cfg := &AppConfig{}
			if err := runtime.ConfigManager().Current().Decode("", cfg); err != nil {
				return errcode.New[ErrorCode](ErrCodeExampleRuntimeNotReady, "runtime config is not ready")
			}
			latest := runtime.ConfigManager().Current()
			version := uint64(0)
			if latest != nil {
				version = latest.Version()
			}
			_, _ = fmt.Fprintf(os.Stdout, "start service=%s addr=%s concurrency=%d debug=%v version=%d\n",
				cfg.Service.Name,
				cfg.Service.Address,
				cfg.Worker.Concurrency,
				rootFlags.Debug,
				version,
			)
			return nil
		}),
	)
}

// defaultConfig 返回本地运行示例默认配置。
func defaultConfig() AppConfig {
	return AppConfig{
		Service: ServiceConfig{
			Name:    "admin",
			Address: ":8080",
		},
		Worker: WorkerConfig{
			Concurrency: 8,
		},
	}
}

// validateConfig 在发布配置前执行语义校验。
func validateConfig(typedCfg *AppConfig) []error {
	if typedCfg == nil {
		return []error{errcode.New[ErrorCode](ErrCodeExampleConfigInvalid, "config is nil")}
	}
	var errs []error
	if strings.TrimSpace(typedCfg.Service.Name) == "" {
		errs = append(errs, errcode.New[ErrorCode](ErrCodeExampleConfigInvalid, "service.name is required"))
	}
	if strings.TrimSpace(typedCfg.Service.Address) == "" {
		errs = append(errs, errcode.New[ErrorCode](ErrCodeExampleConfigInvalid, "service.address is required"))
	}
	if typedCfg.Worker.Concurrency <= 0 {
		errs = append(errs, errcode.New[ErrorCode](ErrCodeExampleConfigInvalid, "worker.concurrency must be greater than 0"))
	}
	return errs
}
