package appruntime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/yangzhuangqiu/gobase/config"
	providerapi "github.com/yangzhuangqiu/gobase/config/provider"
	"github.com/yangzhuangqiu/gobase/errcode"
	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

type runtimeTestDependency struct {
	// Trace 记录 bean 初始化与关闭顺序。
	Trace *[]string `ioc:"-"`
}

// runtimeTestRole 定义 Bean Definition 测试使用的语义接口。
type runtimeTestRole interface {
	// Role 返回可断言的稳定角色值。
	Role() string
}

// runtimeTestRoleBean 验证完整 Definition 会把同一实例发布到接口索引。
type runtimeTestRoleBean struct{}

// Role 返回测试角色值。
func (this *runtimeTestRoleBean) Role() string {
	return "definition"
}

// Init 记录依赖初始化动作。
func (this *runtimeTestDependency) Init(context.Context) error {
	*this.Trace = append(*this.Trace, "init-dependency")
	return nil
}

// Close 记录依赖关闭动作。
func (this *runtimeTestDependency) Close(context.Context) error {
	*this.Trace = append(*this.Trace, "close-dependency")
	return nil
}

type runtimeTestBean struct {
	// Dependency 驱动 bean 生命周期拓扑。
	Dependency *runtimeTestDependency
	// Runtime 验证运行时基础 bean 可注入。
	Runtime *Runtime
	// Manager 验证配置管理器基础 bean 可注入。
	Manager config.Manager
	// Logger 验证默认日志基础 bean 可注入。
	Logger baselog.Logger
	// Info 验证应用元信息基础 bean 可注入。
	Info *ApplicationInfo
	// Trace 记录生命周期顺序，不参与依赖注入。
	Trace *[]string `ioc:"-"`
}

type runtimeTestPanicBean struct{}

// Init 模拟业务初始化中的非预期 panic。
func (this *runtimeTestPanicBean) Init(context.Context) error {
	panic("runtime init panic")
}

// Init 记录业务 bean 初始化动作。
func (this *runtimeTestBean) Init(context.Context) error {
	*this.Trace = append(*this.Trace, "init-bean")
	return nil
}

// Close 记录业务 bean 关闭动作。
func (this *runtimeTestBean) Close(context.Context) error {
	*this.Trace = append(*this.Trace, "close-bean")
	return nil
}

type runtimeTestProvider struct {
	// started 在 Close 开始时通知并发测试。
	started chan struct{}
	// release 控制 Close 完成时机；为空时立即返回。
	release chan struct{}
	// closeErr 是 Close 返回的可观察错误。
	closeErr error
	// closePanic 是 Close 需要模拟的意外 panic payload。
	closePanic any
	// mu 保护 closeCount。
	mu sync.Mutex
	// closeCount 记录实际关闭次数。
	closeCount int
}

var runtimeTestLoggerFactory = baselog.NewLoggerFactory(slog.NewTextHandler(io.Discard, nil))

// Factory 返回稳定的丢弃输出工厂，满足测试 provider 契约。
func (this *runtimeTestProvider) Factory() baselog.LoggerFactory {
	return runtimeTestLoggerFactory
}

// Close 记录关闭次数，并按测试信号控制完成时机。
func (this *runtimeTestProvider) Close() error {
	this.mu.Lock()
	this.closeCount++
	this.mu.Unlock()
	if this.started != nil {
		select {
		case <-this.started:
		default:
			close(this.started)
		}
	}
	if this.release != nil {
		<-this.release
	}
	if this.closePanic != nil {
		panic(this.closePanic)
	}
	return this.closeErr
}

// count 返回 provider 实际关闭次数。
func (this *runtimeTestProvider) count() int {
	this.mu.Lock()
	defer this.mu.Unlock()
	return this.closeCount
}

type runtimeTestLogging struct {
	// provider 是 Init 返回并交给 Runtime 托管的日志 provider。
	provider baselog.Provider
}

type runtimeTestConfigProvider struct {
	// name 是 provider 稳定标识。
	name string
}

// Name 返回测试 provider 名称。
func (this *runtimeTestConfigProvider) Name() string {
	return this.name
}

// LoadBootstrapFromEnv 返回空环境输入，测试不依赖进程环境。
func (this *runtimeTestConfigProvider) LoadBootstrapFromEnv(string) map[string]any {
	return map[string]any{}
}

