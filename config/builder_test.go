package config

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yangzhuangqiu/gobase/config/provider"
)

// noopCloser 用于模拟可关闭但无副作用的 watcher 句柄。
type noopCloser struct{}

// Close 返回 nil。
func (this noopCloser) Close() error {
	return nil
}

// fakeRemoteClient 用于可控注入 Get/Watch 行为。
type fakeRemoteClient struct {
	getConfigFn   func(ctx context.Context, groupID, dataID string) ([]byte, error)
	watchConfigFn func(ctx context.Context, groupID, dataID string, onChange func(content []byte)) (io.Closer, error)
}

// GetConfig 执行测试注入行为。
func (this *fakeRemoteClient) GetConfig(ctx context.Context, groupID, dataID string) ([]byte, error) {
	if this.getConfigFn == nil {
		return nil, nil
	}
	return this.getConfigFn(ctx, groupID, dataID)
}

// WatchConfig 执行测试注入行为。
func (this *fakeRemoteClient) WatchConfig(ctx context.Context, groupID, dataID string, onChange func(content []byte)) (io.Closer, error) {
	if this.watchConfigFn == nil {
		return nil, nil
	}
	return this.watchConfigFn(ctx, groupID, dataID, onChange)
}

// testSource 保存核心测试所需的不透明定位信息与中立元数据。
type testSource struct {
	// Name 是测试日志和 Explain 使用的 Source 名称。
	Name string
	// GroupID 是 fake client 使用的第一段定位信息。
	GroupID string
	// DataID 是 fake client 使用的第二段定位信息。
	DataID string
	// Priority 是远端层合并优先级。
	Priority int
	// Optional 表示测试 Source 是否允许加载或监听降级。
	Optional bool
}

// panicInfoSource 用于验证 Source.Info panic 会进入 Session 回滚路径。
type panicInfoSource struct{}

// Info 模拟扩展 Source 元信息读取异常。
func (this panicInfoSource) Info() provider.SourceInfo {
	panic("source info panic")
}

// Info 返回 Config 内核使用的中立 Source 元信息。
func (this testSource) Info() provider.SourceInfo {
	return provider.SourceInfo{Name: this.Name, Priority: this.Priority, Optional: this.Optional}
}

// testPlan 是测试内部的便捷声明，不属于 config/provider 公共 API。
type testPlan struct {
	// Provider 是注册去重使用的测试 Provider 名称。
	Provider string
	// Client 是测试加载与监听使用的可控客户端。
	Client *fakeRemoteClient
	// Sources 是本次 Open 返回的测试 Source 集合。
	Sources []testSource
	// Release 是 Session.Close 时调用的测试资源。
	Release io.Closer
}

// Name 返回测试 Provider 的稳定名称。
func (this testPlan) Name() string {
	return strings.TrimSpace(this.Provider)
}

// Open 把测试声明转换为一次独立 Session；完全零值表示未启用。
func (this testPlan) Open(ctx context.Context, _ map[string]any) (provider.Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return this.session(), nil
}

// session 把非零测试声明转换为 Session，并复制 Source 切片。
func (this testPlan) session() provider.Session {
	if strings.TrimSpace(this.Provider) == "" && this.Client == nil && len(this.Sources) == 0 && this.Release == nil {
		return nil
	}
	return &testSession{client: this.Client, sources: append([]testSource{}, this.Sources...), release: this.Release}
}

// testSession 适配旧 fake client 以验证新 Session 生命周期语义。
type testSession struct {
	// client 提供测试加载和监听行为。
	client *fakeRemoteClient
	// sources 是会话独占的 Source 快照。
	sources []testSource
	// release 是会话关闭时释放的测试资源。
	release io.Closer
}

// Sources 返回测试 Source 的接口快照。
func (this *testSession) Sources() []provider.Source {
	result := make([]provider.Source, 0, len(this.sources))
	for index := range this.sources {
		result = append(result, this.sources[index])
	}
	return result
}

// Load 通过 fake client 加载测试 Source。
func (this *testSession) Load(ctx context.Context, source provider.Source) ([]byte, error) {
	item := source.(testSource)
	if this.client == nil {
		return nil, errors.New("test client is required")
	}
	return this.client.GetConfig(ctx, item.GroupID, item.DataID)
}

// Watch 通过 fake client 监听测试 Source，并转换为失效通知。
func (this *testSession) Watch(ctx context.Context, source provider.Source, onChange func()) (io.Closer, error) {
	item := source.(testSource)
	if this.client == nil {
		return nil, errors.New("test client is required")
	}
	return this.client.WatchConfig(ctx, item.GroupID, item.DataID, func(_ []byte) { onChange() })
}

// Close 释放测试 Session 持有的资源。
func (this *testSession) Close() error {
	if this.release == nil {
		return nil
	}
	return this.release.Close()
}

// testProvider 是用于验证 Builder 核心语义的最小 provider。
type testProvider struct {
	name                 string
	loadEnvValues        map[string]any
	validateBootstrapFn  func(values map[string]any) error
	schema               []provider.BootstrapKey
	buildFromBootstrapFn func(values map[string]any) (testPlan, error)
}

type schemaOnlyProvider struct {
	name                 string
	schema               []provider.BootstrapKey
	buildFromBootstrapFn func(values map[string]any) (testPlan, error)
}

// directSessionProvider 允许测试直接返回任意 Session，不引入额外公共适配 API。
type directSessionProvider struct {
	// name 是注册使用的稳定 Provider 名称。
	name string
	// openFn 控制本次 Open 返回的 Session 与错误。
	openFn func() (provider.Session, error)
}

// Name 返回测试 Provider 的稳定名称。
func (this *directSessionProvider) Name() string { return this.name }

// Open 执行测试注入的会话建立行为。
func (this *directSessionProvider) Open(context.Context, map[string]any) (provider.Session, error) {
	return this.openFn()
}

// pullOnlySession 验证只实现 Load、不提供 Watch 的合法 Provider 能力。
type pullOnlySession struct {
	// sources 是本会话返回的 Source 集合。
	sources []provider.Source
	// content 是每次 Load 返回的 YAML 内容。
	content []byte
	// release 记录 Session 关闭所有权。
	release io.Closer
}

// Sources 返回 pull-only 会话的 Source 快照。
func (this *pullOnlySession) Sources() []provider.Source {
	return append([]provider.Source{}, this.sources...)
}

// Load 返回预置配置内容。
func (this *pullOnlySession) Load(context.Context, provider.Source) ([]byte, error) {
	return append([]byte{}, this.content...), nil
}

// Close 释放 pull-only 会话资源。
func (this *pullOnlySession) Close() error {
	if this.release == nil {
		return nil
	}
	return this.release.Close()
}

func (this *testProvider) Name() string {
	return this.name
}

func (this *testProvider) LoadBootstrapFromEnv(_ string) map[string]any {
	result := make(map[string]any, len(this.loadEnvValues))
	for key, value := range this.loadEnvValues {
		result[key] = value
	}
	return result
}

func (this *testProvider) ValidateBootstrap(values map[string]any) error {
	if this.validateBootstrapFn == nil {
		return nil
	}
	return this.validateBootstrapFn(values)
}

func (this *testProvider) BootstrapSchema() []provider.BootstrapKey {
	if len(this.schema) == 0 {
		return nil
	}
	result := make([]provider.BootstrapKey, len(this.schema))
	copy(result, this.schema)
	return result
}

func (this *testProvider) Open(_ context.Context, values map[string]any) (provider.Session, error) {
	if this.buildFromBootstrapFn == nil {
		return nil, nil
	}
	plan, err := this.buildFromBootstrapFn(values)
	return plan.session(), err
}

func (this *schemaOnlyProvider) Name() string {
	return this.name
}

func (this *schemaOnlyProvider) BootstrapSchema() []provider.BootstrapKey {
	if len(this.schema) == 0 {
		return nil
	}
	result := make([]provider.BootstrapKey, len(this.schema))
	copy(result, this.schema)
	return result
}

