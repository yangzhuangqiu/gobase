package config_test

import (
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/config/provider"
)

// serviceConfig 演示 scope 解码到业务结构体。
type serviceConfig struct {
	Address string `yaml:"address"`
}

type workerConfig struct {
	Concurrency int `yaml:"concurrency"`
}

// exampleWatchCloser 用于模拟 watcher 句柄。
type exampleWatchCloser struct {
	once    sync.Once
	onClose func()
}

// Close 幂等关闭 watcher。
func (this *exampleWatchCloser) Close() error {
	this.once.Do(func() {
		if this.onClose != nil {
			this.onClose()
		}
	})
	return nil
}

// exampleRemoteClient 是调用方视角的远端 client 测试替身。
type exampleRemoteClient struct {
	mu       sync.Mutex
	content  string
	onChange func(content []byte)
	closed   bool
}

// exampleSource 是调用方自定义 Session 暴露给 Config 内核的最小 Source。
type exampleSource struct{}

// Info 返回示例 Source 的稳定元信息。
func (this exampleSource) Info() provider.SourceInfo {
	return provider.SourceInfo{Name: "main"}
}

// exampleProvider 演示调用方把既有客户端封装为 Provider。
type exampleProvider struct {
	name   string
	client *exampleRemoteClient
}

// Name 返回示例 Provider 的稳定名称。
func (this *exampleProvider) Name() string {
	return this.name
}

// Open 为本次 Config Manager 构建创建 Session。
func (this *exampleProvider) Open(context.Context, map[string]any) (provider.Session, error) {
	return &exampleSession{client: this.client}, nil
}

// exampleSession 把示例 Client 的厂商读写能力收敛到 Session。
type exampleSession struct {
	client *exampleRemoteClient
}

// Sources 返回示例远端源。
func (this *exampleSession) Sources() []provider.Source {
	return []provider.Source{exampleSource{}}
}

// Load 加载示例远端源。
func (this *exampleSession) Load(ctx context.Context, _ provider.Source) ([]byte, error) {
	return this.client.GetConfig(ctx, "g", "d")
}

// Watch 监听示例远端源，并把内容回调转换为失效通知。
func (this *exampleSession) Watch(ctx context.Context, _ provider.Source, onChange func()) (io.Closer, error) {
	return this.client.WatchConfig(ctx, "g", "d", func(_ []byte) { onChange() })
}

// Close 结束示例 Session；客户端资源由测试本身持有。
func (this *exampleSession) Close() error {
	return nil
}

// usecaseSubscriber 用于调用方视角验证 Subscribe(scope) 语义。
type usecaseSubscriber struct {
	scope string

	mu        sync.Mutex
	reloadCnt int
	lastAddr  string
}

// Scope 返回监听范围。
func (this *usecaseSubscriber) Scope() string {
	return this.scope
}

// OnUpdate 解码并记录回调次数与最后值。
func (this *usecaseSubscriber) OnUpdate(update config.Update) error {
	var typed serviceConfig
	if err := update.Snapshot.Decode(this.scope, &typed); err != nil {
		return err
	}
	this.mu.Lock()
	defer this.mu.Unlock()
	this.reloadCnt++
	this.lastAddr = typed.Address
	return nil
}

// snapshot 返回监听统计快照。
func (this *usecaseSubscriber) snapshot() (reloadCnt int, lastAddr string) {
	this.mu.Lock()
	defer this.mu.Unlock()
	return this.reloadCnt, this.lastAddr
}

// GetConfig 返回当前远端内容。
func (this *exampleRemoteClient) GetConfig(ctx context.Context, groupID, dataID string) ([]byte, error) {
	this.mu.Lock()
	defer this.mu.Unlock()
	return []byte(this.content), nil
}

// WatchConfig 注册变更回调并返回可关闭句柄。
func (this *exampleRemoteClient) WatchConfig(ctx context.Context, groupID, dataID string, onChange func(content []byte)) (io.Closer, error) {
	this.mu.Lock()
	this.onChange = onChange
	this.closed = false
	this.mu.Unlock()
	return &exampleWatchCloser{
		onClose: func() {
			this.mu.Lock()
			defer this.mu.Unlock()
			this.closed = true
		},
	}, nil
}

// EmitChange 主动触发一次远端变更。
func (this *exampleRemoteClient) EmitChange(content string) {
	this.mu.Lock()
	this.content = content
	onChange := this.onChange
	closed := this.closed
	this.mu.Unlock()
	if onChange != nil && !closed {
		onChange([]byte(content))
	}
}