// ValidateBootstrap 接受所有测试 bootstrap 输入。
func (this *runtimeTestConfigProvider) ValidateBootstrap(map[string]any) error {
	return nil
}

// BootstrapSchema 返回 provider 自有发现键。
func (this *runtimeTestConfigProvider) BootstrapSchema() []providerapi.BootstrapKey {
	return []providerapi.BootstrapKey{{Key: this.name + ".enabled", Type: providerapi.BootstrapSchemaTypeBool}}
}

// Open 返回 nil Session，避免测试建立远端连接。
func (this *runtimeTestConfigProvider) Open(context.Context, map[string]any) (providerapi.Session, error) {
	return nil, nil
}

// Init 返回预置 provider，验证 Runtime 的日志生命周期所有权。
func (this runtimeTestLogging) Build(context.Context, config.Manager) (baselog.Provider, error) {
	return this.provider, nil
}

// TestUseCaseBuildRuntime 验证运行时可统一构建 config/log/ioc 并注入基础 bean。
//
// 测试方法：
// 1. 创建显式 Module，故意先注册消费者、后注册依赖；
// 2. Build 后按名称解析业务 bean 并检查基础依赖；
// 3. Close 后断言生命周期使用真实 bean 拓扑逆序。
func TestUseCaseBuildRuntime(t *testing.T) {
	trace := make([]string, 0, 4)
	application := New(
		WithName("runtime-test"),
		WithConfig(config.File(config.FileDisabled())),
		WithModules(NewModule("test", func(registry ioc.Registry) error {
			if err := registry.Register(&runtimeTestBean{Trace: &trace}, ioc.WithName("RuntimeTestBean")); err != nil {
				return err
			}
			return registry.Register(&runtimeTestDependency{Trace: &trace})
		})),
	)
	runtime, err := application.Build(context.Background())
	if err != nil {
		t.Fatalf("build runtime failed: %v", err)
	}
	var bean *runtimeTestBean
	if err = runtime.Container().ResolveByName("RuntimeTestBean", &bean); err != nil {
		t.Fatalf("resolve runtime bean failed: %v", err)
	}
	if bean.Runtime != runtime || bean.Manager != runtime.ConfigManager() || bean.Logger == nil {
		t.Fatalf("runtime base beans were not injected: %+v", bean)
	}
	if bean.Info == nil || bean.Info.Name != "runtime-test" {
		t.Fatalf("unexpected application info: %+v", bean.Info)
	}
	if err = runtime.Close(context.Background()); err != nil {
		t.Fatalf("close runtime failed: %v", err)
	}
	expect := []string{"init-dependency", "init-bean", "close-bean", "close-dependency"}
	if !slices.Equal(trace, expect) {
		t.Fatalf("unexpected lifecycle trace: expect=%v actual=%v", expect, trace)
	}
}