func (this *schemaOnlyProvider) Open(_ context.Context, values map[string]any) (provider.Session, error) {
	if this.buildFromBootstrapFn == nil {
		return nil, nil
	}
	plan, err := this.buildFromBootstrapFn(values)
	return plan.session(), err
}

// newStaticProvider 创建一个仅用于完成 provider 注册语义的测试 provider。
func newStaticProvider(name string) *testProvider {
	return &testProvider{
		name:                 name,
		buildFromBootstrapFn: func(values map[string]any) (testPlan, error) { return testPlan{}, nil },
	}
}

// TestPrepareRemotePlansReleasesEarlierResourcesOnFailure 验证 Provider 计划构建中途失败会回滚已取得资源。
// 测试方法：首个 Provider 返回带 Release 的有效 Plan，第二个返回错误，断言资源关闭一次。
func TestPrepareRemotePlansReleasesEarlierResourcesOnFailure(t *testing.T) {
	resource := &testCloser{}
	client := &fakeRemoteClient{}
	builder := New().
		withProvider(&testProvider{name: "a", buildFromBootstrapFn: func(map[string]any) (testPlan, error) {
			return testPlan{
				Provider: "a",
				Client:   client,
				Sources:  []testSource{{Name: "main", GroupID: "g", DataID: "d"}},
				Release:  resource,
			}, nil
		}}).
		withProvider(&testProvider{name: "b", buildFromBootstrapFn: func(map[string]any) (testPlan, error) {
			return testPlan{}, errors.New("build b failed")
		}})
	_, err := builder.prepareRemoteSessions(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected second provider failure")
	}
	if calls := resource.calls.Load(); calls != 1 {
		t.Fatalf("expected earlier provider resource released once, got=%d", calls)
	}
}

// TestBuildReleasesProviderResourceWhenInitialSnapshotFails 验证首次远端读取失败不会泄漏 Plan 资源。
// 测试方法：Provider 返回带 Release 的 Plan，Client 首次 GetConfig 失败，断言 Build 失败且资源关闭一次。
func TestBuildReleasesProviderResourceWhenInitialSnapshotFails(t *testing.T) {
	resource := &testCloser{}
	client := &fakeRemoteClient{getConfigFn: func(context.Context, string, string) ([]byte, error) {
		return nil, errors.New("initial load failed")
	}}
	builder := New().
		withoutConfigFile().
		withProvider(&testProvider{name: "managed", buildFromBootstrapFn: func(map[string]any) (testPlan, error) {
			return testPlan{
				Provider: "managed",
				Client:   client,
				Sources:  []testSource{{Name: "main", GroupID: "g", DataID: "d"}},
				Release:  resource,
			}, nil
		}})
	if _, err := builder.Build(context.Background()); err == nil {
		t.Fatalf("expected initial snapshot failure")
	}
	if calls := resource.calls.Load(); calls != 1 {
		t.Fatalf("expected provider resource released once, got=%d", calls)
	}
}

// TestBuildJoinsWatchStartAndRollbackErrors 验证 watcher 启动失败会保留补偿 Close 错误。
// 测试方法：首个 Source 返回失败 closer，第二个 Source 启动失败，断言两个 cause 均在 Build 错误链中。
func TestBuildJoinsWatchStartAndRollbackErrors(t *testing.T) {
	startErr := errors.New("second watcher start failed")
	closeErr := errors.New("first watcher close failed")
	watchCalls := 0
	client := &fakeRemoteClient{
		getConfigFn: func(context.Context, string, string) ([]byte, error) {
			return []byte("service:\n  name: demo\n"), nil
		},
		watchConfigFn: func(context.Context, string, string, func([]byte)) (io.Closer, error) {
			watchCalls++
			if watchCalls == 1 {
				return &testCloser{err: closeErr}, nil
			}
			return nil, startErr
		},
	}
	builder := New().
		withoutConfigFile().
		withProvider(&testProvider{name: "managed", buildFromBootstrapFn: func(map[string]any) (testPlan, error) {
			return testPlan{
				Provider: "managed",
				Client:   client,
				Sources: []testSource{
					{Name: "first", GroupID: "g", DataID: "a"},
					{Name: "second", GroupID: "g", DataID: "b"},
				},
			}, nil
		}})
	_, err := builder.Build(context.Background())
	if !errors.Is(err, startErr) || !errors.Is(err, closeErr) {
		t.Fatalf("expected start and rollback errors joined, got=%v", err)
	}
	assertErrorCode(t, err, ErrCodeConfigManagerCloseWatcherFailed)
}

// TestBuildAppliesPinnedValuesLayer 验证运行时 pinned 值会覆盖低优先级来源。
func TestBuildAppliesPinnedValuesLayer(t *testing.T) {
	manager, err := New().
		withDefaults(map[string]any{"service": map[string]any{"name": "default"}}).
		withPinnedValues(map[string]any{"service.name": "cli"}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	snapshot := manager.Current()
	if snapshot == nil {
		t.Fatalf("expected latest snapshot")
	}
	if got := snapshot.String("service.name", ""); got != "cli" {
		t.Fatalf("expected service.name=cli, got: %s", got)
	}
	meta, ok := manager.Current().Explain("service.name")
	if !ok {
		t.Fatalf("expected explain metadata for service.name")
	}
	if meta.Source != SourceKindPinned || !meta.Pinned {
		t.Fatalf("expected cli pinned meta, got: %#v", meta)
	}
}

// TestBuildFailsWhenInitialSnapshotValidatorReturnsError 验证 initial install 校验失败会阻断 Build。
func TestBuildFailsWhenInitialSnapshotValidatorReturnsError(t *testing.T) {
	_, err := New().
		withDefaults(map[string]any{"service": map[string]any{"name": "demo"}}).
		withSnapshotValidator(func(snapshot Snapshot) error {
			return errors.New("invalid_snapshot")
		}).
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected build error when snapshot validator fails")
	}
	assertErrorCode(t, err, ErrCodeConfigManagerSnapshotValidateFailed)
	assertErrorContains(t, err, "stage=install_initial_snapshot")
}

// TestBuildLoadsProviderBootstrapEnvBySchema 验证 provider schema 会自动映射 env 值。
func TestBuildLoadsProviderBootstrapEnvBySchema(t *testing.T) {
	t.Setenv("APP_DEMO_ENABLED", "true")

	called := false
	itemProvider := &schemaOnlyProvider{
		name: "demo",
		schema: []provider.BootstrapKey{
			{Key: "demo.enabled", Type: provider.BootstrapSchemaTypeBool},
		},
		buildFromBootstrapFn: func(values map[string]any) (testPlan, error) {
			called = true
			demo, ok := values["demo"].(map[string]any)
			if !ok {
				t.Fatalf("expected demo bootstrap block, got: %#v", values["demo"])
			}
			enabled, ok := demo["enabled"].(bool)
			if !ok || !enabled {
				t.Fatalf("expected demo.enabled bool=true, got: %#v", demo["enabled"])
			}
			return testPlan{}, nil
		},
	}

	manager, err := New().
		withoutConfigFile().
		withBootstrapEnvPrefix("app").
		withProvider(itemProvider).
		Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()
	if !called {
		t.Fatalf("expected provider Open to be called")
	}
}

