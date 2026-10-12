package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	baselog "github.com/yangzhuangqiu/gobase/log"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
	"gopkg.in/yaml.v3"
)

type quickstartLogPlainFullService struct{}

type quickstartLogPlainFullConfig struct {
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
			Filename   string `yaml:"filename"`
			MaxSize    int    `yaml:"maxSize"`
			MaxBackups int    `yaml:"maxBackups"`
			MaxAge     int    `yaml:"maxAge"`
			Compress   bool   `yaml:"compress"`
			LocalTime  bool   `yaml:"localTime"`
		} `yaml:"rotate"`
	} `yaml:"log"`
}

type quickstartLogPlainFullOptions struct {
	ConfigPath   string
	RequestID    string
	UserID       string
	EmitDebug    bool
	DynamicLevel string
}

// runQuickstartLogPlainFull 运行不依赖 app 的全量入口示例。
func runQuickstartLogPlainFull() error {
	options, err := parseQuickstartLogPlainFullOptions(os.Args[1:])
	if err != nil {
		return err
	}

	config, err := loadQuickstartLogPlainFullConfig(options.ConfigPath)
	if err != nil {
		return err
	}
	if err = validateQuickstartLogPlainFullConfig(&config); err != nil {
		return err
	}

	zapConfig, err := buildQuickstartLogPlainZapConfig(config)
	if err != nil {
		return err
	}
	provider, err := logzap.NewProvider(zapConfig)
	if err != nil {
		return err
	}
	defer provider.Close()

	logger := provider.Factory().Get("quickstart.log.plain.full")
	if options.EmitDebug {
		logger.Debug(context.Background(), "plain full debug event",
			baselog.Op("quickstart.log.plain.full.debug"), baselog.Result(baselog.ResultOK),
		)
	}

	if strings.TrimSpace(options.DynamicLevel) != "" {
		if err = provider.ApplyRuntimeConfig(logzap.RuntimeConfig{Level: options.DynamicLevel}); err != nil {
			return err
		}
		logger.Debug(
			context.Background(), "plain full dynamic level applied",
			baselog.Op("quickstart.log.plain.full.dynamic"),
			baselog.Result(baselog.ResultOK), baselog.F("configured_level", options.DynamicLevel),
		)
	}

	logger.Info(
		context.Background(), "plain full quickstart log emitted",
		baselog.Op("quickstart.log.plain.full.run"), baselog.Result(baselog.ResultOK),
		baselog.F("request_id", options.RequestID), baselog.F("user_id", options.UserID),
		baselog.F("service", config.Service.Name), baselog.F("address", config.Service.Address),
		baselog.F("log.level", config.Log.Level), baselog.F("log.format", config.Log.Format),
	)

	_, _ = fmt.Fprintf(os.Stdout, "plain full done service=%s address=%s config=%s\n",
		config.Service.Name,
		config.Service.Address,
		options.ConfigPath,
	)
	return nil
}

func parseQuickstartLogPlainFullOptions(args []string) (quickstartLogPlainFullOptions, error) {
	options := quickstartLogPlainFullOptions{
		ConfigPath: "./quickstart-log/application.yaml",
		RequestID:  "qs-plain-full-rid-001",
		UserID:     "qs-plain-full-uid-001",
	}
	flagSet := flag.NewFlagSet("quickstart-log-plain-full", flag.ContinueOnError)
	flagSet.SetOutput(io.Discard)
	flagSet.StringVar(&options.ConfigPath, "config", options.ConfigPath, "Path to YAML config file.")
	flagSet.StringVar(&options.RequestID, "request-id", options.RequestID, "Request id for context logging.")
	flagSet.StringVar(&options.UserID, "user-id", options.UserID, "User id for context logging.")
	flagSet.BoolVar(&options.EmitDebug, "emit-debug", false, "Emit debug log before business log.")
	flagSet.StringVar(&options.DynamicLevel, "dynamic-level", "", "Apply dynamic level before business log.")
	if err := flagSet.Parse(args); err != nil {
		return quickstartLogPlainFullOptions{}, err
	}
	return options, nil
}

func loadQuickstartLogPlainFullConfig(path string) (quickstartLogPlainFullConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return quickstartLogPlainFullConfig{}, err
	}
	config := quickstartLogPlainFullConfig{}
	if err = yaml.Unmarshal(content, &config); err != nil {
		return quickstartLogPlainFullConfig{}, err
	}
	return config, nil
}

func validateQuickstartLogPlainFullConfig(config *quickstartLogPlainFullConfig) error {
	if config == nil {
		return errors.New("config is nil")
	}
	if strings.TrimSpace(config.Service.Name) == "" {
		return errors.New("service.name is required")
	}
	if strings.TrimSpace(config.Service.Address) == "" {
		return errors.New("service.address is required")
	}
	if !isSupportedQuickstartLogPlainFullLevel(config.Log.Level) {
		return errors.New("log.level must be debug|info|warn|error|fatal")
	}
	switch strings.TrimSpace(strings.ToLower(config.Log.Mode)) {
	case "", "console", "file":
	default:
		return errors.New("log.mode must be console|file")
	}
	switch strings.TrimSpace(strings.ToLower(config.Log.Format)) {
	case "", "json", "text":
	default:
		return errors.New("log.format must be json|text")
	}
	if strings.EqualFold(config.Log.Mode, "file") && strings.TrimSpace(config.Log.Rotate.Filename) == "" {
		return errors.New("log.rotate.filename is required when log.mode=file")
	}
	return nil
}

// isSupportedQuickstartLogPlainFullLevel 校验示例支持的日志级别输入。
func isSupportedQuickstartLogPlainFullLevel(raw string) bool {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", "trace", "debug", "notice", "info", "warn", "warning", "error":
		return true
	default:
		return false
	}
}

func buildQuickstartLogPlainZapConfig(config quickstartLogPlainFullConfig) (logzap.Config, error) {
	return logzap.Config{
		Level:         config.Log.Level,
		Mode:          config.Log.Mode,
		Format:        config.Log.Format,
		DisableCaller: config.Log.DisableCaller,
		Rotate: logzap.RotateConfig{
			Filename:   config.Log.Rotate.Filename,
			MaxSize:    config.Log.Rotate.MaxSize,
			MaxBackups: config.Log.Rotate.MaxBackups,
			MaxAge:     config.Log.Rotate.MaxAge,
			Compress:   config.Log.Rotate.Compress,
			LocalTime:  config.Log.Rotate.LocalTime,
		},
	}, nil
}
