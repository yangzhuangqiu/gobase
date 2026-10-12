package app

import (
	"context"
	"io"

	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/config/provider"
)

// commandRuntimeRemoteClient 提供可观察的测试配置 Provider。
type commandRuntimeRemoteClient struct {
	// Trace 记录 config 远端资源与命令生命周期顺序。
	Trace *[]string
}

// Name 返回测试 Provider 的稳定名称。
func (this *commandRuntimeRemoteClient) Name() string { return "trace" }

// Open 创建可观察命令生命周期的测试 Session。
func (this *commandRuntimeRemoteClient) Open(context.Context, map[string]any) (provider.Session, error) {
	return &commandRuntimeSession{client: this}, nil
}

// commandRuntimeSource 是命令生命周期测试使用的单个 Source。
type commandRuntimeSource struct{}

// Info 返回测试 Source 的稳定元信息。
func (this commandRuntimeSource) Info() provider.SourceInfo { return provider.SourceInfo{Name: "main"} }

// commandRuntimeSession 把测试远端 Client 适配为 Session。
type commandRuntimeSession struct {
	// client 提供 Source 加载与 watcher 生命周期观测能力。
	client *commandRuntimeRemoteClient
}

// Sources 返回命令生命周期测试使用的 Source。
func (this *commandRuntimeSession) Sources() []provider.Source {
	return []provider.Source{commandRuntimeSource{}}
}

// Load 记录并加载测试远端配置。
func (this *commandRuntimeSession) Load(ctx context.Context, _ provider.Source) ([]byte, error) {
	return this.client.GetConfig(ctx, "g", "d")
}

// Watch 建立测试 watcher，并把内容回调转换为失效通知。
func (this *commandRuntimeSession) Watch(ctx context.Context, _ provider.Source, onChange func()) (io.Closer, error) {
	return this.client.WatchConfig(ctx, "g", "d", func(_ []byte) { onChange() })
}

// Close 结束测试 Session；底层 Client 没有独立资源。
func (this *commandRuntimeSession) Close() error { return nil }

// GetConfig 记录 Runtime 已在 hooks 和业务前完成 config Build。
func (this *commandRuntimeRemoteClient) GetConfig(context.Context, string, string) ([]byte, error) {
	*this.Trace = append(*this.Trace, "init")
	return []byte("{}"), nil
}

// WatchConfig 返回由 ConfigManager 持有的关闭句柄。
func (this *commandRuntimeRemoteClient) WatchConfig(context.Context, string, string, func([]byte)) (io.Closer, error) {
	return &commandRuntimeWatchCloser{Trace: this.Trace}, nil
}

// commandRuntimeWatchCloser 记录测试 Runtime 资源关闭顺序。
type commandRuntimeWatchCloser struct {
	// Trace 记录 ConfigManager 在 Shutdown Hooks 后关闭 watcher。
	Trace *[]string
}

// Close 记录 Runtime 在 Shutdown Hooks 后执行 config 资源关闭。
func (this *commandRuntimeWatchCloser) Close() error {
	*this.Trace = append(*this.Trace, "close")
	return nil
}

// commandRuntimeTraceOptions 返回可观察 Runtime config 生命周期的蓝图选项。
func commandRuntimeTraceOptions(trace *[]string) []appruntime.Option {
	return []appruntime.Option{
		appruntime.WithConfig(config.File(config.FileDisabled()), config.Provider(&commandRuntimeRemoteClient{Trace: trace})),
	}
}

// commandRuntimeFlags 声明 Runtime 输入流测试使用的配置 Flag。
type commandRuntimeFlags struct{}

// Bind 声明一个进入 app-runtime pinned 输入的配置 flag。
func (this *commandRuntimeFlags) Bind(flags CommandFlags) {
	flags.ConfigString("service.name", "service name")
}

// commandRuntimeConfig 是 CLI 到 Runtime 输入流测试使用的配置视图。
type commandRuntimeConfig struct {
	// Service 是测试配置业务域。
	Service struct {
		// Name 验证 CLI pinned 覆盖结果。
		Name string `yaml:"name"`
	} `yaml:"service"`
}