// TestBuildLoadsProviderBootstrapEnvByLoader 验证可选 BootstrapEnvLoader 会叠加额外 env 映射。
func TestBuildLoadsProviderBootstrapEnvByLoader(t *testing.T) {
	called := false
	itemProvider := &testProvider{
		name: "demo",
		schema: []provider.BootstrapKey{
			{Key: "demo.enabled", Type: provider.BootstrapSchemaTypeBool},
		},
		loadEnvValues: map[string]any{
			"demo": map[string]any{
				"sources": []any{
					map[string]any{"groupId": "g", "dataId": "d.yaml"},
				},
			},
		},
		buildFromBootstrapFn: func(values map[string]any) (testPlan, error) {
			called = true
			demo, ok := values["demo"].(map[string]any)
			if !ok {
				t.Fatalf("expected demo bootstrap block, got: %#v", values["demo"])
			}
			rawSources, ok := demo["sources"].([]any)
			if !ok || len(rawSources) != 1 {
				t.Fatalf("expected demo.sources from optional loader, got: %#v", demo["sources"])
			}
			return testPlan{}, nil
		},
	}

	manager, err := New().
		withoutConfigFile().
		withBootstrapEnvPrefix("APP").
		withProvider(itemProvider).
		Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()
	if !called {
		t.Fatalf("expected provider Open to be called")
	}
}

// TestBuildFailsWhenProviderBootstrapSchemaTypeInvalid 验证 provider schema 类型非法会在 Build 早期 fail-fast。
func TestBuildFailsWhenProviderBootstrapSchemaTypeInvalid(t *testing.T) {
	itemProvider := &schemaOnlyProvider{
		name: "demo",
		schema: []provider.BootstrapKey{
			{Key: "demo.enabled", Type: "truthy"},
		},
	}

	_, err := New().
		withoutConfigFile().
		withProvider(itemProvider).
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected build error when provider schema type is invalid")
	}
	assertErrorCode(t, err, ErrCodeBootstrapInvalidType)
	assertErrorContains(t, err, "stage=validate_bootstrap_schema")
	assertErrorContains(t, err, "provider=demo")
}

// TestBuildFailsOnUnknownBootstrapRootKey 验证未知 bootstrap 根 key 会 fail-fast。
func TestBuildFailsOnUnknownBootstrapRootKey(t *testing.T) {
	_, err := New().
		withoutConfigFile().
		withProvider(newStaticProvider("nacos")).
		withBootstrapOverrides(map[string]any{
			"nacso.addr": "127.0.0.1:8848",
		}).
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected unknown bootstrap root key error")
	}
	assertErrorToken(t, err, "stage", "validate_bootstrap")
	assertErrorCode(t, err, ErrCodeBootstrapUnknownKey)
	assertErrorToken(t, err, "did_you_mean", "nacos")
}

// TestBuildFailsOnInvalidConfigBootstrapType 验证 bootstrap.config 必须是 string。
func TestBuildFailsOnInvalidConfigBootstrapType(t *testing.T) {
	_, err := New().
		withoutConfigFile().
		withBootstrapOverrides(map[string]any{
			"config.path": "a.yaml",
		}).
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected invalid bootstrap config type error")
	}
	assertErrorContains(t, err, "invalid bootstrap key config type")
	assertErrorCode(t, err, ErrCodeBootstrapInvalidType)
}

// TestBuildValidatesProviderBootstrapBeforeBuildingPlan 验证非法 bootstrap 会在 Provider 建立连接前 fail-fast。
// 测试方法：注册同时实现 validator 与 Open 的 Provider，输入非法字段并断言只触发校验失败。
func TestBuildValidatesProviderBootstrapBeforeBuildingPlan(t *testing.T) {
	providerCalled := false
	client := &fakeRemoteClient{
		getConfigFn: func(ctx context.Context, groupID, dataID string) ([]byte, error) {
			return []byte("service:\n  name: demo\n"), nil
		},
	}
	runtimeProvider := &testProvider{
		name: "demo",
		validateBootstrapFn: func(values map[string]any) error {
			providerCalled = true
			raw, exists := values["demo"]
			if !exists {
				return nil
			}
			block, ok := raw.(map[string]any)
			if !ok {
				return nil
			}
			if _, invalid := block["invalidField"]; invalid {
				return errors.New("invalid demo bootstrap field invalidField")
			}
			return nil
		},
		buildFromBootstrapFn: func(values map[string]any) (testPlan, error) {
			return testPlan{
				Provider: "demo",
				Sources:  []testSource{{Name: "main", GroupID: "g", DataID: "d"}},
				Client:   client,
			}, nil
		},
	}

	_, err := New().
		withoutConfigFile().
		withProvider(runtimeProvider).
		withBootstrapOverrides(map[string]any{
			"demo.invalidField": "x",
		}).
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected provider bootstrap validation error")
	}
	if !providerCalled {
		t.Fatalf("expected provider bootstrap validation to be called")
	}
	assertErrorToken(t, err, "stage", "validate_bootstrap")
	assertErrorCode(t, err, ErrCodeBootstrapProviderInvalid)
}

// TestBuildStripsBootstrapBlockFromRuntimeSnapshot 验证 bootstrap 块不会进入运行时快照。
func TestBuildStripsBootstrapBlockFromRuntimeSnapshot(t *testing.T) {
	file := writeYAML(t, "application.yaml", "bootstrap:\n  config: ignored.yaml\nservice:\n  name: demo\n")

	manager, err := New().withConfigFile(file).Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	snapshot := manager.Current()
	if snapshot == nil {
		t.Fatalf("expected latest snapshot")
	}
	if got := snapshot.String("service.name", ""); got != "demo" {
		t.Fatalf("expected service.name=demo, got: %s", got)
	}
	if _, ok := snapshot.Get("bootstrap.config"); ok {
		t.Fatalf("expected bootstrap block to be removed from runtime snapshot")
	}
}

// TestBuildMergesExplicitFilesInDeclarationOrder 验证多个显式文件按声明顺序形成稳定覆盖层。
// 测试方法：base 提供公共值，overlay 覆盖名称；Build 后断言合并结果和 Explain 文件来源。
func TestBuildMergesExplicitFilesInDeclarationOrder(t *testing.T) {
	dir := t.TempDir()
	baseFile := filepath.Join(dir, "application.yaml")
	overlayFile := filepath.Join(dir, "application-prod.yaml")
	if err := os.WriteFile(baseFile, []byte("service:\n  name: base\n  timeout: 5\n"), 0644); err != nil {
		t.Fatalf("write base file failed: %v", err)
	}
	if err := os.WriteFile(overlayFile, []byte("service:\n  name: prod\n"), 0644); err != nil {
		t.Fatalf("write overlay file failed: %v", err)
	}

	manager, err := New(File(FilePath(baseFile, overlayFile))).Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	if actual := manager.Current().String("service.name", ""); actual != "prod" {
		t.Fatalf("expected overlay service.name, got=%s", actual)
	}
	if actual := manager.Current().Int("service.timeout", 0); actual != 5 {
		t.Fatalf("expected base service.timeout, got=%d", actual)
	}
	meta, exists := manager.Current().Explain("service.name")
	if !exists || meta.Source != SourceKindFile || meta.Layer != fileLayerName(overlayFile) {
		t.Fatalf("unexpected overlay explain metadata: exists=%v meta=%#v", exists, meta)
	}
}

// TestPrepareRuntimePlanMergesFileBootstrapInDeclarationOrder 验证多个文件的 bootstrap 也按声明顺序覆盖。
// 测试方法：两个文件分别声明 watchInterval，构建计划后断言后文件值生效。
func TestPrepareRuntimePlanMergesFileBootstrapInDeclarationOrder(t *testing.T) {
	dir := t.TempDir()
	baseFile := filepath.Join(dir, "application.yaml")
	overlayFile := filepath.Join(dir, "application-prod.yaml")
	if err := os.WriteFile(baseFile, []byte("bootstrap:\n  configFile:\n    watchInterval: 2s\n"), 0644); err != nil {
		t.Fatalf("write base file failed: %v", err)
	}
	if err := os.WriteFile(overlayFile, []byte("bootstrap:\n  configFile:\n    watchInterval: 3s\n"), 0644); err != nil {
		t.Fatalf("write overlay file failed: %v", err)
	}

	plan, err := New(File(FilePath(baseFile, overlayFile), FileWatch())).prepareRuntimePlan(context.Background())
	if err != nil {
		t.Fatalf("prepare runtime plan failed: %v", err)
	}
	if plan.localFileWatchInterval != 3*time.Second {
		t.Fatalf("expected overlay watch interval=3s, got=%s", plan.localFileWatchInterval)
	}
}

