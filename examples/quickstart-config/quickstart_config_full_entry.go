package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/config/provider"
)

// quickstartConfigFull 是全量示例配置结构。
type quickstartConfigFull struct {
	Service struct {
		Name    string `yaml:"name"`
		Address string `yaml:"address"`
	} `yaml:"service"`
	Worker struct {
		Concurrency int `yaml:"concurrency"`
	} `yaml:"worker"`
	Feature struct {
		EnableCache bool `yaml:"enable_cache"`
	} `yaml:"feature"`
}

type quickstartWorkerScope struct {
	Concurrency int `yaml:"concurrency"`
}

type quickstartServiceScope struct {
	Name    string `yaml:"name"`
	Address string `yaml:"address"`
}

type watchRegistration struct {
	id       uint64
	onChange func(content []byte)
}

type quickstartWatchCloser struct {
	mu      sync.Mutex
	closed  bool
	closeFn func()
}

func (this *quickstartWatchCloser) Close() error {
	this.mu.Lock()
	defer this.mu.Unlock()
	if this.closed {
		return nil
	}
	this.closed = true
	if this.closeFn != nil {
		this.closeFn()
	}
	return nil
}

// quickstartRemoteClient 是全量示例中的远端 client 测试替身。
//
// 场景：
// 1. 教学演示：不依赖真实配置中心，也能观察 refresh 行为；
// 2. 单元测试：可控触发变更，验证 Merge 与监听链路。
type quickstartRemoteClient struct {
	mu            sync.Mutex
	nextWatchID   uint64
	contents      map[string][]byte
	registrations map[string][]watchRegistration
}

func newQuickstartRemoteClient(initial map[string]string) *quickstartRemoteClient {
	contents := make(map[string][]byte, len(initial))
	for key, value := range initial {
		contents[key] = []byte(value)
	}
	return &quickstartRemoteClient{
		contents:      contents,
		registrations: map[string][]watchRegistration{},
	}
}

func remoteSourceKey(groupID string, dataID string) string {
	return strings.TrimSpace(groupID) + "/" + strings.TrimSpace(dataID)
}

func (this *quickstartRemoteClient) GetConfig(ctx context.Context, groupID, dataID string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := remoteSourceKey(groupID, dataID)
	this.mu.Lock()
	defer this.mu.Unlock()
	content, exists := this.contents[key]
	if !exists {
		return nil, fmt.Errorf("remote source not found key=%s", key)
	}
	result := make([]byte, len(content))
	copy(result, content)
	return result, nil
}

func (this *quickstartRemoteClient) WatchConfig(_ context.Context, groupID, dataID string, onChange func(content []byte)) (io.Closer, error) {
	key := remoteSourceKey(groupID, dataID)
	this.mu.Lock()
	this.nextWatchID++
	currentID := this.nextWatchID
	this.registrations[key] = append(this.registrations[key], watchRegistration{
		id:       currentID,
		onChange: onChange,
	})
	this.mu.Unlock()

	return &quickstartWatchCloser{
		closeFn: func() {
			this.mu.Lock()
			defer this.mu.Unlock()
			current := this.registrations[key]
			next := make([]watchRegistration, 0, len(current))
			for _, item := range current {
				if item.id == currentID {
					continue
				}
				next = append(next, item)
			}
			this.registrations[key] = next
		},
	}, nil
}

// Trigger 模拟远端配置变更并触发 watcher 回调。
func (this *quickstartRemoteClient) Trigger(groupID string, dataID string, content string) {
	key := remoteSourceKey(groupID, dataID)
	payload := []byte(content)

	this.mu.Lock()
	this.contents[key] = payload
	current := append([]watchRegistration{}, this.registrations[key]...)
	this.mu.Unlock()

	for _, item := range current {
		if item.onChange == nil {
			continue
		}
		item.onChange(payload)
	}
}

// quickstartRemoteSource 保存 mock provider 私有的 group/data 定位信息。
type quickstartRemoteSource struct {
	groupID string
	dataID  string
}

// Info 返回 Config 内核需要的中立 Source 元信息。
func (this quickstartRemoteSource) Info() provider.SourceInfo {
	return provider.SourceInfo{Name: "main", Priority: 20}
}

