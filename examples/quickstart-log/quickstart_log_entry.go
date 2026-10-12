package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yangzhuangqiu/gobase/app"
	applogzap "github.com/yangzhuangqiu/gobase/app-log-zap"
	"github.com/yangzhuangqiu/gobase/config"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

type quickstartLogMinimalService struct{}

// quickstartLogMinimalConfig 是 quickstart-log 的轻量配置结构。
type quickstartLogMinimalConfig struct {
	Service struct {
		Name    string `yaml:"name"`
		Address string `yaml:"address"`
	} `yaml:"service"`
	Log struct {
		Level  string `yaml:"level"`
		Mode   string `yaml:"mode"`
		Format string `yaml:"format"`
	} `yaml:"log"`
}

// runQuickstartLog 运行轻量入口，演示 app 自动装配日志后业务层直接写日志。
func runQuickstartLog() error {
	return app.Run(
		app.WithName("quickstart-log"),
		app.WithDescription("Minimal quickstart for gobase/log"),
		app.WithVersion("v0.1.0"),
		app.WithLogging(applogzap.New(applogzap.WithScope("log"))),
		app.WithConfig(config.Defaults(defaultQuickstartLogMinimalConfig())),
		app.WithRunner(runQuickstartLogMinimal),
	)
}

// runQuickstartLogMinimal 演示显式名称、Context-first 与结构化字段。
func runQuickstartLogMinimal(ctx context.Context, runtime app.Runtime) error {
	cfg := &quickstartLogMinimalConfig{}
	if err := runtime.ConfigManager().Current().Decode("", cfg); err != nil {
		return err
	}

	logger := runtime.Logger("quickstart.log.minimal")
	logger.Debug(ctx, "minimal debug event",
		baselog.Op("quickstart.log.minimal.debug"), baselog.Result(baselog.ResultOK),
	)
	logger.Info(
		ctx,
		"minimal quickstart log emitted",
		baselog.Op("quickstart.log.minimal.run"),
		baselog.Result(baselog.ResultOK),
		baselog.F("request_id", "qs-minimal-rid-001"),
		baselog.F("user_id", "qs-minimal-uid-001"),
		baselog.F("service", cfg.Service.Name),
		baselog.F("address", cfg.Service.Address),
	)

	version := uint64(0)
	if latest := runtime.ConfigManager().Current(); latest != nil {
		version = latest.Version()
	}
	_, _ = fmt.Fprintf(os.Stdout, "minimal done service=%s address=%s log.level=%s version=%d\n",
		cfg.Service.Name,
		cfg.Service.Address,
		cfg.Log.Level,
		version,
	)
	return nil
}

func defaultQuickstartLogMinimalConfig() quickstartLogMinimalConfig {
	cfg := quickstartLogMinimalConfig{}
	cfg.Service.Name = "quickstart-log"
	cfg.Service.Address = ":8080"
	cfg.Log.Level = "info"
	cfg.Log.Mode = "console"
	cfg.Log.Format = "text"
	return cfg
}