// waitForCondition 轮询等待条件成立。
func waitForCondition(timeout time.Duration, check func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if check() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestUseCaseBuildReadExplain 演示调用方最小用法：Build -> Current -> Decode -> Explain。
func TestUseCaseBuildReadExplain(t *testing.T) {
	manager, err := config.New(
		config.File(config.FileDisabled()),
		config.Defaults(map[string]any{
			"service": map[string]any{"address": ":8080"},
			"worker":  map[string]any{"concurrency": 8},
		}),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	snapshot := manager.Current()
	if snapshot == nil {
		t.Fatalf("expected non-nil snapshot")
	}
	if got := snapshot.String("service.address", ""); got != ":8080" {
		t.Fatalf("expected service.address=:8080, got: %s", got)
	}
	if got := snapshot.Int("worker.concurrency", 0); got != 8 {
		t.Fatalf("expected worker.concurrency=8, got: %d", got)
	}

	var service serviceConfig
	if err = snapshot.Decode("service", &service); err != nil {
		t.Fatalf("decode snapshot failed: %v", err)
	}
	if service.Address != ":8080" {
		t.Fatalf("expected scoped service.address=:8080, got: %s", service.Address)
	}

	meta, ok := snapshot.Explain("service.address")
	if !ok {
		t.Fatalf("expected explain metadata for service.address")
	}
	if meta.Source != config.SourceKindDefault {
		t.Fatalf("expected source=default, got: %s", meta.Source)
	}
}

// TestUseCaseObserveWithWatcherRefresh 演示远端变更后 pull 最新 typed 状态。
func TestUseCaseObserveWithWatcherRefresh(t *testing.T) {
	client := &exampleRemoteClient{
		content: "service:\n  address: ':8080'\n",
	}
	manager, err := config.New(
		config.File(config.FileDisabled()),
		config.Provider(&exampleProvider{name: "demo", client: client}),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	observation, err := config.Observe[serviceConfig](manager, "service")
	if err != nil {
		t.Fatalf("observe service failed: %v", err)
	}
	initial, err := observation.Current()
	if err != nil || initial.Address != ":8080" {
		t.Fatalf("unexpected initial observation: value=%+v err=%v", initial, err)
	}

	client.EmitChange("service:\n  address: ':9090'\n")
	ok := waitForCondition(2*time.Second, func() bool {
		latest := manager.Current()
		if latest == nil {
			return false
		}
		current, loadErr := observation.Current()
		return loadErr == nil && latest.String("service.address", "") == ":9090" && current.Address == ":9090"
	})
	if !ok {
		t.Fatalf("expected watcher refresh to update service.address to :9090")
	}
}

// TestUseCaseCloseStopsWatcherEffect 演示 Close 后 watcher 回调不会再发布新快照。
func TestUseCaseCloseStopsWatcherEffect(t *testing.T) {
	client := &exampleRemoteClient{
		content: "service:\n  address: ':8080'\n",
	}
	manager, err := config.New(
		config.File(config.FileDisabled()),
		config.Provider(&exampleProvider{name: "demo", client: client}),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}

	latest := manager.Current()
	if latest == nil {
		t.Fatalf("expected latest snapshot")
	}
	beforeVersion := latest.Version()
	beforeAddress := latest.String("service.address", "")

	if err = manager.Close(); err != nil {
		t.Fatalf("close manager failed: %v", err)
	}

	client.EmitChange("service:\n  address: ':9999'\n")

	after := manager.Current()
	if after == nil {
		t.Fatalf("expected latest snapshot after close")
	}
	if after.Version() != beforeVersion {
		t.Fatalf("expected version unchanged after close, before=%d after=%d", beforeVersion, after.Version())
	}
	if got := after.String("service.address", ""); got != beforeAddress {
		t.Fatalf("expected address unchanged after close, want=%s got=%s", beforeAddress, got)
	}
}

// TestUseCaseSubscribeByScope 演示 Subscribe(scope) 的初次回放、更新与取消。
func TestUseCaseSubscribeByScope(t *testing.T) {
	client := &exampleRemoteClient{
		content: "service:\n  address: ':8080'\nworker:\n  concurrency: 8\n",
	}
	manager, err := config.New(
		config.File(config.FileDisabled()),
		config.Provider(&exampleProvider{name: "demo", client: client}),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	subscriber := &usecaseSubscriber{scope: "service"}
	subscription, err := manager.Subscribe(subscriber)
	if err != nil {
		t.Fatalf("subscribe failed: %v", err)
	}
	reloadCnt, lastAddr := subscriber.snapshot()
	if reloadCnt != 1 || lastAddr != ":8080" {
		t.Fatalf("expected initial replay count=1 addr=:8080, got count=%d addr=%s", reloadCnt, lastAddr)
	}

	client.EmitChange("service:\n  address: ':9090'\nworker:\n  concurrency: 8\n")
	ok := waitForCondition(2*time.Second, func() bool {
		reloadCnt, lastAddr = subscriber.snapshot()
		latest := manager.Current()
		if latest == nil {
			return false
		}
		return reloadCnt == 2 && lastAddr == ":9090" && latest.String("service.address", "") == ":9090"
	})
	if !ok {
		t.Fatalf("expected scoped listener to receive updated value")
	}

	subscription.Cancel()
	client.EmitChange("service:\n  address: ':10080'\nworker:\n  concurrency: 8\n")
	changedAfterCancel := waitForCondition(500*time.Millisecond, func() bool {
		reloadCnt, _ = subscriber.snapshot()
		return reloadCnt > 2
	})
	if changedAfterCancel {
		t.Fatalf("expected no listener callback after cancel")
	}
}

// TestUseCaseDecodeAndSubscribeLifecycle 演示 Decode/Subscribe 在更新、scope 缺失与取消下的行为。
// 测试方法：订阅 worker 并刷新；scope 消失时断言保留最后成功值，取消后不再接收后续版本。
func TestUseCaseDecodeAndSubscribeLifecycle(t *testing.T) {
	client := &exampleRemoteClient{
		content: "service:\n  address: ':8080'\nworker:\n  concurrency: 8\n",
	}
	manager, err := config.New(
		config.File(config.FileDisabled()),
		config.Provider(&exampleProvider{name: "demo", client: client}),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	var service serviceConfig
	if err = manager.Current().Decode("service", &service); err != nil {
		t.Fatalf("decode service failed: %v", err)
	}
	if service.Address != ":8080" {
		t.Fatalf("expected service.address=:8080, got: %s", service.Address)
	}

	var workerMu sync.Mutex
	worker := workerConfig{}
	subscription, err := config.Subscribe[workerConfig](manager, "worker", func(update config.TypedUpdate[workerConfig]) error {
		workerMu.Lock()
		worker = update.Value
		workerMu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe worker failed: %v", err)
	}
	workerMu.Lock()
	if worker.Concurrency != 8 {
		t.Fatalf("expected initial worker.concurrency=8, got: %d", worker.Concurrency)
	}
	workerMu.Unlock()

	client.EmitChange("service:\n  address: ':9090'\nworker:\n  concurrency: 16\n")
	ok := waitForCondition(2*time.Second, func() bool {
		workerMu.Lock()
		defer workerMu.Unlock()
		return worker.Concurrency == 16
	})
	if !ok {
		t.Fatalf("expected worker update to 16")
	}

	client.EmitChange("service:\n  address: ':7070'\n")
	ok = waitForCondition(2*time.Second, func() bool {
		_, exists := manager.Current().Get("worker.concurrency")
		return !exists
	})
	if !ok {
		t.Fatalf("expected published snapshot without worker scope")
	}
	workerMu.Lock()
	if worker.Concurrency != 16 {
		t.Fatalf("expected missing scope to preserve last successful binding, got: %d", worker.Concurrency)
	}
	workerMu.Unlock()

	subscription.Cancel()
	client.EmitChange("service:\n  address: ':6060'\nworker:\n  concurrency: 32\n")
	changedAfterCancel := waitForCondition(500*time.Millisecond, func() bool {
		workerMu.Lock()
		defer workerMu.Unlock()
		return worker.Concurrency != 16
	})
	if changedAfterCancel {
		t.Fatalf("expected worker unchanged after cancel, got: %d", worker.Concurrency)
	}
}

// TestUseCaseDecodeRejectsNonPointer 演示 Snapshot.Decode 对非法 out 参数会 fail-fast。
func TestUseCaseDecodeRejectsNonPointer(t *testing.T) {
	manager, err := config.New(
		config.File(config.FileDisabled()),
		config.Defaults(map[string]any{
			"service": map[string]any{"address": ":8080"},
		}),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	if err = manager.Current().Decode("service", serviceConfig{}); err == nil {
		t.Fatalf("expected Snapshot.Decode reject non-pointer out")
	}
}