// TestBuildFileWatchRefreshesAllFileLayers 验证任一文件变化都会全量重读有序文件集合。
// 测试方法：监听 base+overlay，修改 base 独有字段，等待 Snapshot 更新并确认 overlay 仍保持覆盖。
func TestBuildFileWatchRefreshesAllFileLayers(t *testing.T) {
	dir := t.TempDir()
	baseFile := filepath.Join(dir, "application.yaml")
	overlayFile := filepath.Join(dir, "application-prod.yaml")
	if err := os.WriteFile(baseFile, []byte("service:\n  base: v1\n  name: base\n"), 0644); err != nil {
		t.Fatalf("write base file failed: %v", err)
	}
	if err := os.WriteFile(overlayFile, []byte("service:\n  name: prod\n"), 0644); err != nil {
		t.Fatalf("write overlay file failed: %v", err)
	}

	manager, err := New(File(FilePath(baseFile, overlayFile), FileWatch(50*time.Millisecond))).Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	if err = os.WriteFile(baseFile, []byte("service:\n  base: changed-by-watch\n  name: base\n"), 0644); err != nil {
		t.Fatalf("rewrite base file failed: %v", err)
	}
	if !waitForCondition(3*time.Second, func() bool {
		return manager.Current().String("service.base", "") == "changed-by-watch"
	}) {
		t.Fatalf("expected base file watch to refresh snapshot")
	}
	if actual := manager.Current().String("service.name", ""); actual != "prod" {
		t.Fatalf("expected overlay to remain effective after refresh, got=%s", actual)
	}
}

// TestBuildUsesStartupFileSnapshotByDefault 验证默认文件层仅在启动阶段读取一次。
func TestBuildUsesStartupFileSnapshotByDefault(t *testing.T) {
	file := writeYAML(t, "application.yaml", "service:\n  name: v1\n")
	var onChange func(content []byte)
	client := &fakeRemoteClient{
		getConfigFn: func(ctx context.Context, groupID, dataID string) ([]byte, error) {
			return []byte{}, nil
		},
		watchConfigFn: func(ctx context.Context, groupID, dataID string, callback func(content []byte)) (io.Closer, error) {
			onChange = callback
			return noopCloser{}, nil
		},
	}
	manager, err := New().
		withConfigFile(file).
		withProvider(testPlan{
			Provider: "demo",
			Sources:  []testSource{{Name: "refresh", GroupID: "g", DataID: "d"}},
			Client:   client,
		}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	if got := manager.Current().String("service.name", ""); got != "v1" {
		t.Fatalf("expected startup file value v1, got: %s", got)
	}
	if onChange == nil {
		t.Fatalf("expected watch callback to be installed")
	}

	if err = os.WriteFile(file, []byte("service:\n  name: v2\n"), 0644); err != nil {
		t.Fatalf("rewrite file failed: %v", err)
	}
	onChange([]byte("remote_changed"))

	if got := manager.Current().String("service.name", ""); got != "v1" {
		t.Fatalf("expected cached file value v1 after refresh, got: %s", got)
	}
}

// TestBuildFileWatchRefreshesOnFileChange 验证启用本地文件监听后，文件变化会触发 refresh。
func TestBuildFileWatchRefreshesOnFileChange(t *testing.T) {
	file := writeYAML(t, "application.yaml", "service:\n  name: startup\n")
	manager, err := New().
		withConfigFile(file).
		withLocalFileWatch(50 * time.Millisecond).
		Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	if got := manager.Current().String("service.name", ""); got != "startup" {
		t.Fatalf("expected startup value, got: %s", got)
	}

	if err = os.WriteFile(file, []byte("service:\n  name: changed-by-watch\n"), 0644); err != nil {
		t.Fatalf("rewrite file failed: %v", err)
	}
	ok := waitForCondition(3*time.Second, func() bool {
		return manager.Current().String("service.name", "") == "changed-by-watch"
	})
	if !ok {
		t.Fatalf("expected local file watch to refresh snapshot")
	}
}

// TestLocalFileWatchRetriesSameFileVersionAfterPrePublishFailure 验证发布前失败不会确认文件扫描基线。
// 测试方法：修改文件后让首次 rebuild 失败，使用相同文件状态再次扫描，断言第二次仍会 refresh 并发布成功。
func TestLocalFileWatchRetriesSameFileVersionAfterPrePublishFailure(t *testing.T) {
	file := writeYAML(t, "application.yaml", "service:\n  name: old\n")
	initial, err := readLocalFileWatchStates([]string{file})
	if err != nil {
		t.Fatalf("read initial file state failed: %v", err)
	}
	if err = os.WriteFile(file, []byte("service:\n  name: changed-after-retry\n"), 0644); err != nil {
		t.Fatalf("rewrite file failed: %v", err)
	}

	manager := newDefaultManager()
	installInitialForTest(t, manager, snapshotState{raw: map[string]any{"service": map[string]any{"name": "old"}}})
	refreshCalls := 0
	manager.setRebuild(func(context.Context) (snapshotState, error) {
		refreshCalls++
		if refreshCalls == 1 {
			return snapshotState{}, errors.New("transient rebuild failure")
		}
		return snapshotState{raw: map[string]any{"service": map[string]any{"name": "changed-after-retry"}}}, nil
	})

	builder := New()
	current := builder.tryRefreshOnLocalFileChange(context.Background(), manager, []string{file}, initial, "test_scan", "test_refresh")
	if !localFileWatchStatesEqual(current, initial) {
		t.Fatalf("pre-publish failure must retain previous file baseline")
	}
	current = builder.tryRefreshOnLocalFileChange(context.Background(), manager, []string{file}, current, "test_scan", "test_refresh")
	if refreshCalls != 2 {
		t.Fatalf("expected unchanged file version to retry refresh, calls=%d", refreshCalls)
	}
	latest, err := readLocalFileWatchStates([]string{file})
	if err != nil {
		t.Fatalf("read latest file state failed: %v", err)
	}
	if !localFileWatchStatesEqual(current, latest) {
		t.Fatalf("successful publish must confirm latest file baseline")
	}
	if actual := manager.Current().String("service.name", ""); actual != "changed-after-retry" {
		t.Fatalf("expected retried snapshot to publish, got: %s", actual)
	}
}

// TestPrepareRuntimePlanUsesFileLocalWatchInterval 验证未显式传 interval 时，会读取文件 bootstrap 的轮询间隔。
//
// 测试方法：
// 1. 在配置文件 bootstrap 中写入 configFile.watchInterval=2（无单位按秒）；
// 2. 启用 withLocalFileWatch()（不传参数）；
// 3. 断言 prepareRuntimePlan 产出的轮询间隔为 2 秒。
func TestPrepareRuntimePlanUsesFileLocalWatchInterval(t *testing.T) {
	file := writeYAML(t, "application.yaml", "bootstrap:\n  configFile:\n    watchInterval: 2\nservice:\n  name: demo\n")
	builder := New().
		withConfigFile(file).
		withLocalFileWatch()

	plan, err := builder.prepareRuntimePlan(context.Background())
	if err != nil {
		t.Fatalf("prepare runtime plan failed: %v", err)
	}
	if got := plan.localFileWatchInterval; got != 2*time.Second {
		t.Fatalf("expected local file watch interval=2s, got: %s", got)
	}
}

// TestPrepareRuntimePlanLocalWatchIntervalFallsBackDefault 验证文件中的非法轮询值会回退默认 30 秒。
//
// 测试方法：
// 1. 在配置文件中写入非法值（非正整数）；
// 2. 启用 withLocalFileWatch()；
// 3. 断言运行计划轮询间隔回退到 defaultLocalFileWatchInterval。
func TestPrepareRuntimePlanLocalWatchIntervalFallsBackDefault(t *testing.T) {
	file := writeYAML(t, "application.yaml", "bootstrap:\n  configFile:\n    watchInterval: bad-value\nservice:\n  name: demo\n")
	builder := New().
		withConfigFile(file).
		withLocalFileWatch()

	plan, err := builder.prepareRuntimePlan(context.Background())
	if err != nil {
		t.Fatalf("prepare runtime plan failed: %v", err)
	}
	if got := plan.localFileWatchInterval; got != defaultLocalFileWatchInterval {
		t.Fatalf("expected fallback interval=%s, got: %s", defaultLocalFileWatchInterval, got)
	}
}

// TestPrepareRuntimePlanLocalWatchExplicitIntervalWins 验证显式传入 interval 会覆盖配置文件中的轮询值。
//
// 测试方法：
// 1. 配置文件写入轮询间隔 2s；
// 2. 调用 withLocalFileWatch(150ms)；
// 3. 断言运行计划采用 150ms，而非文件值。
func TestPrepareRuntimePlanLocalWatchExplicitIntervalWins(t *testing.T) {
	file := writeYAML(t, "application.yaml", "bootstrap:\n  configFile:\n    watchInterval: 2s\nservice:\n  name: demo\n")
	builder := New().
		withConfigFile(file).
		withLocalFileWatch(150 * time.Millisecond)

	plan, err := builder.prepareRuntimePlan(context.Background())
	if err != nil {
		t.Fatalf("prepare runtime plan failed: %v", err)
	}
	if got := plan.localFileWatchInterval; got != 150*time.Millisecond {
		t.Fatalf("expected explicit interval=150ms, got: %s", got)
	}
}

// TestBuildFailsWhenLocalWatchIntervalComesFromOverrides 验证本地轮询间隔不接受 override 注入（仅配置文件生效）。
//
// 测试方法：
// 1. 通过 WithBootstrapOverrides 注入 configFile.watchInterval；
// 2. 触发 Build；
// 3. 断言 strict bootstrap 校验以 unknown_key fail-fast。
func TestBuildFailsWhenLocalWatchIntervalComesFromOverrides(t *testing.T) {
	_, err := New().
		withLocalFileWatch().
		withBootstrapOverrides(map[string]any{
			"configFile.watchInterval": "2s",
		}).
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected build error")
	}
	assertErrorCode(t, err, ErrCodeBootstrapUnknownKey)
	assertErrorToken(t, err, "stage", "validate_bootstrap")
}

