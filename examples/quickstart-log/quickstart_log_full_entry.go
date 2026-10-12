package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/yangzhuangqiu/gobase/app"
	applogzap "github.com/yangzhuangqiu/gobase/app-log-zap"
	"github.com/yangzhuangqiu/gobase/config"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

type quickstartLogFullErrorCode = string

const (
	errCodeQuickstartLogInvalidArg quickstartLogFullErrorCode = "EXAMPLE_QUICKSTART_LOG_INVALID_ARG"
	errCodeQuickstartLogConfigBad  quickstartLogFullErrorCode = "EXAMPLE_QUICKSTART_LOG_CONFIG_BAD"
)

type quickstartLogFullService struct{}

// quickstartLogFullConfig 是全量入口的运行时配置结构。
type quickstartLogFullConfig struct {
	Service struct {
		Name    string `yaml:"name"`
		Address string `yaml:"address"`
	} `yaml:"service"`
	Log struct {
		Level         string `yaml:"level"`
		Mode          string `yaml:"mode"`
		Format        string `yaml:"format"`
		DisableCaller bool   `yaml:"disableCaller"`
		Rotate        struct {
			Filename string `yaml:"filename"`
		} `yaml:"rotate"`
	} `yaml:"log"`
}

// quickstartLogRootFlags 演示配置参数与普通业务 Flag 的分离声明。
type quickstartLogRootFlags struct {
	// RequestID 是写入当前进程日志上下文的请求 ID。
	RequestID string
	// UserID 是写入当前进程日志上下文的用户 ID。
	UserID string
	// EmitDebug 控制是否额外输出 debug 日志，默认关闭。
	EmitDebug bool
}

// Bind 声明 root 的配置覆盖 Flag 和普通业务 Flag。
func (this *quickstartLogRootFlags) Bind(flags app.CommandFlags) {
	flags.ConfigString("service.name", "业务服务名，会进入 pinned 层覆盖。")
	flags.ConfigString("service.address", "业务监听地址，会进入 pinned 层覆盖。")
	flags.ConfigString("log.level", "日志级别，会进入 pinned 层覆盖。")
	flags.ConfigString("log.format", "日志格式，会进入 pinned 层覆盖。")

	flags.String("request-id", &this.RequestID, "", "运行时请求 ID（仅影响当前进程日志上下文）。")
	flags.String("user-id", &this.UserID, "", "运行时用户 ID（仅影响当前进程日志上下文）。")
	flags.Bool("emit-debug", &this.EmitDebug, false, "是否额外输出 debug 日志。")
}

// Complete 在校验前补全日志上下文标识。
func (this *quickstartLogRootFlags) Complete() error {
	if strings.TrimSpace(this.RequestID) == "" {
		this.RequestID = "qs-full-rid-001"
	}
	if strings.TrimSpace(this.UserID) == "" {
		this.UserID = "qs-full-uid-001"
	}
	return nil
}

// ValidateFlags 校验日志上下文所需的业务 Flag。
func (this *quickstartLogRootFlags) ValidateFlags() []error {
	var errs []error
	if strings.TrimSpace(this.RequestID) == "" {
		errs = append(errs, fmt.Errorf("%s: request-id is required", errCodeQuickstartLogInvalidArg))
	}
	if strings.TrimSpace(this.UserID) == "" {
		errs = append(errs, fmt.Errorf("%s: user-id is required", errCodeQuickstartLogInvalidArg))
	}
	return errs
}

type quickstartLogExplainFlags struct {
	// Key 是需要查询来源信息的 dotted 配置键，默认 log.level。
	Key string
}

// Bind 声明 explain 子命令的业务 Flag。
func (this *quickstartLogExplainFlags) Bind(flags app.CommandFlags) {
	flags.String("key", &this.Key, "log.level", "需要 explain 的配置键（dotted key）。")
}

// ValidateFlags 校验 explain 查询键非空。
func (this *quickstartLogExplainFlags) ValidateFlags() []error {
	if strings.TrimSpace(this.Key) == "" {
		return []error{fmt.Errorf("%s: --key is required", errCodeQuickstartLogInvalidArg)}
	}
	return nil
}