// TestUseCaseBeanDefinitionsPublishSemanticInterfaces 验证 Runtime 能原样注册完整 IOC Definition。
//
// 测试方法：
// 1. 使用 WithBeanDefinitions 声明具体 Bean、接口类型和名称；
// 2. Build 后分别按接口和名称解析；
// 3. 断言两种索引都返回原始实例，并由 Runtime 正常关闭。
func TestUseCaseBeanDefinitionsPublishSemanticInterfaces(t *testing.T) {
	// 第一阶段：由应用蓝图保存完整 Definition，接口声明与名称不在 app-runtime 重建。
	bean := &runtimeTestRoleBean{}
	application := New(
		WithConfig(config.File(config.FileDisabled())),
		WithBeanDefinitions(ioc.Define(
			bean,
			ioc.WithName("RuntimeTestRole"),
			ioc.WithDeclare[runtimeTestRole](),
		)),
	)
	runtime, err := application.Build(context.Background())
	if err != nil {
		t.Fatalf("build runtime with definition: %v", err)
	}
	// 第二阶段：从 IOC 的接口与名称索引验证原始 Definition 被无损注册。
	resolved, err := ioc.ResolveAs[runtimeTestRole](runtime.Container())
	if err != nil || resolved != bean || resolved.Role() != "definition" {
		t.Fatalf("unexpected interface resolution: resolved=%v err=%v", resolved, err)
	}
	named, err := ioc.ResolveByNameAs[runtimeTestRole](runtime.Container(), "RuntimeTestRole")
	if err != nil || named != bean {
		t.Fatalf("unexpected named resolution: resolved=%v err=%v", named, err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("close runtime with definition: %v", err)
	}
}

// TestUseCaseBeanDefinitionsRejectNilBeforeResourceBuild 验证空 Definition 在 Runtime 获取资源前失败。
//
// 测试方法：声明空 Bean Definition 后调用 Build，断言返回稳定 bean_invalid 且不创建 Runtime。
func TestUseCaseBeanDefinitionsRejectNilBeforeResourceBuild(t *testing.T) {
	application := New(WithBeanDefinitions(ioc.Definition{}))
	runtime, err := application.Build(context.Background())
	if runtime != nil || !errcode.IsCode[ErrorCode](err, ErrCodeBeanInvalid) {
		t.Fatalf("expected bean definition declaration error: runtime=%v err=%v", runtime, err)
	}
}

// TestUseCaseBuildFailureClosesAcquiredResources 验证自动配置失败会关闭已取得的日志与配置资源。
//
// 测试方法：
// 1. 注入可计数日志 provider，并让 Module Register 返回错误；
// 2. 调用 Build；
// 3. 断言 Build 失败且日志 provider 恰好关闭一次。
func TestUseCaseBuildFailureClosesAcquiredResources(t *testing.T) {
	provider := &runtimeTestProvider{}
	application := New(
		WithConfig(config.File(config.FileDisabled())),
		WithLogging(runtimeTestLogging{provider: provider}),
		WithModules(NewModule("failed", func(ioc.Registry) error {
			return errors.New("register failed")
		})),
	)
	_, err := application.Build(context.Background())
	if err == nil {
		t.Fatalf("build should fail")
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeRuntimeBuildFailed) {
		t.Fatalf("expect runtime build error code, got=%v", err)
	}
	if provider.count() != 1 {
		t.Fatalf("provider must close once on rollback, got=%d", provider.count())
	}
}

// TestUseCaseBuildPanicClosesAcquiredResources 验证业务 Init panic 不会绕过 Runtime 外层资源回滚。
//
// 测试方法：
// 1. 注入可计数日志 provider，并通过 Module 注册 panic bean；
// 2. recover Application.Build panic；
// 3. 断言 panic 同时保留 Runtime build 与 IOC init_failed 语义，日志 provider 恰好关闭一次。
func TestUseCaseBuildPanicClosesAcquiredResources(t *testing.T) {
	provider := &runtimeTestProvider{}
	application := New(
		WithConfig(config.File(config.FileDisabled())),
		WithLogging(runtimeTestLogging{provider: provider}),
		WithModules(NewModule("panic", func(registry ioc.Registry) error {
			return registry.Register(&runtimeTestPanicBean{})
		})),
	)

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		_, _ = application.Build(context.Background())
	}()
	if recovered == nil {
		t.Fatal("build should preserve unexpected Init panic")
	}
	recoveredErr, ok := recovered.(error)
	if !ok {
		t.Fatalf("panic payload must be error, got=%T", recovered)
	}
	if !errcode.IsCode[ErrorCode](recoveredErr, ErrCodeRuntimeBuildFailed) {
		t.Fatalf("expect runtime build error code, got=%v", recoveredErr)
	}
	if !errcode.IsCode[ioc.ErrorCode](recoveredErr, ioc.ErrCodeIOCBuildInitFailed) {
		t.Fatalf("expect IOC init failed cause, got=%v", recoveredErr)
	}
	if provider.count() != 1 {
		t.Fatalf("provider must close once on panic rollback, got=%d", provider.count())
	}
}

// TestUseCaseConcurrentCloseReturnsFirstResult 验证并发 Close 等待首次清理并返回相同结果。
//
// 测试方法：
// 1. 使用阻塞且返回错误的日志 provider 构建 Runtime；
// 2. 并发调用两次 Close，在首次关闭开始后释放阻塞；
// 3. 断言两次结果都可按 close_failed 分支，且 provider 只关闭一次。
func TestUseCaseConcurrentCloseReturnsFirstResult(t *testing.T) {
	provider := &runtimeTestProvider{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		closeErr: errors.New("close failed"),
	}
	application := New(WithConfig(config.File(config.FileDisabled())), WithLogging(runtimeTestLogging{provider: provider}))
	runtime, err := application.Build(context.Background())
	if err != nil {
		t.Fatalf("build runtime failed: %v", err)
	}
	results := make(chan error, 2)
	go func() { results <- runtime.Close(context.Background()) }()
	<-provider.started
	go func() { results <- runtime.Close(context.Background()) }()
	close(provider.release)
	for index := 0; index < 2; index++ {
		closeErr := <-results
		if !errcode.IsCode[ErrorCode](closeErr, ErrCodeRuntimeCloseFailed) {
			t.Fatalf("expect close_failed result, got=%v", closeErr)
		}
	}
	if provider.count() != 1 {
		t.Fatalf("provider must close once, got=%d", provider.count())
	}
}

