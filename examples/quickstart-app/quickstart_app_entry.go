package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yangzhuangqiu/gobase/app"
	"github.com/yangzhuangqiu/gobase/config"
)

// quickstartMinimalConfig 是 quickstart-app 的轻量配置结构。
type quickstartMinimalConfig struct {
	Service struct {
		Name    string `yaml:"name"`
		Address string `yaml:"address"`
	} `yaml:"service"`
	Worker struct {
		Concurrency int `yaml:"concurrency"`
	} `yaml:"worker"`
}

// runQuickstartApp 运行轻量入口，适合 5 分钟快速上手。
func runQuickstartApp() error {
	return app.Run(
		// [必须][推荐] 设置命令名。
		app.WithName("quickstart"),
		// [可选][推荐] 设置帮助描述。
		app.WithDescription("Minimal quickstart for gobase/app"),
		// [可选][推荐] 设置版本文本。
		app.WithVersion("v0.1.0"),
		// [可选][推荐] app 门面提供默认配置兜底。
		app.WithConfig(config.Defaults(defaultMinimalConfig())),
		// [必须][推荐] 提供 root 业务入口。
		app.WithRunner(runMinimal),
	)
}

// runMinimal 演示如何读取最终配置与来源解释。
func runMinimal(_ context.Context, runtime app.Runtime) error {
	cfg := &quickstartMinimalConfig{}
	if err := runtime.ConfigManager().Current().Decode("", cfg); err != nil {
		return err
	}

	version := uint64(0)
	if latest := runtime.ConfigManager().Current(); latest != nil {
		version = latest.Version()
	}
	meta, exists := runtime.ConfigManager().Current().Explain("service.address")

	_, _ = fmt.Fprintf(os.Stdout, "service.name=%s service.address=%s worker.concurrency=%d version=%d\n",
		cfg.Service.Name,
		cfg.Service.Address,
		cfg.Worker.Concurrency,
		version,
	)
	if exists {
		_, _ = fmt.Fprintf(os.Stdout, "explain service.address source=%s layer=%s priority=%d pinned=%v\n",
			meta.Source,
			meta.Layer,
			meta.Priority,
			meta.Pinned,
		)
	}
	return nil
}

func defaultMinimalConfig() quickstartMinimalConfig {
	cfg := quickstartMinimalConfig{}
	cfg.Service.Name = "quickstart"
	cfg.Service.Address = ":8080"
	cfg.Worker.Concurrency = 4
	return cfg
}