// TestPrepareRuntimePlanParsesConfigFileWatchIntervalUnits 验证 configFile.watchInterval 的单位解析语义。
//
// 测试方法：
// 1. 分别写入 ms/s/min 和无单位样例；
// 2. 构建运行计划并读取 localFileWatchInterval；
// 3. 断言结果与期望时长一致。
func TestPrepareRuntimePlanParsesConfigFileWatchIntervalUnits(t *testing.T) {
	cases := []struct {
		name     string
		rawValue string
		expect   time.Duration
	}{
		{name: "milliseconds", rawValue: "1500ms", expect: 1500 * time.Millisecond},
		{name: "seconds", rawValue: "5s", expect: 5 * time.Second},
		{name: "minutes", rawValue: "2min", expect: 2 * time.Minute},
		{name: "no_unit_is_seconds", rawValue: "7", expect: 7 * time.Second},
	}
	for _, item := range cases {
		item := item
		t.Run(item.name, func(t *testing.T) {
			file := writeYAML(t, "application.yaml", "bootstrap:\n  configFile:\n    watchInterval: "+item.rawValue+"\nservice:\n  name: demo\n")
			plan, err := New().
				withConfigFile(file).
				withLocalFileWatch().
				prepareRuntimePlan(context.Background())
			if err != nil {
				t.Fatalf("prepare runtime plan failed: %v", err)
			}
			if plan.localFileWatchInterval != item.expect {
				t.Fatalf("expected interval=%s, got: %s", item.expect, plan.localFileWatchInterval)
			}
		})
	}
}

// TestPrepareRuntimePlanConfigFileWatchIntervalOutOfRangeFallsBackDefault 验证轮询间隔越界会回退默认值。
//
// 测试方法：
// 1. 写入低于 1s 与高于 60min 的样例；
// 2. 构建运行计划；
// 3. 断言都回退到 defaultLocalFileWatchInterval。
func TestPrepareRuntimePlanConfigFileWatchIntervalOutOfRangeFallsBackDefault(t *testing.T) {
	cases := []string{"500ms", "61min"}
	for _, rawValue := range cases {
		rawValue := rawValue
		t.Run(rawValue, func(t *testing.T) {
			file := writeYAML(t, "application.yaml", "bootstrap:\n  configFile:\n    watchInterval: "+rawValue+"\nservice:\n  name: demo\n")
			plan, err := New().
				withConfigFile(file).
				withLocalFileWatch().
				prepareRuntimePlan(context.Background())
			if err != nil {
				t.Fatalf("prepare runtime plan failed: %v", err)
			}
			if plan.localFileWatchInterval != defaultLocalFileWatchInterval {
				t.Fatalf("expected fallback interval=%s, got: %s", defaultLocalFileWatchInterval, plan.localFileWatchInterval)
			}
		})
	}
}

// TestBuildFailsWhenProvidersHaveDuplicateNames 验证不同 Provider 同名时统一 fail-fast。
// 测试方法：同时注册同名 Provider 和 Plan，Build 后断言 duplicate 错误且不进入远端构建阶段。
func TestBuildFailsWhenProvidersHaveDuplicateNames(t *testing.T) {
	itemProvider := &testProvider{
		name: "demo",
		buildFromBootstrapFn: func(values map[string]any) (testPlan, error) {
			t.Fatalf("duplicate declarations must fail before Open")
			return testPlan{}, nil
		},
	}

	_, err := New().
		withoutConfigFile().
		withProvider(itemProvider).
		withProvider(testPlan{
			Provider: "demo",
			Sources:  []testSource{{Name: "main", GroupID: "g", DataID: "d"}},
			Client:   &fakeRemoteClient{},
		}).
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected duplicate provider error")
	}
	assertErrorCode(t, err, ErrCodeProviderDuplicate)
	assertErrorContains(t, err, "reason=duplicate_name")
}

// TestBuildRejectsTypedNilProvider 验证装入接口后的 typed nil Provider 会在获取资源前返回稳定错误。
// 测试方法：通过声明式 Provider Option 传入 nil *testProvider，断言 Build 不 panic 且返回 name_required。
func TestBuildRejectsTypedNilProvider(t *testing.T) {
	var item *testProvider
	_, err := New(File(FileDisabled()), Provider(item)).Build(context.Background())
	assertErrorCode(t, err, ErrCodeProviderNameRequired)
}

// TestBuildUsesRegisteredProviderNameForSession 验证 Provider 是唯一身份来源，Session 不重复声明名称。
// 测试方法：Provider 返回可加载 Session，断言内核使用注册名归因并成功加载，不存在双名称一致性负担。
func TestBuildUsesRegisteredProviderNameForSession(t *testing.T) {
	itemProvider := &testProvider{
		name: "demo",
		buildFromBootstrapFn: func(values map[string]any) (testPlan, error) {
			return testPlan{
				Client: &fakeRemoteClient{getConfigFn: func(context.Context, string, string) ([]byte, error) {
					return []byte("service:\n  name: session\n"), nil
				}},
				Sources: []testSource{{Name: "main", GroupID: "g", DataID: "d"}},
			}, nil
		},
	}
	manager, err := New().withoutConfigFile().withProvider(itemProvider).Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()
	if got := manager.Current().String("service.name", ""); got != "session" {
		t.Fatalf("expected session value, got=%q", got)
	}
}