// TestUseCaseBlueprintRejectsDuplicateDeclarations 验证运行时蓝图对重复配置 fail-fast。
//
// 测试方法：
// 1. 注册两个同名 Module；
// 2. 调用 Build；
// 3. 断言返回 auto_configuration.invalid 错误码。
func TestUseCaseBlueprintRejectsDuplicateDeclarations(t *testing.T) {
	module := NewModule("duplicate", func(ioc.Registry) error { return nil })
	application := New(WithConfig(config.File(config.FileDisabled())), WithModules(module, module))
	_, err := application.Build(context.Background())
	if !errcode.IsCode[ErrorCode](err, ErrCodeModuleInvalid) {
		t.Fatalf("expect duplicate auto configuration error, got=%v", err)
	}
}

// TestUseCaseProviderExposesBootstrapSchema 验证 Runtime 从 Provider 可选能力收集 CLI schema。
//
// 测试方法：
// 1. 注册实现 BootstrapSchemaProvider 的 Provider；
// 2. 读取 BootstrapSchemas；
// 3. 断言返回 Provider 自身声明的发现键。
func TestUseCaseProviderExposesBootstrapSchema(t *testing.T) {
	application := New(WithConfig(config.Provider(&runtimeTestConfigProvider{name: "runtime"})))
	schemas := application.BootstrapSchemas()
	if len(schemas["runtime"]) != 1 || schemas["runtime"][0].Key != "runtime.enabled" {
		t.Fatalf("unexpected provider schema: %+v", schemas)
	}
}

// TestUseCaseProviderWithoutSchemaDoesNotExposeBootstrapSchema 验证普通 Provider 不产生空 schema 条目。
// 测试方法：注册未实现 BootstrapSchemaProvider 的 Provider，读取 BootstrapSchemas 并断言不存在对应名称。
func TestUseCaseProviderWithoutSchemaDoesNotExposeBootstrapSchema(t *testing.T) {
	application := New(WithConfig(config.Provider(&runtimeSessionProvider{name: "managed"})))
	if _, exists := application.BootstrapSchemas()["managed"]; exists {
		t.Fatalf("provider without schema must not expose bootstrap schema")
	}
}

type runtimeBoundConfig struct {
	// Name 是 app-runtime typed binding 和 IOC 注入的断言字段。
	Name string `yaml:"name"`
}

type runtimeBoundBean struct {
	// Config 验证 *ConfigValue[T] 可由 Bean Definition 自动注入。
	Config *ConfigValue[runtimeBoundConfig]
}

// runtimeConfigClient 提供 app-runtime typed binding 热更新测试所需的可控远端来源。
type runtimeConfigClient struct {
	// mu 保护 content 与 onChange，允许测试同步触发刷新。
	mu sync.Mutex
	// content 是下一次 Snapshot rebuild 返回的 YAML。
	content []byte
	// onChange 是 Config Manager 注册的变化信号回调。
	onChange func([]byte)
}

// runtimeSessionProvider 把测试 Client 封装为 Provider；client 为空时表示未启用。
type runtimeSessionProvider struct {
	name   string
	client *runtimeConfigClient
}

// Name 返回测试 Provider 的稳定名称。
func (this *runtimeSessionProvider) Name() string {
	return this.name
}

// Open 创建本次 Runtime 构建使用的 Session。
func (this *runtimeSessionProvider) Open(context.Context, map[string]any) (providerapi.Session, error) {
	if this.client == nil {
		return nil, nil
	}
	return &runtimeConfigSession{client: this.client}, nil
}

// runtimeConfigSource 是 Runtime 测试使用的单个中立 Source。
type runtimeConfigSource struct{}

// Info 返回测试 Source 的稳定元信息。
func (this runtimeConfigSource) Info() providerapi.SourceInfo {
	return providerapi.SourceInfo{Name: "main"}
}

// runtimeConfigSession 适配既有测试 Client 的加载与监听能力。
type runtimeConfigSession struct {
	client *runtimeConfigClient
}