// quickstartRemoteProvider 演示已有远端 Client 如何接入 Provider 契约。
type quickstartRemoteProvider struct {
	client *quickstartRemoteClient
	source quickstartRemoteSource
}

// Name 返回 mock provider 的稳定名称。
func (this *quickstartRemoteProvider) Name() string { return "mock-remote" }

// Open 为本次 Manager 构建创建独立 Session。
func (this *quickstartRemoteProvider) Open(context.Context, map[string]any) (provider.Session, error) {
	return &quickstartRemoteSession{client: this.client, source: this.source}, nil
}

// quickstartRemoteSession 聚合远端连接、Source 和会话生命周期。
type quickstartRemoteSession struct {
	client *quickstartRemoteClient
	source quickstartRemoteSource
}

// Sources 返回 mock provider 的单个 Source。
func (this *quickstartRemoteSession) Sources() []provider.Source {
	return []provider.Source{this.source}
}

// Load 拉取 mock Source 的完整内容。
func (this *quickstartRemoteSession) Load(ctx context.Context, source provider.Source) ([]byte, error) {
	item := source.(quickstartRemoteSource)
	return this.client.GetConfig(ctx, item.groupID, item.dataID)
}

// Watch 监听 mock Source，并把内容回调转换为失效通知。
func (this *quickstartRemoteSession) Watch(ctx context.Context, source provider.Source, onChange func()) (io.Closer, error) {
	item := source.(quickstartRemoteSource)
	return this.client.WatchConfig(ctx, item.groupID, item.dataID, func(_ []byte) { onChange() })
}

// Close 结束 mock Session；示例 Client 由调用方持有且无独立连接资源。
func (this *quickstartRemoteSession) Close() error { return nil }