// TestBuildFailsWhenSessionHasNoSources 验证非 nil Session 必须声明至少一个 Source。
// 测试方法：返回没有 Source 的 Session，断言在首次加载前返回稳定 session_invalid 错误。
func TestBuildFailsWhenSessionHasNoSources(t *testing.T) {
	_, err := New().withoutConfigFile().withProvider(testPlan{Provider: "demo", Client: &fakeRemoteClient{}}).Build(context.Background())
	if err == nil {
		t.Fatalf("expected incomplete session error")
	}
	assertErrorCode(t, err, ErrCodeProviderSessionInvalid)
	assertErrorContains(t, err, "reason=sources_required")
}

// TestBuildRollsBackPreviousSessionWhenProviderOpenPanics 验证 Provider.Open panic 不泄漏此前已打开会话。
// 测试方法：先打开带关闭计数的 Provider，再让后续 Provider panic，断言 Build 返回稳定错误且前一 Session 被关闭一次。
func TestBuildRollsBackPreviousSessionWhenProviderOpenPanics(t *testing.T) {
	const sensitivePayload = "token=provider-secret"
	release := &testCloser{}
	first := testPlan{
		Provider: "a-provider",
		Client: &fakeRemoteClient{getConfigFn: func(context.Context, string, string) ([]byte, error) {
			return []byte("service:\n  name: first\n"), nil
		}},
		Sources: []testSource{{Name: "main", GroupID: "g", DataID: "d"}},
		Release: release,
	}
	second := &directSessionProvider{name: "b-provider", openFn: func() (provider.Session, error) {
		panic(sensitivePayload)
	}}

	_, err := New(File(FileDisabled()), Provider(first), Provider(second)).Build(context.Background())
	if err == nil {
		t.Fatalf("expected provider panic error")
	}
	assertErrorContains(t, err, "reason=provider_call_panic")
	assertErrorContains(t, err, "payload_type=string")
	if strings.Contains(err.Error(), sensitivePayload) {
		t.Fatalf("provider panic error leaked payload: %v", err)
	}
	if release.calls.Load() != 1 {
		t.Fatalf("expected previous session rollback once, got=%d", release.calls.Load())
	}
}

// TestBuildClosesSessionWhenSourceInfoPanics 验证 Source.Info panic 会关闭当前 Session。
// 测试方法：返回包含 panic Source 的 pull Session，断言拓扑校验失败且 Session Close 执行一次。
func TestBuildClosesSessionWhenSourceInfoPanics(t *testing.T) {
	release := &testCloser{}
	itemSession := &pullOnlySession{sources: []provider.Source{panicInfoSource{}}, content: []byte("service:\n  name: demo\n"), release: release}
	item := &directSessionProvider{name: "panic-info", openFn: func() (provider.Session, error) { return itemSession, nil }}

	_, err := New(File(FileDisabled()), Provider(item)).Build(context.Background())
	if err == nil {
		t.Fatalf("expected source info panic error")
	}
	assertErrorContains(t, err, "stage=read_source_info")
	if release.calls.Load() != 1 {
		t.Fatalf("expected current session rollback once, got=%d", release.calls.Load())
	}
}

// TestBuildDoesNotDegradeOptionalSourceLoadPanic 验证 Optional 只降级普通加载失败，不吞掉扩展 panic。
// 测试方法：让 optional Source 的 Load panic，断言 Build 失败并关闭 Session。
func TestBuildDoesNotDegradeOptionalSourceLoadPanic(t *testing.T) {
	release := &testCloser{}
	item := testPlan{
		Provider: "panic-load",
		Client: &fakeRemoteClient{getConfigFn: func(context.Context, string, string) ([]byte, error) {
			panic("load panic")
		}},
		Sources: []testSource{{Name: "optional", GroupID: "g", DataID: "d", Optional: true}},
		Release: release,
	}

	_, err := New(File(FileDisabled()), Provider(item)).Build(context.Background())
	if err == nil {
		t.Fatalf("expected optional load panic to fail build")
	}
	assertErrorContains(t, err, "reason=provider_call_panic")
	if release.calls.Load() != 1 {
		t.Fatalf("expected session close once, got=%d", release.calls.Load())
	}
}

// TestBuildSupportsPullOnlySession 验证 WatchableSession 是可选能力。
// 测试方法：返回只实现 Session 的 Provider，断言初始加载成功、watcher 为零且 Close 释放会话一次。
func TestBuildSupportsPullOnlySession(t *testing.T) {
	release := &testCloser{}
	itemSession := &pullOnlySession{
		sources: []provider.Source{testSource{Name: "main"}},
		content: []byte("service:\n  name: pull-only\n"),
		release: release,
	}

	// 构建阶段应正常加载，但不为 pull-only Session 建立 watcher。
	manager, err := New(File(FileDisabled()), Provider(&directSessionProvider{
		name:   "pull-only",
		openFn: func() (provider.Session, error) { return itemSession, nil },
	})).Build(context.Background())
	if err != nil {
		t.Fatalf("build pull-only manager failed: %v", err)
	}
	if got := manager.Current().String("service.name", ""); got != "pull-only" {
		t.Fatalf("expected pull-only value, got=%q", got)
	}
	if status := manager.Status(); status.Resources().WatchCount != 0 || status.Resources().ProviderSessionCount != 1 {
		t.Fatalf("unexpected pull-only status: %+v", status)
	}

	// Manager 仍然拥有 Session，即使该 Session 没有 watcher 能力。
	if err = manager.Close(); err != nil {
		t.Fatalf("close pull-only manager failed: %v", err)
	}
	if calls := release.calls.Load(); calls != 1 {
		t.Fatalf("expected session released once, got=%d", calls)
	}
}

// TestBuildRejectsInvalidSessionSources 验证 Source 中立元信息在首次加载前完成校验。
// 测试方法：分别返回 nil、空名称和重名 Source，断言稳定 session_invalid 错误与可分支 reason。
func TestBuildRejectsInvalidSessionSources(t *testing.T) {
	tests := []struct {
		name    string
		sources []provider.Source
		reason  string
	}{
		{name: "nil", sources: []provider.Source{nil}, reason: "reason=source_is_nil"},
		{name: "empty_name", sources: []provider.Source{testSource{}}, reason: "reason=source_name_required"},
		{name: "duplicate_name", sources: []provider.Source{testSource{Name: "same"}, testSource{Name: "same"}}, reason: "reason=duplicate_source_name"},
	}

	// 每个无效拓扑都必须在调用 Load 前失败，并由构建回滚关闭 Session。
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			release := &testCloser{}
			_, err := New(File(FileDisabled()), Provider(&directSessionProvider{
				name: "invalid-" + item.name,
				openFn: func() (provider.Session, error) {
					return &pullOnlySession{sources: item.sources, release: release}, nil
				},
			})).Build(context.Background())
			assertErrorCode(t, err, ErrCodeProviderSessionInvalid)
			assertErrorContains(t, err, item.reason)
			if calls := release.calls.Load(); calls != 1 {
				t.Fatalf("expected invalid session released once, got=%d", calls)
			}
		})
	}
}

// TestBuildSupportsDirectProviderSession 验证直接 Provider 可通过统一入口加载远端配置。
// 测试方法：测试 Provider 在 Open 中返回 Session，并断言远端值进入最终快照。
func TestBuildSupportsDirectProviderSession(t *testing.T) {
	client := &fakeRemoteClient{
		getConfigFn: func(ctx context.Context, groupID, dataID string) ([]byte, error) {
			return []byte("service:\n  name: direct\n"), nil
		},
	}

	manager, err := New().
		withProvider(testPlan{
			Provider: "direct",
			Sources:  []testSource{{Name: "main", GroupID: "g", DataID: "d"}},
			Client:   client,
		}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	if got := manager.Current().String("service.name", ""); got != "direct" {
		t.Fatalf("expected service.name=direct, got: %s", got)
	}
}

// TestBuildFailsWhenDirectProviderNameMissing 验证直接 Provider 缺少名称时在声明阶段 fail-fast。
// 测试方法：注册名称为空的测试 Provider 并断言返回稳定 name_required 错误码。
func TestBuildFailsWhenDirectProviderNameMissing(t *testing.T) {
	_, err := New().
		withProvider(testPlan{
			Sources: []testSource{{GroupID: "g", DataID: "d"}},
			Client:  &fakeRemoteClient{},
		}).
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected build error")
	}
	assertErrorContains(t, err, "reason=name_required")
	assertErrorCode(t, err, ErrCodeProviderNameRequired)
}

