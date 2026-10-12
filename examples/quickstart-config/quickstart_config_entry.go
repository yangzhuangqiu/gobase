package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yangzhuangqiu/gobase/config"
)

// quickstartConfig 是最小示例配置结构。
type quickstartConfig struct {
	Service struct {
		Name    string `yaml:"name"`
		Address string `yaml:"address"`
	} `yaml:"service"`
	Worker struct {
		Concurrency int `yaml:"concurrency"`
	} `yaml:"worker"`
}

// runQuickstartConfig 演示 config 模块最小闭环：Build -> Snapshot.Decode -> Snapshot.Explain。
func runQuickstartConfig() error {
	defaultCfg := quickstartConfig{}
	defaultCfg.Service.Name = "quickstart-config"
	defaultCfg.Service.Address = ":8080"
	defaultCfg.Worker.Concurrency = 4

	manager, err := config.New(
		// [可选][推荐] Defaults：提供兜底配置，适合本地开发与单测场景。
		config.Defaults(defaultCfg),
	).Build(context.Background())
	if err != nil {
		return err
	}
	// [可选][推荐] Close：释放 watcher 与内部资源；有远端监听时尤其建议调用。
	defer func() { _ = manager.Close() }()

	cfg := &quickstartConfig{}
	snapshot := manager.Current()
	// [必须][推荐] Snapshot.Decode：从同一个不可变版本解码业务结构；空 scope 表示整份配置。
	if err = snapshot.Decode("", cfg); err != nil {
		return err
	}
	// [可选][推荐] Snapshot.Explain：查看本次解码版本中某个 key 的最终来源层级。
	meta, exists := snapshot.Explain("service.address")

	_, _ = fmt.Fprintf(os.Stdout, "service.name=%s service.address=%s worker.concurrency=%d\n",
		cfg.Service.Name,
		cfg.Service.Address,
		cfg.Worker.Concurrency,
	)
	if exists {
		_, _ = fmt.Fprintf(os.Stdout, "explain service.address source=%s layer=%s priority=%d pinned=%v version=%d\n",
			meta.Source,
			meta.Layer,
			meta.Priority,
			meta.Pinned,
			meta.Version,
		)
	}
	return nil
}