// Sources 返回 Runtime 测试使用的单个 Source。
func (this *runtimeConfigSession) Sources() []providerapi.Source {
	return []providerapi.Source{runtimeConfigSource{}}
}

// Load 拉取当前测试配置内容。
func (this *runtimeConfigSession) Load(ctx context.Context, _ providerapi.Source) ([]byte, error) {
	return this.client.GetConfig(ctx, "g", "d")
}

// Watch 监听测试配置变化并转换为失效通知。
func (this *runtimeConfigSession) Watch(ctx context.Context, _ providerapi.Source, onChange func()) (io.Closer, error) {
	return this.client.WatchConfig(ctx, "g", "d", func(_ []byte) { onChange() })
}

// Close 结束测试 Session；Client 无独立资源需要释放。
func (this *runtimeConfigSession) Close() error {
	return nil
}

// GetConfig 返回当前远端 YAML 副本。
func (this *runtimeConfigClient) GetConfig(context.Context, string, string) ([]byte, error) {
	this.mu.Lock()
	defer this.mu.Unlock()
	return append([]byte{}, this.content...), nil
}

// WatchConfig 保存 Manager 的变化信号回调；测试 client 无独立关闭资源。
func (this *runtimeConfigClient) WatchConfig(_ context.Context, _ string, _ string, onChange func([]byte)) (io.Closer, error) {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.onChange = onChange
	return nil, nil
}

// Emit 替换远端 YAML 并同步触发一次 Manager refresh。
func (this *runtimeConfigClient) Emit(content string) {
	this.mu.Lock()
	this.content = []byte(content)
	onChange := this.onChange
	this.mu.Unlock()
	if onChange != nil {
		onChange([]byte(content))
	}
}