// TestBuildRejectsBootstrapForProviderWithoutSchema 验证无 schema Provider 不静默接受 bootstrap 命名空间。
// 测试方法：注册无 schema Provider 并传入同名 bootstrap root，断言 strict 校验返回 unknown_key。
func TestBuildRejectsBootstrapForProviderWithoutSchema(t *testing.T) {
	_, err := New().
		withoutConfigFile().
		withProvider(testPlan{
			Provider: "managed",
			Client:   &fakeRemoteClient{},
			Sources:  []testSource{{Name: "main", GroupID: "g", DataID: "d"}},
		}).
		withBootstrapOverrides(map[string]any{"managed.enabled": true}).
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected fixed plan bootstrap root to be rejected")
	}
	assertErrorCode(t, err, ErrCodeBootstrapUnknownKey)
	assertErrorContains(t, err, "unknown bootstrap key managed")
}

// TestBuildFailsWhenRemotePriorityOutOfRange 验证 remote source priority 越界会在构建阶段 fail-fast。
func TestBuildFailsWhenRemotePriorityOutOfRange(t *testing.T) {
	_, err := New().
		withProvider(testPlan{
			Provider: "demo",
			Sources: []testSource{{
				Name:     "main",
				GroupID:  "g",
				DataID:   "d",
				Priority: remoteSourcePriorityMax + 1,
			}},
			Client: &fakeRemoteClient{},
		}).
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected build error when remote priority is out of range")
	}
	assertErrorCode(t, err, ErrCodeProviderSessionInvalid)
	assertErrorContains(t, err, "stage=validate_remote_source_priority")
	assertErrorContains(t, err, "reason=priority_out_of_range")
}

// TestBuildAcceptsRemotePriorityBoundary 验证 remote source priority 边界值可通过构建。
func TestBuildAcceptsRemotePriorityBoundary(t *testing.T) {
	client := &fakeRemoteClient{
		getConfigFn: func(ctx context.Context, groupID, dataID string) ([]byte, error) {
			return []byte("service:\n  name: remote\n"), nil
		},
	}
	for _, priority := range []int{remoteSourcePriorityMin, remoteSourcePriorityMax} {
		priority := priority
		t.Run("priority_"+strconv.Itoa(priority), func(t *testing.T) {
			manager, err := New().
				withPinnedValues(map[string]any{"service.name": "cli"}).
				withProvider(testPlan{
					Provider: "demo",
					Sources: []testSource{{
						Name:     "main",
						GroupID:  "g",
						DataID:   "d",
						Priority: priority,
					}},
					Client: client,
				}).
				Build(context.Background())
			if err != nil {
				t.Fatalf("build manager failed: %v", err)
			}
			defer func() { _ = manager.Close() }()
			if got := manager.Current().String("service.name", ""); got != "cli" {
				t.Fatalf("expected pinned value to win, got: %s", got)
			}
		})
	}
}

// TestBuildFailsWhenProviderPlanPriorityOutOfRange 验证 provider 解析计划中的优先级也会被统一约束。
func TestBuildFailsWhenProviderPlanPriorityOutOfRange(t *testing.T) {
	runtimeProvider := &testProvider{
		name: "demo",
		buildFromBootstrapFn: func(values map[string]any) (testPlan, error) {
			return testPlan{
				Provider: "demo",
				Sources: []testSource{{
					Name:     "main",
					GroupID:  "g",
					DataID:   "d",
					Priority: remoteSourcePriorityMin - 1,
				}},
				Client: &fakeRemoteClient{},
			}, nil
		},
	}
	_, err := New().
		withoutConfigFile().
		withProvider(runtimeProvider).
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected build error when provider plan priority is out of range")
	}
	assertErrorCode(t, err, ErrCodeProviderSessionInvalid)
	assertErrorContains(t, err, "stage=validate_remote_source_priority")
	assertErrorContains(t, err, "reason=priority_out_of_range")
}

// TestBuildMarksRemoteLayerAsRemoteSource verifies remote layer metadata.
func TestBuildMarksRemoteLayerAsRemoteSource(t *testing.T) {
	client := &fakeRemoteClient{
		getConfigFn: func(ctx context.Context, groupID, dataID string) ([]byte, error) {
			return []byte("service:\n  name: remote\n"), nil
		},
	}
	manager, err := New().
		withProvider(testPlan{
			Provider: "demo-provider",
			Sources:  []testSource{{Name: "main", GroupID: "g", DataID: "d"}},
			Client:   client,
		}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	meta, ok := manager.Current().Explain("service.name")
	if !ok {
		t.Fatalf("expected explain metadata for service.name")
	}
	if meta.Source != SourceKindRemote || meta.Layer != "demo-provider:main" {
		t.Fatalf("expected remote layer metadata, got: %#v", meta)
	}
}

// TestBuildFailsWhenConfigFileMissing 验证配置文件缺失时 Build 在启动阶段失败。
func TestBuildFailsWhenConfigFileMissing(t *testing.T) {
	_, err := New().
		withConfigFile("_not_exists_/application.yaml").
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected build error when config file is missing")
	}
	assertErrorToken(t, err, "stage", "prepare_runtime_plan")
	assertErrorToken(t, err, "stage", "resolve_file")
	assertErrorCode(t, err, ErrCodeConfigResolveFileFailed)
}

// TestBuildFailsWhenConfigFileDisabledButConfigured 验证禁用配置文件时显式配置路径会失败。
func TestBuildFailsWhenConfigFileDisabledButConfigured(t *testing.T) {
	_, err := New().
		withoutConfigFile().
		withConfigFile("application.yaml").
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected build error when config file is disabled but configured")
	}
	assertErrorCode(t, err, ErrCodeConfigConfigFileDisabledConflict)
	assertErrorToken(t, err, "reason", "config_file_disabled_but_explicitly_configured")
}

// TestWatcherRefreshErrorReportsByHandler 验证 watcher 刷新失败会回调上报。
func TestWatcherRefreshErrorReportsByHandler(t *testing.T) {
	watchTriggered := make(chan struct{}, 1)
	var onChange func(content []byte)
	refreshFailed := false
	client := &fakeRemoteClient{
		getConfigFn: func(ctx context.Context, groupID, dataID string) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if refreshFailed {
				return nil, errors.New("refresh_failed")
			}
			return []byte("service:\n  address: ':8080'\n"), nil
		},
		watchConfigFn: func(ctx context.Context, groupID, dataID string, callback func(content []byte)) (io.Closer, error) {
			onChange = callback
			return noopCloser{}, nil
		},
	}

	var reported error
	manager, err := New().
		withProvider(testPlan{
			Provider: "demo",
			Sources:  []testSource{{Name: "main", GroupID: "g", DataID: "d"}},
			Client:   client,
		}).
		withRefreshErrorHandler(func(err error) {
			reported = err
			watchTriggered <- struct{}{}
		}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	if onChange == nil {
		t.Fatalf("expected watch callback to be installed")
	}

	refreshFailed = true
	onChange([]byte("changed"))

	select {
	case <-watchTriggered:
	case <-time.After(2 * time.Second):
		t.Fatalf("expected refresh error handler to be called")
	}
	if reported == nil {
		t.Fatalf("expected reported refresh error")
	}
	assertErrorToken(t, reported, "stage", "watch_demo_refresh")
	assertErrorCode(t, reported, ErrCodeConfigWatchRefreshFailed)
}

type watcherContextKey struct{}