// runQuickstartConfigFull 演示 config 全量能力：file+remote+pinned、Observe、Subscribe、refresh。
func runQuickstartConfigFull() error {
	const (
		remoteGroupID = "demo-group"
		remoteDataID  = "quickstart-config.yaml"
	)
	// [可选][推荐] 远端替身 client：教学场景下用本地模拟替代真实配置中心。
	client := newQuickstartRemoteClient(map[string]string{
		remoteSourceKey(remoteGroupID, remoteDataID): "service:\n  name: from-remote\nworker:\n  concurrency: 16\nfeature:\n  enable_cache: true\n",
	})

	defaultCfg := quickstartConfigFull{}
	defaultCfg.Service.Name = "from-default"
	defaultCfg.Service.Address = ":8080"
	defaultCfg.Worker.Concurrency = 2
	defaultCfg.Feature.EnableCache = false

	manager, err := config.New(
		// [可选][推荐] Defaults：默认层，适合提供“启动兜底”。
		config.Defaults(defaultCfg),
		// [可选][按场景推荐] File：本地文件层；适合本地开发、灰度手工覆盖。
		config.File(config.FilePath("quickstart-config/application.yaml")),
		// [可选][按场景推荐] PinnedValues：CLI/程序内强覆盖，优先级最高。
		// 场景：紧急覆盖、临时排障、发布窗口快速开关。
		config.PinnedValues(map[string]any{
			"service.address": ":28080",
		}),
		// [可选][高级] 已有 Client 通过 Provider+Session 封装，厂商定位信息不会泄漏给 config 内核。
		config.Provider(&quickstartRemoteProvider{
			client: client,
			source: quickstartRemoteSource{groupID: remoteGroupID, dataID: remoteDataID},
		}),
		// [可选][推荐] SnapshotValidators：发布快照前语义校验，拒绝非法配置进入运行态。
		config.SnapshotValidators(func(snapshot config.Snapshot) error {
			if snapshot.Int("worker.concurrency", 0) <= 0 {
				return fmt.Errorf("worker.concurrency must be greater than 0")
			}
			return nil
		}),
		// [可选][推荐] OnRefreshError：教学与排障场景下建议开启，便于观察刷新失败原因。
		config.OnRefreshError(func(err error) {
			_, _ = fmt.Fprintf(os.Stdout, "[refresh-error] %v\n", err)
		}),
	).Build(context.Background())
	if err != nil {
		return err
	}
	// [可选][推荐] Close：释放 watcher 资源；示例结束时统一收口。
	defer func() { _ = manager.Close() }()

	// [可选][推荐] Observe：业务在需要时 pull 指定 scope 的最新 typed 状态。
	workerObservation, err := config.Observe[quickstartWorkerScope](manager, "worker")
	if err != nil {
		return err
	}
	serviceObservation, err := config.Observe[quickstartServiceScope](manager, "service")
	if err != nil {
		return err
	}

	// [可选][按场景推荐] Subscribe：配置发布后需要执行副作用时使用。
	serviceSubscription, err := config.Subscribe[quickstartServiceScope](manager, "service", func(update config.TypedUpdate[quickstartServiceScope]) error {
		_, _ = fmt.Fprintf(os.Stdout, "[subscriber] initial=%t service.name=%s service.address=%s\n",
			update.Initial, update.Value.Name, update.Value.Address)
		return nil
	})
	if err != nil {
		return err
	}
	defer serviceSubscription.Cancel()

	if err = printCurrentConfig(manager, "initial"); err != nil {
		return err
	}
	workerValue, err := workerObservation.Current()
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "observe worker.concurrency=%d\n", workerValue.Concurrency)

	// 教学观察点：触发远端更新后，worker.concurrency 会随远端变化；
	// 但 service.address 被 pinned 覆盖，应保持 :28080 不变。
	client.Trigger(remoteGroupID, remoteDataID, "service:\n  name: from-remote-v2\nworker:\n  concurrency: 64\nfeature:\n  enable_cache: true\n")
	time.Sleep(150 * time.Millisecond)

	if err = printCurrentConfig(manager, "after-refresh"); err != nil {
		return err
	}
	workerValue, err = workerObservation.Current()
	if err != nil {
		return err
	}
	serviceValue, err := serviceObservation.Current()
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "observe worker.concurrency=%d\n", workerValue.Concurrency)
	_, _ = fmt.Fprintf(os.Stdout, "observe service.name=%s service.address=%s\n", serviceValue.Name, serviceValue.Address)

	// 教学观察点（失败路径）：
	// 推送一份会被 SnapshotValidator 拒绝的配置（worker.concurrency=0），
	// 预期行为：打印 refresh-error，且当前快照保持上一版不变。
	client.Trigger(remoteGroupID, remoteDataID, "service:\n  name: from-remote-invalid\nworker:\n  concurrency: 0\nfeature:\n  enable_cache: true\n")
	time.Sleep(150 * time.Millisecond)

	if err = printCurrentConfig(manager, "after-invalid-refresh"); err != nil {
		return err
	}
	workerValue, err = workerObservation.Current()
	if err != nil {
		return err
	}
	serviceValue, err = serviceObservation.Current()
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(os.Stdout, "observe worker.concurrency=%d\n", workerValue.Concurrency)
	_, _ = fmt.Fprintf(os.Stdout, "observe service.name=%s service.address=%s\n", serviceValue.Name, serviceValue.Address)
	return nil
}

func printCurrentConfig(manager config.Manager, stage string) error {
	cfg := &quickstartConfigFull{}
	snapshot := manager.Current()
	if err := snapshot.Decode("", cfg); err != nil {
		return err
	}
	meta, exists := snapshot.Explain("service.address")
	version := snapshot.Version()

	_, _ = fmt.Fprintf(os.Stdout, "[%s] service.name=%s service.address=%s worker.concurrency=%d feature.enable_cache=%v version=%d\n",
		stage,
		cfg.Service.Name,
		cfg.Service.Address,
		cfg.Worker.Concurrency,
		cfg.Feature.EnableCache,
		version,
	)
	if exists {
		_, _ = fmt.Fprintf(os.Stdout, "[%s] explain service.address source=%s layer=%s priority=%d pinned=%v version=%d\n",
			stage,
			meta.Source,
			meta.Layer,
			meta.Priority,
			meta.Pinned,
			meta.Version,
		)
	}
	return nil
}