// runQuickstartLogFull 运行全量入口，演示 app + config + log 的配合实践。
func runQuickstartLogFull() error {
	rootFlags := &quickstartLogRootFlags{}
	explainFlags := &quickstartLogExplainFlags{}

	return app.Run(
		app.WithName("quickstart-log-full"),
		app.WithDescription("Comprehensive quickstart for gobase/log"),
		app.WithVersion("v0.1.0"),
		app.WithLogging(applogzap.New(applogzap.WithScope("log"))),
		app.WithConfig(
			config.Defaults(defaultQuickstartLogFullConfig()),
			config.Validate(validateQuickstartLogFullConfig),
		),
		app.WithFlagBinder(rootFlags),
		app.WithBeforeRunHook(func(ctx context.Context, runtime app.Runtime, command app.CommandContext) error {
			runtime.Logger("quickstart.log.hook").Info(
				ctx, "before hook",
				baselog.Op("quickstart.log.before_hook"), baselog.Result(baselog.ResultOK),
				baselog.F("command", command.Name()), baselog.F("path", command.Path()),
			)
			return nil
		}),
		app.WithCommands(
			app.Command{
				Use:        "explain",
				Short:      "Explain one config key metadata",
				FlagBinder: explainFlags,
				Runner: func(ctx context.Context, runtime app.Runtime, command app.CommandContext) error {
					meta, exists := runtime.ConfigManager().Current().Explain(explainFlags.Key)
					if !exists {
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
				Short:       "Run diagnostics without loading config",
				RuntimeMode: app.RuntimeModeDisabled,
				Runner: func(ctx context.Context, runtime app.Runtime, command app.CommandContext) error {
					_, _ = fmt.Fprintln(os.Stdout, "doctor: ok (RuntimeModeDisabled)")
					return nil
				},
			},
		),
		app.WithRunner(func(ctx context.Context, runtime app.Runtime) error {
			cfg := &quickstartLogFullConfig{}
			if err := runtime.ConfigManager().Current().Decode("", cfg); err != nil {
				return err
			}

			logger := runtime.Logger("quickstart.log.full")
			if rootFlags.EmitDebug {
				logger.Debug(ctx, "full debug event",
					baselog.Op("quickstart.log.full.debug"), baselog.Result(baselog.ResultOK),
				)
			}

			logger.Info(
				ctx,
				"full quickstart log emitted",
				baselog.Op("quickstart.log.full.run"), baselog.Result(baselog.ResultOK),
				baselog.F("request_id", rootFlags.RequestID), baselog.F("user_id", rootFlags.UserID),
				baselog.F("service", cfg.Service.Name), baselog.F("address", cfg.Service.Address),
				baselog.F("log.level", cfg.Log.Level), baselog.F("log.format", cfg.Log.Format),
			)

			meta, _ := runtime.ConfigManager().Current().Explain("log.level")
			_, _ = fmt.Fprintf(os.Stdout, "full done service=%s address=%s log.level=%s explain.source=%s explain.pinned=%v\n",
				cfg.Service.Name,
				cfg.Service.Address,
				cfg.Log.Level,
				meta.Source,
				meta.Pinned,
			)
			return nil
		}),
	)
}

func defaultQuickstartLogFullConfig() quickstartLogFullConfig {
	cfg := quickstartLogFullConfig{}
	cfg.Service.Name = "quickstart-log-full"
	cfg.Service.Address = ":18080"
	cfg.Log.Level = "info"
	cfg.Log.Mode = "console"
	cfg.Log.Format = "json"
	return cfg
}

func validateQuickstartLogFullConfig(cfg *quickstartLogFullConfig) []error {
	var errs []error
	if cfg == nil {
		return []error{fmt.Errorf("%s: config is nil", errCodeQuickstartLogConfigBad)}
	}
	if strings.TrimSpace(cfg.Service.Name) == "" {
		errs = append(errs, fmt.Errorf("%s: service.name is required", errCodeQuickstartLogConfigBad))
	}
	if strings.TrimSpace(cfg.Service.Address) == "" {
		errs = append(errs, fmt.Errorf("%s: service.address is required", errCodeQuickstartLogConfigBad))
	}
	if !isSupportedQuickstartLogFullLevel(cfg.Log.Level) {
		errs = append(errs, fmt.Errorf("%s: log.level must be trace|debug|info|warn|error", errCodeQuickstartLogConfigBad))
	}
	switch strings.TrimSpace(strings.ToLower(cfg.Log.Mode)) {
	case "", "console", "file":
	default:
		errs = append(errs, fmt.Errorf("%s: log.mode must be console|file", errCodeQuickstartLogConfigBad))
	}
	switch strings.TrimSpace(strings.ToLower(cfg.Log.Format)) {
	case "", "json", "text":
	default:
		errs = append(errs, fmt.Errorf("%s: log.format must be json|text", errCodeQuickstartLogConfigBad))
	}
	if strings.EqualFold(cfg.Log.Mode, "file") && strings.TrimSpace(cfg.Log.Rotate.Filename) == "" {
		errs = append(errs, fmt.Errorf("%s: log.rotate.filename is required when log.mode=file", errCodeQuickstartLogConfigBad))
	}
	return errs
}

// isSupportedQuickstartLogFullLevel 校验示例支持的日志级别输入。
func isSupportedQuickstartLogFullLevel(raw string) bool {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", "trace", "debug", "notice", "info", "warn", "warning", "error":
		return true
	default:
		return false
	}
}