// TestBuildContextCancellationDoesNotStopManagerWatchers 验证 Build 成功后 watcher 只由 Manager Close 终止。
// 测试方法：使用可取消且携带值的 Build context 创建远端 watcher，取消父 context 后触发 refresh，断言新版本仍发布且上下文值保留；最后 Close 并断言 watcher context 被取消。
func TestBuildContextCancellationDoesNotStopManagerWatchers(t *testing.T) {
	content := "service:\n  address: ':8080'\n"
	var (
		watchCtx context.Context
		onChange func(content []byte)
	)
	client := &fakeRemoteClient{
		getConfigFn: func(ctx context.Context, groupID, dataID string) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return []byte(content), nil
		},
		watchConfigFn: func(ctx context.Context, groupID, dataID string, callback func(content []byte)) (io.Closer, error) {
			watchCtx = ctx
			onChange = callback
			return noopCloser{}, nil
		},
	}

	parent := context.WithValue(context.Background(), watcherContextKey{}, "trace-value")
	buildCtx, cancelBuild := context.WithCancel(parent)
	manager, err := New(
		File(FileDisabled()),
		Provider(testPlan{
			Provider: "demo",
			Sources:  []testSource{{Name: "main", GroupID: "g", DataID: "d"}},
			Client:   client,
		}),
	).Build(buildCtx)
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}

	cancelBuild()
	if watchCtx == nil || onChange == nil {
		_ = manager.Close()
		t.Fatal("expected watcher context and callback")
	}
	select {
	case <-watchCtx.Done():
		_ = manager.Close()
		t.Fatal("build context cancellation must not stop manager-owned watcher")
	default:
	}
	if actual := watchCtx.Value(watcherContextKey{}); actual != "trace-value" {
		_ = manager.Close()
		t.Fatalf("watcher context lost build value: %v", actual)
	}

	content = "service:\n  address: ':9090'\n"
	onChange([]byte("changed"))
	if actual := manager.Current().String("service.address", ""); actual != ":9090" {
		_ = manager.Close()
		t.Fatalf("manager watcher stopped after build context cancellation: %s", actual)
	}
	if status := manager.Status(); status.State() != ManagerStateRunning || status.Resources().WatchCount != 1 {
		_ = manager.Close()
		t.Fatalf("unexpected active manager status: %+v", status)
	}

	if err = manager.Close(); err != nil {
		t.Fatalf("close manager failed: %v", err)
	}
	select {
	case <-watchCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("manager close did not cancel watcher context")
	}
}

// TestWatcherRefreshErrorHandlerPanicIsRecovered 验证上报回调 panic 不影响主流程。
func TestWatcherRefreshErrorHandlerPanicIsRecovered(t *testing.T) {
	var onChange func(content []byte)
	refreshFailed := false
	client := &fakeRemoteClient{
		getConfigFn: func(ctx context.Context, groupID, dataID string) ([]byte, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if refreshFailed {
				return nil, errors.New("refresh_failed")
			}
			return []byte("service:\n  address: ':8080'\n"), nil
		},
		watchConfigFn: func(ctx context.Context, groupID, dataID string, callback func(content []byte)) (io.Closer, error) {
			onChange = callback
			return noopCloser{}, nil
		},
	}

	handlerCalls := 0
	manager, err := New().
		withProvider(testPlan{
			Provider: "demo",
			Sources:  []testSource{{Name: "main", GroupID: "g", DataID: "d"}},
			Client:   client,
		}).
		withRefreshErrorHandler(func(err error) {
			handlerCalls++
			panic("handler panic")
		}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("build manager failed: %v", err)
	}
	defer func() { _ = manager.Close() }()

	refreshFailed = true
	onChange([]byte("changed_1"))
	onChange([]byte("changed_2"))

	if handlerCalls != 2 {
		t.Fatalf("expected handler to be called twice, got: %d", handlerCalls)
	}
}

// TestEmitRefreshErrorHandlerPanicIsLogged 验证回调 panic 会留下可观测日志。
func TestEmitRefreshErrorHandlerPanicIsLogged(t *testing.T) {
	var buf bytes.Buffer
	originWriter := log.Writer()
	originFlags := log.Flags()
	originPrefix := log.Prefix()
	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")
	defer func() {
		log.SetOutput(originWriter)
		log.SetFlags(originFlags)
		log.SetPrefix(originPrefix)
	}()

	emitRefreshError(func(err error) {
		panic("handler panic")
	}, errors.New("refresh failed"))

	logged := buf.String()
	if !strings.Contains(logged, "op=config.builder.refresh_error_handler result=failed stage=callback_panic") {
		t.Fatalf("expected panic observability log, got: %s", logged)
	}
	if !strings.Contains(logged, "handler panic") {
		t.Fatalf("expected panic reason in log, got: %s", logged)
	}
}

// TestBuildFailsWhenWatchStartFailsForRequiredSource 验证 required source 的 watcher 启动失败会导致 Build 失败。
func TestBuildFailsWhenWatchStartFailsForRequiredSource(t *testing.T) {
	client := &fakeRemoteClient{
		getConfigFn: func(ctx context.Context, groupID, dataID string) ([]byte, error) {
			return []byte("service:\n  address: ':8080'\n"), nil
		},
		watchConfigFn: func(ctx context.Context, groupID, dataID string, callback func(content []byte)) (io.Closer, error) {
			return nil, errors.New("watch_failed")
		},
	}

	_, err := New().
		withProvider(testPlan{
			Provider: "demo",
			Sources:  []testSource{{Name: "required", GroupID: "g", DataID: "d", Optional: false}},
			Client:   client,
		}).
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected build error when required watcher fails")
	}
	assertErrorToken(t, err, "stage", "start_watchers")
	assertErrorToken(t, err, "stage", "watch_demo")
	assertErrorCode(t, err, ErrCodeConfigWatchFailed)
}

// TestBuildSkipsWatchStartFailureForOptionalSource 验证 optional source 的 watcher 启动失败不会阻断 Build。
func TestBuildSkipsWatchStartFailureForOptionalSource(t *testing.T) {
	client := &fakeRemoteClient{
		getConfigFn: func(ctx context.Context, groupID, dataID string) ([]byte, error) {
			return []byte("service:\n  address: ':8080'\n"), nil
		},
		watchConfigFn: func(ctx context.Context, groupID, dataID string, callback func(content []byte)) (io.Closer, error) {
			return nil, errors.New("watch_failed")
		},
	}

	manager, err := New().
		withProvider(testPlan{
			Provider: "demo",
			Sources:  []testSource{{Name: "optional", GroupID: "g", DataID: "d", Optional: true}},
			Client:   client,
		}).
		Build(context.Background())
	if err != nil {
		t.Fatalf("expected optional watcher failure to be skipped, got: %v", err)
	}
	defer func() { _ = manager.Close() }()
}

// TestBuildFailsWhenInitialRemoteLoadFails 验证初始 required remote 拉取失败会导致 Build 失败。
func TestBuildFailsWhenInitialRemoteLoadFails(t *testing.T) {
	client := &fakeRemoteClient{
		getConfigFn: func(ctx context.Context, groupID, dataID string) ([]byte, error) {
			return nil, errors.New("remote_get_failed")
		},
	}

	_, err := New().
		withProvider(testPlan{
			Provider: "demo",
			Sources:  []testSource{{Name: "required", GroupID: "g", DataID: "d", Optional: false}},
			Client:   client,
		}).
		Build(context.Background())
	if err == nil {
		t.Fatalf("expected build error when initial remote load fails")
	}
	assertErrorToken(t, err, "stage", "build_initial_snapshot")
	assertErrorToken(t, err, "stage", "load_demo_layer")
	assertErrorCode(t, err, ErrCodeConfigSnapshotRemoteLoadFailed)
}

// waitForCondition 在给定超时内轮询检查条件是否成立。
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

// writeYAML 写入测试 YAML 文件并返回绝对路径。
func writeYAML(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write yaml failed: %v", err)
	}
	return path
}