// TestUseCaseConfigBindingInjectsTypedValue 验证聚合 Config 与 Bean Definitions 构成常规 typed config 路径。
//
// 测试方法：声明 Defaults/Bind 和一个简单 bean，Build 后 Resolve bean，断言其 Config 已注入且可直接 Load。
func TestUseCaseConfigBindingInjectsTypedValue(t *testing.T) {
	bean := &runtimeBoundBean{}
	application := New(
		WithConfig(
			config.File(config.FileDisabled()),
			config.Defaults(map[string]any{"service": map[string]any{"name": "orders"}}),
		),
		WithConfigBinding[runtimeBoundConfig]("service"),
		WithBeanDefinitions(ioc.Define(bean)),
	)
	runtime, err := application.Build(context.Background())
	if err != nil {
		t.Fatalf("build runtime failed: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if bean.Config == nil || bean.Config.Load().Name != "orders" {
		t.Fatalf("typed config was not injected: %+v", bean.Config)
	}
}

// TestConfigBindingPullsLatestWithoutSubscriber 验证 IOC typed binding 通过 pull 读取最新 Snapshot。
// 测试方法：构建可触发 watcher 的远端 Plan，刷新到 v2 后调用 Load，断言值更新且 Manager 没有 binding Subscriber。
func TestConfigBindingPullsLatestWithoutSubscriber(t *testing.T) {
	client := &runtimeConfigClient{content: []byte("service:\n  name: v1\n")}
	bean := &runtimeBoundBean{}
	application := New(
		WithConfig(
			config.File(config.FileDisabled()),
			config.Provider(&runtimeSessionProvider{name: "runtime-binding", client: client}),
		),
		WithConfigBinding[runtimeBoundConfig]("service"),
		WithBeanDefinitions(ioc.Define(bean)),
	)
	runtime, err := application.Build(context.Background())
	if err != nil {
		t.Fatalf("build runtime failed: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if status := runtime.ConfigManager().Status(); status.Resources().SubscriptionCount != 0 {
		t.Fatalf("typed binding must not register subscriber, got=%d", status.Resources().SubscriptionCount)
	}

	client.Emit("service:\n  name: v2\n")
	if actual := bean.Config.Load().Name; actual != "v2" {
		t.Fatalf("expected pull binding value v2, got=%q", actual)
	}
}

// TestConfigBindingRunsAllValidators 验证 variadic typed validators 不会静默忽略后续声明。
// 测试方法：声明两个计数 validator 并 Build，断言二者都在首次 Snapshot 发布前执行一次。
func TestConfigBindingRunsAllValidators(t *testing.T) {
	firstCalls := 0
	secondCalls := 0
	application := New(
		WithConfig(
			config.File(config.FileDisabled()),
			config.Defaults(map[string]any{"service": map[string]any{"name": "orders"}}),
		),
		WithConfigBinding("service",
			func(*runtimeBoundConfig) []error {
				firstCalls++
				return nil
			},
			func(*runtimeBoundConfig) []error {
				secondCalls++
				return nil
			},
		),
	)
	runtime, err := application.Build(context.Background())
	if err != nil {
		t.Fatalf("build runtime failed: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("expected both validators once, first=%d second=%d", firstCalls, secondCalls)
	}
}

// TestUseCaseBuildWithInputAppliesSingleRunConfig 验证高级单次输入不会污染 Application 稳定声明。
//
// 测试方法：同一 Application 先用 pinned 输入 Build，再走普通 Build；断言首个值被覆盖、第二个恢复 defaults。
func TestUseCaseBuildWithInputAppliesSingleRunConfig(t *testing.T) {
	application := New(WithConfig(
		config.File(config.FileDisabled()),
		config.Defaults(map[string]any{"service": map[string]any{"name": "default"}}),
	))
	first, err := application.BuildWithInput(context.Background(), BuildInput{Config: config.Input{
		PinnedValues: map[string]any{"service.name": "pinned"},
	}})
	if err != nil {
		t.Fatalf("build runtime with input failed: %v", err)
	}
	var firstConfig runtimeBoundConfig
	if err = first.ConfigManager().Current().Decode("service", &firstConfig); err != nil {
		t.Fatalf("apply first config failed: %v", err)
	}
	_ = first.Close(context.Background())

	second, err := application.Build(context.Background())
	if err != nil {
		t.Fatalf("build runtime without input failed: %v", err)
	}
	t.Cleanup(func() { _ = second.Close(context.Background()) })
	var secondConfig runtimeBoundConfig
	if err = second.ConfigManager().Current().Decode("service", &secondConfig); err != nil {
		t.Fatalf("apply second config failed: %v", err)
	}
	if firstConfig.Name != "pinned" || secondConfig.Name != "default" {
		t.Fatalf("single run input leaked: first=%s second=%s", firstConfig.Name, secondConfig.Name)
	}
}

// TestUseCaseClosePanicReturnsStableError 验证关闭扩展 panic 不会阻塞后续 Close。
//
// 测试方法：日志 Provider.Close 触发 panic，连续调用两次 Runtime.Close；断言两次都返回同一 close_failed 语义。
func TestUseCaseClosePanicReturnsStableError(t *testing.T) {
	provider := &runtimeTestProvider{closePanic: "close panic"}
	runtime, err := New(
		WithConfig(config.File(config.FileDisabled())),
		WithLogging(runtimeTestLogging{provider: provider}),
	).Build(context.Background())
	if err != nil {
		t.Fatalf("build runtime failed: %v", err)
	}
	first := runtime.Close(context.Background())
	second := runtime.Close(context.Background())
	if !errcode.IsCode[ErrorCode](first, ErrCodeRuntimeCloseFailed) || !errcode.IsCode[ErrorCode](second, ErrCodeRuntimeCloseFailed) {
		t.Fatalf("close panic must return stable error: first=%v second=%v", first, second)
	}
	if _, subscribeErr := config.Subscribe[runtimeBoundConfig](runtime.ConfigManager(), "", func(config.TypedUpdate[runtimeBoundConfig]) error { return nil }); subscribeErr == nil {
		t.Fatal("logging close panic must not skip ConfigManager close")
	}
}

// TestUseCaseBlueprintRejectsTypedNilDeclarations 验证聚合能力不会接受 interface 中隐藏的 typed nil。
//
// 测试方法：分别声明 typed nil Bean、Module 和 Logging，调用 Build 并断言三个声明错误码均在资源获取前返回。
func TestUseCaseBlueprintRejectsTypedNilDeclarations(t *testing.T) {
	var bean *runtimeBoundBean
	var module *functionModule
	var logging *runtimeTestLogging
	application := New(
		WithBeanDefinitions(ioc.Define(bean)),
		WithModules(module),
		WithLogging(logging),
	)
	_, err := application.Build(context.Background())
	if !errcode.IsCode[ErrorCode](err, ErrCodeBeanInvalid) ||
		!errcode.IsCode[ErrorCode](err, ErrCodeModuleInvalid) ||
		!errcode.IsCode[ErrorCode](err, ErrCodeLoggingInvalid) {
		t.Fatalf("expected all typed nil declaration errors, got=%v", err)
	}
}
