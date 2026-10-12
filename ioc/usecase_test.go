package ioc

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yangzhuangqiu/gobase/errcode"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

type testRepo interface {
	ID() string
}

type testRepoIdentity interface {
	ID() string
}

type mysqlRepo struct{}

func (this *mysqlRepo) ID() string {
	return "mysql"
}

func (this *mysqlRepo) GetName() string {
	return "mysql"
}

type redisRepo struct{}

func (this *redisRepo) ID() string {
	return "redis"
}

func (this *redisRepo) GetName() string {
	return "redis"
}

type singleRepoConsumer struct {
	Repo testRepo
}

type multiRepoConsumer struct {
	Mysql testRepo
	Redis testRepo
}

type optionalRepoConsumer struct {
	Repo testRepo `ioc:"required=false"`
}

type requiredRepoConsumer struct {
	Repo testRepo
}

type testPlugin interface {
	Kind() string
}

type emailPlugin struct{}

func (this *emailPlugin) Kind() string {
	return "email"
}

func (this *emailPlugin) GetName() string {
	return "email"
}

type smsPlugin struct{}

func (this *smsPlugin) Kind() string {
	return "sms"
}

func (this *smsPlugin) GetName() string {
	return "sms"
}

type pluginCollector struct {
	Plugins   []testPlugin
	PluginMap map[string]testPlugin
}

type cycleNodeA struct {
	B *cycleNodeB
}

type cycleNodeB struct {
	A *cycleNodeA
}

type orderBeanA struct {
	Trace *[]string `ioc:"-"`
}

func (this *orderBeanA) Init(_ context.Context) error {
	*this.Trace = append(*this.Trace, "init-a")
	return nil
}

func (this *orderBeanA) Close(_ context.Context) error {
	*this.Trace = append(*this.Trace, "close-a")
	return nil
}

type orderBeanB struct {
	A     *orderBeanA
	Trace *[]string `ioc:"-"`
}

func (this *orderBeanB) Init(_ context.Context) error {
	*this.Trace = append(*this.Trace, "init-b")
	return nil
}

func (this *orderBeanB) Close(_ context.Context) error {
	*this.Trace = append(*this.Trace, "close-b")
	return nil
}

// TestUseCaseRegisterBuildResolve 验证基础注册、构建与单实例解析路径。
//
// 测试方法：
// 1. 注册 repo 与 consumer；
// 2. Build 后断言 consumer 字段已注入；
// 3. 使用 Resolve 校验按接口与按具体类型解析。
func TestUseCaseRegisterBuildResolve(t *testing.T) {
	container := New()
	if err := container.Register(&mysqlRepo{}, WithDeclare[testRepo]()); err != nil {
		t.Fatalf("register repo failed: %v", err)
	}
	consumer := &singleRepoConsumer{}
	if err := container.Register(consumer); err != nil {
		t.Fatalf("register consumer failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if consumer.Repo == nil || consumer.Repo.ID() != "mysql" {
		t.Fatalf("consumer repo inject unexpected, got=%v", consumer.Repo)
	}

	var asInterface testRepo
	if err := container.Resolve(&asInterface); err != nil {
		t.Fatalf("resolve interface failed: %v", err)
	}
	if asInterface.ID() != "mysql" {
		t.Fatalf("resolve interface unexpected id=%s", asInterface.ID())
	}

	var asConcrete *mysqlRepo
	if err := container.Resolve(&asConcrete); err != nil {
		t.Fatalf("resolve concrete failed: %v", err)
	}
	if asConcrete == nil {
		t.Fatalf("resolve concrete should not be nil")
	}
}

// TestUseCaseResolveAllAndByName 验证多实现的全量解析与按名解析能力。
//
// 测试方法：
// 1. 注册两个实现并声明到同一接口；
// 2. Build 后用 ResolveAll 拉取全部实例；
// 3. 使用 ResolveByName 按名称取指定实现。
func TestUseCaseResolveAllAndByName(t *testing.T) {
	container := New()
	if err := container.Register(&mysqlRepo{}, WithName("Mysql"), WithDeclare[testRepo]()); err != nil {
		t.Fatalf("register mysql failed: %v", err)
	}
	if err := container.Register(&redisRepo{}, WithName("Redis"), WithDeclare[testRepo]()); err != nil {
		t.Fatalf("register redis failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	var all []testRepo
	if err := container.ResolveAll(&all); err != nil {
		t.Fatalf("resolve all failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("resolve all size mismatch, got=%d", len(all))
	}

	var byName testRepo
	if err := container.ResolveByName("Redis", &byName); err != nil {
		t.Fatalf("resolve by name failed: %v", err)
	}
	if byName.ID() != "redis" {
		t.Fatalf("resolve by name unexpected id=%s", byName.ID())
	}
}

// TestUseCaseSingleNameAndMultipleDeclares 验证一个 bean 只保留一个名称，但可建立多个声明类型索引。
//
// 测试方法：
// 1. 对同一实例重复设置名称，并通过多个 WithDeclare 分别声明两个接口；
// 2. Build 后断言只有最后设置的名称可解析；
// 3. 分别按两个接口解析，并断言得到同一个原始实例。
func TestUseCaseSingleNameAndMultipleDeclares(t *testing.T) {
	bean := &mysqlRepo{}
	container := New()
	if err := container.Register(
		bean,
		WithName("Ignored"),
		WithName("Primary"),
		WithDeclare[testRepo](),
		WithDeclare[testRepoIdentity](),
	); err != nil {
		t.Fatalf("register bean failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	var byPrimary *mysqlRepo
	if err := container.ResolveByName("Primary", &byPrimary); err != nil {
		t.Fatalf("resolve primary name failed: %v", err)
	}
	if byPrimary != bean {
		t.Fatalf("primary name should resolve original bean")
	}

	var byIgnored *mysqlRepo
	err := container.ResolveByName("Ignored", &byIgnored)
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCResolveNotFound) {
		t.Fatalf("earlier name should be overwritten, got=%v", err)
	}

	var asRepo testRepo
	if err := container.Resolve(&asRepo); err != nil {
		t.Fatalf("resolve testRepo failed: %v", err)
	}
	var asIdentity testRepoIdentity
	if err := container.Resolve(&asIdentity); err != nil {
		t.Fatalf("resolve testRepoIdentity failed: %v", err)
	}
	if asRepo != bean || asIdentity != bean {
		t.Fatalf("all declares should resolve original bean")
	}
}

// TestUseCaseRejectInvalidGenericDeclare 验证泛型声明不会绕过 bean 的类型实现约束。
//
// 测试方法：
// 1. 为不实现 fmt.Stringer 的 bean 声明该接口；
// 2. 调用 Register；
// 3. 断言返回 invalid_declare 错误码，且非法 bean 不进入容器。
func TestUseCaseRejectInvalidGenericDeclare(t *testing.T) {
	container := New()
	err := container.Register(&mysqlRepo{}, WithDeclare[fmt.Stringer]())
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCRegisterInvalidDecl) {
		t.Fatalf("expect invalid declare code, got=%v", err)
	}
	if container.Metrics().RegisterTotal != 0 {
		t.Fatalf("invalid bean should not increase register total")
	}
}

// TestUseCaseInjectMultiByFieldName 验证同类型多实现按字段名匹配注入。
//
// 测试方法：
// 1. 注册两个同接口实现并命名为 Mysql/Redis；
// 2. 注册包含同名字段的 consumer；
// 3. Build 后断言字段绑定到对应实现。
func TestUseCaseInjectMultiByFieldName(t *testing.T) {
	container := New()
	if err := container.Register(&mysqlRepo{}, WithName("Mysql"), WithDeclare[testRepo]()); err != nil {
		t.Fatalf("register mysql failed: %v", err)
	}
	if err := container.Register(&redisRepo{}, WithName("Redis"), WithDeclare[testRepo]()); err != nil {
		t.Fatalf("register redis failed: %v", err)
	}
	consumer := &multiRepoConsumer{}
	if err := container.Register(consumer); err != nil {
		t.Fatalf("register consumer failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if consumer.Mysql == nil || consumer.Mysql.ID() != "mysql" {
		t.Fatalf("mysql inject mismatch, got=%v", consumer.Mysql)
	}
	if consumer.Redis == nil || consumer.Redis.ID() != "redis" {
		t.Fatalf("redis inject mismatch, got=%v", consumer.Redis)
	}
}

// TestUseCaseInjectSliceAndMap 验证切片与 map 聚合注入语义。
//
// 测试方法：
// 1. 注册两个 plugin 实现；
// 2. 为两个实现设置与 GetName 返回值不同的 bean 名称；
// 3. Build 后断言 []interface 注入顺序，且 map key 固定使用 bean 名称。
func TestUseCaseInjectSliceAndMap(t *testing.T) {
	container := New()
	if err := container.Register(&emailPlugin{}, WithName("Email"), WithDeclare[testPlugin]()); err != nil {
		t.Fatalf("register email failed: %v", err)
	}
	if err := container.Register(&smsPlugin{}, WithName("SMS"), WithDeclare[testPlugin]()); err != nil {
		t.Fatalf("register sms failed: %v", err)
	}
	collector := &pluginCollector{}
	if err := container.Register(collector); err != nil {
		t.Fatalf("register collector failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if len(collector.Plugins) != 2 {
		t.Fatalf("plugins inject size mismatch, got=%d", len(collector.Plugins))
	}
	if len(collector.PluginMap) != 2 {
		t.Fatalf("plugin map inject size mismatch, got=%d", len(collector.PluginMap))
	}
	if _, ok := collector.PluginMap["Email"]; !ok {
		t.Fatalf("plugin map missing key=Email")
	}
	if _, ok := collector.PluginMap["SMS"]; !ok {
		t.Fatalf("plugin map missing key=SMS")
	}
	if _, ok := collector.PluginMap["email"]; ok {
		t.Fatalf("plugin map should ignore GetName result")
	}
}

// TestUseCaseOptionalDependency 验证 required=false 可选依赖语义。
//
// 测试方法：
// 1. 仅注册包含可选依赖字段的 consumer；
// 2. Build 不应失败；
// 3. 字段保持 nil。
func TestUseCaseOptionalDependency(t *testing.T) {
	container := New()
	consumer := &optionalRepoConsumer{}
	if err := container.Register(consumer); err != nil {
		t.Fatalf("register consumer failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build should pass for optional dependency, got=%v", err)
	}
	if consumer.Repo != nil {
		t.Fatalf("optional dependency should stay nil, got=%v", consumer.Repo)
	}
}

// TestUseCaseRequiredDependencyMissing 验证缺失必需依赖时会 fail-fast。
//
// 测试方法：
// 1. 注册带必需依赖字段的 consumer；
// 2. Build 预期返回 dependency_not_found；
// 3. 使用错误码断言可分支语义。
func TestUseCaseRequiredDependencyMissing(t *testing.T) {
	container := New()
	if err := container.Register(&requiredRepoConsumer{}); err != nil {
		t.Fatalf("register consumer failed: %v", err)
	}
	err := container.Build(context.Background())
	if err == nil {
		t.Fatalf("build should fail when required dependency missing")
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCBuildDependencyMiss) {
		t.Fatalf("expect dependency miss code, got=%v", err)
	}
}

// TestUseCaseCircularDependency 验证循环依赖会在 Build 阶段被检测。
//
// 测试方法：
// 1. 注册 A->B 与 B->A 两个 bean；
// 2. Build 预期返回 circular_dependency；
// 3. 错误消息包含依赖链路。
func TestUseCaseCircularDependency(t *testing.T) {
	container := New()
	if err := container.Register(&cycleNodeA{}); err != nil {
		t.Fatalf("register A failed: %v", err)
	}
	if err := container.Register(&cycleNodeB{}); err != nil {
		t.Fatalf("register B failed: %v", err)
	}
	err := container.Build(context.Background())
	if err == nil {
		t.Fatalf("build should fail on circular dependency")
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCBuildCircular) {
		t.Fatalf("expect circular code, got=%v", err)
	}
	if !strings.Contains(err.Error(), "cycleNodeA") || !strings.Contains(err.Error(), "cycleNodeB") {
		t.Fatalf("circular message should contain chain, got=%v", err)
	}
}

// TestUseCaseCircularDependencyAllowed 验证弱约束模式下允许循环依赖降级执行。
//
// 测试方法：
// 1. 使用 WithAllowCircularDependency(true) 创建容器并注册 A/B 循环依赖；
// 2. 执行 Build 预期成功；
// 3. 断言 A/B 双向字段都已完成注入。
func TestUseCaseCircularDependencyAllowed(t *testing.T) {
	container := New(WithAllowCircularDependency(true))
	nodeA := &cycleNodeA{}
	nodeB := &cycleNodeB{}
	if err := container.Register(nodeA); err != nil {
		t.Fatalf("register A failed: %v", err)
	}
	if err := container.Register(nodeB); err != nil {
		t.Fatalf("register B failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build should pass in allow circular mode, got=%v", err)
	}
	if nodeA.B == nil || nodeB.A == nil {
		t.Fatalf("circular inject should complete, nodeA.B=%v nodeB.A=%v", nodeA.B, nodeB.A)
	}
	if nodeA.B != nodeB || nodeB.A != nodeA {
		t.Fatalf("circular inject relation mismatch")
	}
}

// TestUseCaseInitAndCloseOrder 验证初始化与关闭顺序符合依赖语义。
//
// 测试方法：
// 1. B 依赖 A，A/B 都实现 Init 与 Close；
// 2. Build 后应先 init-a 再 init-b；
// 3. Close 时应逆序 close-b 再 close-a。
func TestUseCaseInitAndCloseOrder(t *testing.T) {
	trace := make([]string, 0, 4)
	container := New()
	if err := container.Register(&orderBeanA{Trace: &trace}); err != nil {
		t.Fatalf("register A failed: %v", err)
	}
	if err := container.Register(&orderBeanB{Trace: &trace}); err != nil {
		t.Fatalf("register B failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if !slices.Equal(trace, []string{"init-a", "init-b"}) {
		t.Fatalf("init order mismatch, got=%v", trace)
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if !slices.Equal(trace, []string{"init-a", "init-b", "close-b", "close-a"}) {
		t.Fatalf("close order mismatch, got=%v", trace)
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close second time should be nil, got=%v", err)
	}
}

// TestUseCaseResolveAmbiguous 验证 Resolve 在多实现未指定名称时返回歧义错误。
//
// 测试方法：
// 1. 注册两个 testRepo 实现；
// 2. Build 后直接 Resolve(testRepo)；
// 3. 断言返回 ambiguous 错误码。
func TestUseCaseResolveAmbiguous(t *testing.T) {
	container := New()
	if err := container.Register(&mysqlRepo{}, WithName("Mysql"), WithDeclare[testRepo]()); err != nil {
		t.Fatalf("register mysql failed: %v", err)
	}
	if err := container.Register(&redisRepo{}, WithName("Redis"), WithDeclare[testRepo]()); err != nil {
		t.Fatalf("register redis failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	var repo testRepo
	err := container.Resolve(&repo)
	if err == nil {
		t.Fatalf("resolve should fail on ambiguous")
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCResolveAmbiguous) {
		t.Fatalf("expect ambiguous code, got=%v", err)
	}
}

// TestUseCaseBuildAndResolveStateGuard 验证 Build 前解析与 Build 后注册的状态约束。
//
// 测试方法：
// 1. Build 前调用 Resolve，断言 not_built；
// 2. Build 成功后再次 Register，断言状态错误；
// 3. 错误码必须可分支。
func TestUseCaseBuildAndResolveStateGuard(t *testing.T) {
	container := New()
	if err := container.Register(&mysqlRepo{}, WithDeclare[testRepo]()); err != nil {
		t.Fatalf("register mysql failed: %v", err)
	}
	var repo testRepo
	err := container.Resolve(&repo)
	if err == nil {
		t.Fatalf("resolve before build should fail")
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCContainerNotBuilt) {
		t.Fatalf("expect not_built code, got=%v", err)
	}

	if err = container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	err = container.Register(&redisRepo{}, WithDeclare[testRepo]())
	if err == nil {
		t.Fatalf("register after build should fail")
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCInternal) {
		t.Fatalf("expect internal code, got=%v", err)
	}
}

type initFailBean struct{}

func (this *initFailBean) Init(_ context.Context) error {
	return errors.New("init failed")
}

type closeRecorderBean struct {
	Trace *[]string `ioc:"-"`
}

func (this *closeRecorderBean) Close(_ context.Context) error {
	*this.Trace = append(*this.Trace, "rollback-close")
	return nil
}

// TestUseCaseInitFailureSkipsCloserWithoutSuccessfulInit 验证失败回滚不会关闭未成功 Init 的对象。
//
// 测试方法：
// 1. 注册一个只实现 Closer 的 bean 与一个 Init 失败 bean；
// 2. Build 失败后断言 Closer 未被调用；
// 3. 断言错误码为 init_failed，明确回滚清单只包含成功 Initializer。
func TestUseCaseInitFailureSkipsCloserWithoutSuccessfulInit(t *testing.T) {
	trace := make([]string, 0, 1)
	container := New()
	if err := container.Register(&closeRecorderBean{Trace: &trace}); err != nil {
		t.Fatalf("register closer bean failed: %v", err)
	}
	if err := container.Register(&initFailBean{}); err != nil {
		t.Fatalf("register init fail bean failed: %v", err)
	}
	err := container.Build(context.Background())
	if err == nil {
		t.Fatalf("build should fail when init returns error")
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCBuildInitFailed) {
		t.Fatalf("expect init_failed code, got=%v", err)
	}
	if len(trace) != 0 {
		t.Fatalf("rollback close should not run without initialized bean, got=%v", trace)
	}
}

type testContainerLogger struct {
	mu      sync.Mutex
	entries []string
}

var _ baselog.Logger = (*testContainerLogger)(nil)

// Enabled 在测试中启用所有日志级别。
func (this *testContainerLogger) Enabled(context.Context, baselog.Level) bool {
	return true
}

// Log 记录显式级别事件和结构化字段。
func (this *testContainerLogger) Log(_ context.Context, level baselog.Level, msg string, fields ...baselog.Field) {
	entry := fmt.Sprintf("LEVEL=%d %s", level, msg)
	for _, field := range fields {
		entry += fmt.Sprintf(" %s=%v", field.Key, field.Value)
	}
	this.append(entry)
}

// Debug 记录测试调试事件。
func (this *testContainerLogger) Debug(ctx context.Context, msg string, fields ...baselog.Field) {
	this.Log(ctx, baselog.LevelDebug, msg, fields...)
}

// Info 记录测试信息事件。
func (this *testContainerLogger) Info(ctx context.Context, msg string, fields ...baselog.Field) {
	this.Log(ctx, baselog.LevelInfo, msg, fields...)
}

// Warn 记录测试告警事件。
func (this *testContainerLogger) Warn(ctx context.Context, msg string, fields ...baselog.Field) {
	this.Log(ctx, baselog.LevelWarn, msg, fields...)
}

// Error 记录测试失败事件。
func (this *testContainerLogger) Error(ctx context.Context, msg string, fields ...baselog.Field) {
	this.Log(ctx, baselog.LevelError, msg, fields...)
}

// With 在容器日志断言桩中复用接收器；当前测试不覆盖固定字段派生。
func (this *testContainerLogger) With(...baselog.Field) baselog.Logger {
	return this
}

// append 记录一条测试日志，供断言容器日志行为。
func (this *testContainerLogger) append(entry string) {
	this.mu.Lock()
	defer this.mu.Unlock()
	this.entries = append(this.entries, entry)
}

// contains 判断日志集合是否包含目标片段。
func (this *testContainerLogger) contains(fragment string) bool {
	this.mu.Lock()
	defer this.mu.Unlock()
	for _, entry := range this.entries {
		if strings.Contains(entry, fragment) {
			return true
		}
	}
	return false
}

// TestUseCaseOptionalLogger 验证容器可选日志会输出关键生命周期事件。
//
// 测试方法：
// 1. 注入 test logger 创建容器；
// 2. 执行 Register/Build/Close；
// 3. 断言日志包含 build/close 的结果语义。
func TestUseCaseOptionalLogger(t *testing.T) {
	logger := &testContainerLogger{}
	container := New(WithLogger(logger))
	if err := container.Register(&mysqlRepo{}, WithDeclare[testRepo]()); err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if !logger.contains("op=ioc.build result=ok stage=finish") {
		t.Fatalf("expect build success log, logs=%v", logger.entries)
	}
	if !logger.contains("op=ioc.close result=ok stage=finish") {
		t.Fatalf("expect close success log, logs=%v", logger.entries)
	}
}

// TestUseCaseGenericResolveHelpers 验证泛型解析辅助 API 的类型安全语义。
//
// 测试方法：
// 1. 注册并构建单实现 + 多实现场景；
// 2. 分别调用 ResolveAs/ResolveAllAs/ResolveByNameAs；
// 3. 断言返回结果类型与内容符合预期。
func TestUseCaseGenericResolveHelpers(t *testing.T) {
	container := New()
	if err := container.Register(&mysqlRepo{}, WithName("Mysql"), WithDeclare[testRepo]()); err != nil {
		t.Fatalf("register mysql failed: %v", err)
	}
	if err := container.Register(&redisRepo{}, WithName("Redis"), WithDeclare[testRepo]()); err != nil {
		t.Fatalf("register redis failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	byName, err := ResolveByNameAs[testRepo](container, "Mysql")
	if err != nil {
		t.Fatalf("ResolveByNameAs failed: %v", err)
	}
	if byName.ID() != "mysql" {
		t.Fatalf("ResolveByNameAs unexpected id=%s", byName.ID())
	}

	all, err := ResolveAllAs[testRepo](container)
	if err != nil {
		t.Fatalf("ResolveAllAs failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ResolveAllAs size mismatch, got=%d", len(all))
	}

	singleContainer := New()
	if err = singleContainer.Register(&mysqlRepo{}, WithDeclare[testRepo]()); err != nil {
		t.Fatalf("register single mysql failed: %v", err)
	}
	if err = singleContainer.Build(context.Background()); err != nil {
		t.Fatalf("build single container failed: %v", err)
	}
	single, err := ResolveAs[testRepo](singleContainer)
	if err != nil {
		t.Fatalf("ResolveAs failed: %v", err)
	}
	if single.ID() != "mysql" {
		t.Fatalf("ResolveAs unexpected id=%s", single.ID())
	}
}

// TestUseCaseMetricsObserveCircularDegrade 验证循环降级路径的日志与指标可观测性。
//
// 测试方法：
// 1. 启用 allow-circular 并注册循环依赖；
// 2. Build 成功后读取 Metrics 快照；
// 3. 断言降级计数、降级链路、降级顺序和日志事件都可见。
func TestUseCaseMetricsObserveCircularDegrade(t *testing.T) {
	logger := &testContainerLogger{}
	container := New(WithLogger(logger), WithAllowCircularDependency(true))
	if err := container.Register(&cycleNodeA{}); err != nil {
		t.Fatalf("register A failed: %v", err)
	}
	if err := container.Register(&cycleNodeB{}); err != nil {
		t.Fatalf("register B failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build should pass under degraded mode, got=%v", err)
	}
	metrics := container.Metrics()
	if metrics.BuildCircularDegradedTotal != 1 {
		t.Fatalf("expect BuildCircularDegradedTotal=1, got=%d", metrics.BuildCircularDegradedTotal)
	}
	if metrics.LastCircularDegrade == nil {
		t.Fatalf("expect LastCircularDegrade not nil")
	}
	if !strings.Contains(metrics.LastCircularDegrade.Chain, "cycleNodeA") {
		t.Fatalf("expect chain includes cycleNodeA, got=%s", metrics.LastCircularDegrade.Chain)
	}
	if len(metrics.LastCircularDegrade.Order) != 2 {
		t.Fatalf("expect degrade order size=2, got=%d", len(metrics.LastCircularDegrade.Order))
	}
	if !logger.contains("op=ioc.build result=degraded stage=order") {
		t.Fatalf("expect degraded log, logs=%v", logger.entries)
	}
}

type lifecyclePolicyBean struct {
	// Name 是生命周期轨迹中的稳定名称。
	Name string
	// Trace 记录 Init/Close 调用，禁止参与字段注入。
	Trace *[]string `ioc:"-"`
	// Fail 控制 Init 是否返回失败，用于覆盖失败策略。
	Fail bool
	// Dependency 是可选依赖，用于验证 borrowed 对象的字段注入默认值。
	Dependency *borrowedInjectionDependency `ioc:"required=false"`
}

// borrowedInjectionDependency 是 borrowed 注入策略测试使用的无生命周期依赖。
type borrowedInjectionDependency struct{}

type lifecycleOrderDependency struct {
	// Trace 记录生命周期顺序，用于验证依赖拓扑与关闭逆序。
	Trace *[]string `ioc:"-"`
}

// Init 记录依赖 bean 初始化动作，承担生命周期顺序测试的上游阶段。
func (this *lifecycleOrderDependency) Init(context.Context) error {
	*this.Trace = append(*this.Trace, "init-dependency")
	return nil
}

// Close 记录依赖 bean 关闭动作，验证其晚于消费者释放。
func (this *lifecycleOrderDependency) Close(context.Context) error {
	*this.Trace = append(*this.Trace, "close-dependency")
	return nil
}

type lifecycleOrderConsumer struct {
	// Dependency 是消费者的必填上游依赖，用于驱动真实 bean 拓扑。
	Dependency *lifecycleOrderDependency
	// Trace 记录生命周期顺序，不参与依赖注入。
	Trace *[]string `ioc:"-"`
}

// Init 记录消费者初始化动作，验证其晚于依赖初始化。
func (this *lifecycleOrderConsumer) Init(context.Context) error {
	*this.Trace = append(*this.Trace, "init-consumer")
	return nil
}

// Close 记录消费者关闭动作，验证其早于依赖释放。
func (this *lifecycleOrderConsumer) Close(context.Context) error {
	*this.Trace = append(*this.Trace, "close-consumer")
	return nil
}

// TestUseCaseCloseUsesReverseInitOrder 验证 Close 使用真实初始化拓扑逆序，而不是注册逆序。
//
// 测试方法：
// 1. 先注册消费者、后注册依赖，使注册顺序与依赖拓扑相反；
// 2. Build 后调用 Close；
// 3. 断言依赖先初始化、消费者先关闭。
func TestUseCaseCloseUsesReverseInitOrder(t *testing.T) {
	trace := make([]string, 0, 4)
	container := New()
	if err := container.Register(&lifecycleOrderConsumer{Trace: &trace}); err != nil {
		t.Fatalf("register consumer failed: %v", err)
	}
	if err := container.Register(&lifecycleOrderDependency{Trace: &trace}); err != nil {
		t.Fatalf("register dependency failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if !slices.Equal(trace, []string{"init-dependency", "init-consumer", "close-consumer", "close-dependency"}) {
		t.Fatalf("unexpected lifecycle order: %v", trace)
	}
}

// TestUseCaseBorrowedBeanSkipsManagedLifecycle 验证借入 bean 默认只参与解析，不注入字段且不由容器 Init/Close。
//
// 测试方法：
// 1. 注册可注入依赖及实现 Initializer/Closer 的 borrowed bean；
// 2. 执行 Build 与 Close；
// 3. 断言依赖字段和生命周期轨迹均为空，但 bean 仍可正常解析。
func TestUseCaseBorrowedBeanSkipsManagedLifecycle(t *testing.T) {
	trace := make([]string, 0, 2)
	bean := &lifecyclePolicyBean{Name: "borrowed", Trace: &trace}
	container := New()
	if err := container.Register(&borrowedInjectionDependency{}); err != nil {
		t.Fatalf("register borrowed dependency failed: %v", err)
	}
	if err := container.Register(bean, WithName("BorrowedLifecycle"), WithBorrowed()); err != nil {
		t.Fatalf("register borrowed bean failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	var resolved *lifecyclePolicyBean
	if err := container.ResolveByName("BorrowedLifecycle", &resolved); err != nil {
		t.Fatalf("resolve borrowed bean failed: %v", err)
	}
	if resolved != bean {
		t.Fatalf("resolved borrowed bean mismatch")
	}
	if bean.Dependency != nil {
		t.Fatalf("borrowed bean must skip field injection by default")
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close failed: %v", err)
	}
	if len(trace) != 0 {
		t.Fatalf("borrowed bean lifecycle must not run, got=%v", trace)
	}
}

// TestUseCaseBorrowedBeanAllowsExplicitInjection 验证 borrowed bean 可显式恢复字段注入且不受 Option 顺序影响。
//
// 测试方法：
// 1. 分别以 borrowed→inject 和 inject→borrowed 顺序注册 borrowed bean；
// 2. 两种顺序都显式设置 WithInject(true) 并执行 Build；
// 3. 断言两者均注入同一个依赖，但生命周期仍由外部负责。
func TestUseCaseBorrowedBeanAllowsExplicitInjection(t *testing.T) {
	testCases := []struct {
		// name 是子场景名称，用于定位 Option 顺序差异。
		name string
		// options 是当前场景使用的注册选项顺序。
		options []RegisterOption
	}{
		{name: "borrowed_then_inject", options: []RegisterOption{WithBorrowed(), WithInject(true)}},
		{name: "inject_then_borrowed", options: []RegisterOption{WithInject(true), WithBorrowed()}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// 阶段一：注册唯一依赖与显式启用注入的 borrowed bean。
			dependency := &borrowedInjectionDependency{}
			trace := make([]string, 0, 2)
			bean := &lifecyclePolicyBean{Name: testCase.name, Trace: &trace}
			container := New()
			if err := container.Register(dependency); err != nil {
				t.Fatalf("register borrowed dependency failed: %v", err)
			}
			if err := container.Register(bean, testCase.options...); err != nil {
				t.Fatalf("register borrowed bean failed: %v", err)
			}

			// 阶段二：Build 应注入字段，但跳过 borrowed bean 的 Init。
			if err := container.Build(context.Background()); err != nil {
				t.Fatalf("build failed: %v", err)
			}
			if bean.Dependency != dependency {
				t.Fatalf("explicit injection mismatch: got=%p want=%p", bean.Dependency, dependency)
			}
			if len(trace) != 0 {
				t.Fatalf("borrowed bean lifecycle must remain external, got=%v", trace)
			}
			if err := container.Close(context.Background()); err != nil {
				t.Fatalf("close failed: %v", err)
			}
		})
	}
}

// TestUseCaseCloseBeforeBuildSkipsUninitializedBeans 验证未成功 Build 的 bean 不进入正常关闭清单。
//
// 测试方法：
// 1. 注册实现 Closer 的 managed bean，但不调用 Build；
// 2. 直接关闭容器；
// 3. 断言 bean Close 未执行，约束 Register 阶段不得提前获取资源。
func TestUseCaseCloseBeforeBuildSkipsUninitializedBeans(t *testing.T) {
	trace := make([]string, 0, 1)
	container := New()
	if err := container.Register(&lifecyclePolicyBean{Name: "created", Trace: &trace}); err != nil {
		t.Fatalf("register bean failed: %v", err)
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("close created container failed: %v", err)
	}
	if len(trace) != 0 {
		t.Fatalf("uninitialized bean must not close, got=%v", trace)
	}
}

// Init 记录生命周期策略测试的初始化动作，并按场景返回预设失败。
func (this *lifecyclePolicyBean) Init(_ context.Context) error {
	*this.Trace = append(*this.Trace, "init-"+this.Name)
	if this.Fail {
		return errors.New("init failed:" + this.Name)
	}
	return nil
}

// Close 记录生命周期策略测试的资源回收动作。
func (this *lifecyclePolicyBean) Close(_ context.Context) error {
	*this.Trace = append(*this.Trace, "close-"+this.Name)
	return nil
}

// TestUseCaseInitFailureStopsAndRollsBack 验证 Init 返回错误时固定首错停止并逆序回滚。
//
// 测试方法：
// 1. 依次注册成功、失败和不应执行的三个 bean；
// 2. 调用 Build 并断言返回 init_failed；
// 3. 断言失败后的 Init 未执行，成功项已逆序 Close，指标记录一次失败。
func TestUseCaseInitFailureStopsAndRollsBack(t *testing.T) {
	trace := make([]string, 0, 4)
	container := New()
	if err := container.Register(&lifecyclePolicyBean{Name: "ok", Trace: &trace}, WithName("LifecycleOK")); err != nil {
		t.Fatalf("register ok bean failed: %v", err)
	}
	if err := container.Register(&lifecyclePolicyBean{Name: "fail", Trace: &trace, Fail: true}, WithName("LifecycleFail")); err != nil {
		t.Fatalf("register fail bean failed: %v", err)
	}
	if err := container.Register(&lifecyclePolicyBean{Name: "skipped", Trace: &trace}, WithName("LifecycleSkipped")); err != nil {
		t.Fatalf("register skipped bean failed: %v", err)
	}
	err := container.Build(context.Background())
	if err == nil {
		t.Fatalf("build should fail when init has error")
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCBuildInitFailed) {
		t.Fatalf("expect init failed code, got=%v", err)
	}
	if !slices.Equal(trace, []string{"init-ok", "init-fail", "close-ok"}) {
		t.Fatalf("unexpected fixed stop trace, got=%v", trace)
	}
	metrics := container.Metrics()
	if metrics.InitFailedTotal != 1 || metrics.BuildFailedTotal != 1 {
		t.Fatalf("unexpected init/build failure metrics: %+v", metrics)
	}
}

type rollbackCloseFailBean struct{}

// Init 表示该 bean 已成功进入回滚清单。
func (this *rollbackCloseFailBean) Init(context.Context) error {
	return nil
}

// Close 模拟 Init 失败回滚阶段的资源关闭错误。
func (this *rollbackCloseFailBean) Close(context.Context) error {
	return errors.New("rollback close failed")
}

// TestUseCaseInitFailureAggregatesRollbackError 验证原始 Init 错误与回滚 Close 错误同时保留。
//
// 测试方法：
// 1. 注册成功 Init 但 Close 失败的 bean，再注册 Init 失败 bean；
// 2. 调用 Build；
// 3. 断言结果同时可按 init_failed 和 close_failed 分支。
func TestUseCaseInitFailureAggregatesRollbackError(t *testing.T) {
	container := New()
	if err := container.Register(&rollbackCloseFailBean{}); err != nil {
		t.Fatalf("register rollback close fail bean failed: %v", err)
	}
	if err := container.Register(&initFailBean{}); err != nil {
		t.Fatalf("register init fail bean failed: %v", err)
	}

	err := container.Build(context.Background())
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCBuildInitFailed) {
		t.Fatalf("expect init_failed code, got=%v", err)
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCCloseFailed) {
		t.Fatalf("expect close_failed rollback cause, got=%v", err)
	}
}

type lifecyclePanicBean struct {
	Trace *[]string `ioc:"-"`
}

// Init 记录异常初始化动作后触发业务 panic。
func (this *lifecyclePanicBean) Init(context.Context) error {
	*this.Trace = append(*this.Trace, "init-panic")
	panic("unexpected init panic")
}

// TestUseCaseInitPanicRollsBackAndRepanics 验证意外 Init panic 会先回滚再继续 panic。
//
// 测试方法：
// 1. 注册成功 bean、panic bean 和不应执行的 bean；
// 2. recover Build panic 并断言被归一化为 init_failed；
// 3. 断言成功项已关闭、后续 Init 未执行、容器恢复后允许继续 Register。
func TestUseCaseInitPanicRollsBackAndRepanics(t *testing.T) {
	trace := make([]string, 0, 4)
	container := New()
	if err := container.Register(&lifecyclePolicyBean{Name: "ok", Trace: &trace}, WithName("LifecyclePanicOK")); err != nil {
		t.Fatalf("register ok bean failed: %v", err)
	}
	if err := container.Register(&lifecyclePanicBean{Trace: &trace}, WithName("LifecyclePanic")); err != nil {
		t.Fatalf("register panic bean failed: %v", err)
	}
	if err := container.Register(&lifecyclePolicyBean{Name: "skipped", Trace: &trace}, WithName("LifecyclePanicSkipped")); err != nil {
		t.Fatalf("register skipped bean failed: %v", err)
	}

	var recovered any
	func() {
		defer func() {
			recovered = recover()
		}()
		_ = container.Build(context.Background())
	}()
	if recovered == nil {
		t.Fatal("build should preserve unexpected Init panic")
	}
	recoveredErr, ok := recovered.(error)
	if !ok || !errcode.IsCode[ErrorCode](recoveredErr, ErrCodeIOCBuildInitFailed) {
		t.Fatalf("expect init_failed panic error, got=%T %v", recovered, recovered)
	}
	if !slices.Equal(trace, []string{"init-ok", "init-panic", "close-ok"}) {
		t.Fatalf("unexpected panic rollback trace, got=%v", trace)
	}
	metrics := container.Metrics()
	if metrics.InitFailedTotal != 1 || metrics.BuildFailedTotal != 1 {
		t.Fatalf("unexpected panic failure metrics: %+v", metrics)
	}
	if err := container.Register(&mysqlRepo{}, WithName("AfterPanic")); err != nil {
		t.Fatalf("container should return to created after panic, got=%v", err)
	}
}

type closeFailBean struct {
	// Name 是当前关闭节点名称，用于断言逆序执行轨迹。
	Name string
	// Trace 收集 Closer 调用顺序，不参与依赖注入。
	Trace *[]string `ioc:"-"`
	// Err 是本节点返回的稳定失败原因，用于验证聚合错误链。
	Err error `ioc:"-"`
}

// Close 记录当前节点并返回固定 cause，用于验证完整遍历和错误聚合。
func (this *closeFailBean) Close(_ context.Context) error {
	*this.Trace = append(*this.Trace, "close-"+this.Name)
	return this.Err
}

// TestUseCaseCloseAlwaysContinuesAndAggregates 验证 Close 固定遍历全部 Closer 并聚合错误。
//
// 测试方法：
// 1. 注册两个返回不同 cause 的 Closer；
// 2. 调用 Close；
// 3. 断言按逆序执行全部节点，返回错误可匹配两个 cause。
func TestUseCaseCloseAlwaysContinuesAndAggregates(t *testing.T) {
	// 阶段一：注册两个具有独立 cause 的 Closer，构造可验证的逆序清单。
	trace := make([]string, 0, 2)
	firstCause := errors.New("close failed:first")
	secondCause := errors.New("close failed:second")
	container := New()
	if err := container.Register(&closeFailBean{Name: "first", Trace: &trace, Err: firstCause}, WithName("CloseFirst")); err != nil {
		t.Fatalf("register first failed: %v", err)
	}
	if err := container.Register(&closeFailBean{Name: "second", Trace: &trace, Err: secondCause}, WithName("CloseSecond")); err != nil {
		t.Fatalf("register second failed: %v", err)
	}
	// 阶段二：完成 Build 后执行唯一关闭事务。
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	err := container.Close(context.Background())
	if err == nil {
		t.Fatalf("close should fail")
	}
	// 阶段三：验证遍历完整性和 errors.Join 的 cause 保留语义。
	if !slices.Equal(trace, []string{"close-second", "close-first"}) {
		t.Fatalf("unexpected close trace, got=%v", trace)
	}
	if !errors.Is(err, firstCause) || !errors.Is(err, secondCause) {
		t.Fatalf("close error must aggregate every cause, got=%v", err)
	}
}

type closeBlockingBean struct {
	release <-chan struct{}
	Trace   *[]string `ioc:"-"`
}

func (this *closeBlockingBean) Close(_ context.Context) error {
	<-this.release
	*this.Trace = append(*this.Trace, "close-blocking")
	return nil
}

type closeFastBean struct {
	Trace *[]string `ioc:"-"`
}

func (this *closeFastBean) Close(_ context.Context) error {
	*this.Trace = append(*this.Trace, "close-fast")
	return nil
}

type initContextAwareBean struct{}

func (this *initContextAwareBean) Init(ctx context.Context) error {
	return ctx.Err()
}

type closeContextAwareBean struct{}

func (this *closeContextAwareBean) Close(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// TestUseCaseCloseTimeout 验证 Close 超时策略会返回超时错误并记录指标。
//
// 测试方法：
// 1. 配置 CloseTimeout 和 continue 策略；
// 2. 注册一个阻塞 Closer 和一个快速 Closer；
// 3. 断言 Close 返回超时错误，且指标记录 timeout 次数。
func TestUseCaseCloseTimeout(t *testing.T) {
	// 阶段一：注册快速与阻塞 Closer，并配置单 bean timeout。
	trace := make([]string, 0, 2)
	release := make(chan struct{})
	container := New(
		WithCloseTimeout(20 * time.Millisecond),
	)
	if err := container.Register(&closeFastBean{Trace: &trace}); err != nil {
		t.Fatalf("register fast bean failed: %v", err)
	}
	if err := container.Register(&closeBlockingBean{release: release, Trace: &trace}); err != nil {
		t.Fatalf("register blocking bean failed: %v", err)
	}
	// 阶段二：Build 后执行 Close，阻塞节点应由 timeout 结束等待。
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}
	err := container.Close(context.Background())
	close(release)
	if err == nil {
		t.Fatalf("close should fail with timeout")
	}
	// 阶段三：验证 timeout 和 bean 失败指标分别累计。
	metrics := container.Metrics()
	if metrics.CloseTimeoutTotal != 1 {
		t.Fatalf("expect CloseTimeoutTotal=1, got=%d", metrics.CloseTimeoutTotal)
	}
	if metrics.CloseFailedTotal == 0 {
		t.Fatalf("expect CloseFailedTotal > 0, got=%d", metrics.CloseFailedTotal)
	}
}

// TestUseCaseBuildUseContext 验证 Build 会把调用上下文传递给 Initializer。
//
// 测试方法：
// 1. 注册一个读取 ctx.Err 的 Initializer；
// 2. 用已取消 context 调用 Build；
// 3. 断言 Build 返回 init_failed 语义错误。
func TestUseCaseBuildUseContext(t *testing.T) {
	container := New()
	if err := container.Register(&initContextAwareBean{}); err != nil {
		t.Fatalf("register init context aware bean failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := container.Build(ctx)
	if err == nil {
		t.Fatalf("build should fail when initializer receives canceled context")
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCBuildInitFailed) {
		t.Fatalf("expect init_failed code, got=%v", err)
	}
}

// TestUseCaseCloseUseContext 验证 Close 会把调用上下文传递给 Closer。
//
// 测试方法：
// 1. 注册一个等待 ctx.Done 的 Closer；
// 2. Build 后用已取消 context 调用 Close；
// 3. 断言 Close 返回 close_failed 且原因为 close_canceled。
func TestUseCaseCloseUseContext(t *testing.T) {
	container := New()
	if err := container.Register(&closeContextAwareBean{}); err != nil {
		t.Fatalf("register close context aware bean failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := container.Close(ctx)
	if err == nil {
		t.Fatalf("close should fail when closer receives canceled context")
	}
	if !errcode.IsCode[ErrorCode](err, ErrCodeIOCCloseFailed) {
		t.Fatalf("expect close_failed code, got=%v", err)
	}
	if !strings.Contains(err.Error(), "reason=close_canceled") {
		t.Fatalf("expect close_canceled reason, got=%v", err)
	}
}

type sharedCloseBean struct {
	// calls 记录底层 Closer 实际执行次数，验证共享事务至多执行一次。
	calls atomic.Int32
	// entered 通知测试 Closer 已进入阻塞阶段。
	entered chan<- struct{}
	// release 控制 Closer 何时返回，保证并发 waiter 能稳定加入。
	release <-chan struct{}
	// err 是关闭事务应向全部调用者共享的稳定结果。
	err error
}

// Close 进入可控阻塞点并返回固定结果，用于验证并发关闭事务。
func (this *sharedCloseBean) Close(context.Context) error {
	this.calls.Add(1)
	this.entered <- struct{}{}
	<-this.release
	return this.err
}

// TestUseCaseConcurrentCloseSharesFirstResult 验证并发和重复 Close 共享首次事务结果。
//
// 测试方法：
// 1. 用阻塞 Closer 保持 executor 处于 closing；
// 2. 启动 waiter 并验证 closing 阶段拒绝其他操作；
// 3. 释放 executor，断言 Closer 只执行一次且所有 Close 返回同一 error。
func TestUseCaseConcurrentCloseSharesFirstResult(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	cause := errors.New("shared close failed")
	bean := &sharedCloseBean{entered: entered, release: release, err: cause}
	container := New()
	if err := container.Register(bean); err != nil {
		t.Fatalf("register shared close bean failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// 阶段一：首个调用成为 executor，并阻塞在底层 Closer。
	firstResult := make(chan error, 1)
	go func() { firstResult <- container.Close(context.Background()) }()
	<-entered

	// 阶段二：并发调用加入共享事务，指标确认它已经进入 waiter 分支。
	secondResult := make(chan error, 1)
	go func() { secondResult <- container.Close(context.Background()) }()
	deadline := time.Now().Add(time.Second)
	for container.Metrics().CloseJoinedTotal != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if container.Metrics().CloseJoinedTotal != 1 {
		t.Fatalf("concurrent close did not join shared transaction")
	}
	select {
	case err := <-secondResult:
		t.Fatalf("waiter returned before executor completed: %v", err)
	default:
	}

	// closing 是不可逆终态过渡，注册、构建和解析统一按 container.closed 拒绝。
	if err := container.Register(&struct{}{}); !errcode.IsCode[ErrorCode](err, ErrCodeIOCContainerClosed) || !strings.Contains(err.Error(), "reason=close_running") {
		t.Fatalf("register during closing must return close_running, got=%v", err)
	}
	if err := container.Build(context.Background()); !errcode.IsCode[ErrorCode](err, ErrCodeIOCContainerClosed) || !strings.Contains(err.Error(), "reason=close_running") {
		t.Fatalf("build during closing must return close_running, got=%v", err)
	}
	var resolved *sharedCloseBean
	if err := container.Resolve(&resolved); !errcode.IsCode[ErrorCode](err, ErrCodeIOCContainerClosed) || !strings.Contains(err.Error(), "reason=close_running") {
		t.Fatalf("resolve during closing must return close_running, got=%v", err)
	}

	// 阶段三：executor 发布结果后，waiter 与后续重复调用稳定读取同一个 error。
	close(release)
	firstErr := <-firstResult
	secondErr := <-secondResult
	repeatedErr := container.Close(context.Background())
	if firstErr == nil || !errors.Is(firstErr, cause) {
		t.Fatalf("first close must preserve cause, got=%v", firstErr)
	}
	if firstErr != secondErr || firstErr != repeatedErr {
		t.Fatalf("all close calls must share first result: first=%v second=%v repeated=%v", firstErr, secondErr, repeatedErr)
	}
	if bean.calls.Load() != 1 {
		t.Fatalf("closer must execute once, got=%d", bean.calls.Load())
	}
	metrics := container.Metrics()
	if metrics.CloseTotal != 1 || metrics.CloseJoinedTotal != 1 {
		t.Fatalf("unexpected close transaction metrics: %+v", metrics)
	}
}

// TestUseCaseCloseWaiterContextOnlyCancelsWaiting 验证 waiter context 不会取消共享关闭事务。
//
// 测试方法：
// 1. 启动一个阻塞中的 Close executor；
// 2. 使用已取消 context 加入关闭事务；
// 3. 断言 waiter 独立失败，executor 释放后仍成功发布共享结果。
func TestUseCaseCloseWaiterContextOnlyCancelsWaiting(t *testing.T) {
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	bean := &sharedCloseBean{entered: entered, release: release}
	container := New()
	if err := container.Register(bean); err != nil {
		t.Fatalf("register shared close bean failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// 阶段一：executor 使用独立 context 执行真实关闭。
	executorResult := make(chan error, 1)
	go func() { executorResult <- container.Close(context.Background()) }()
	<-entered

	// 阶段二：waiter 取消只结束自身等待，并产生可分支的 wait_canceled。
	waitCtx, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	waitErr := container.Close(waitCtx)
	if !errcode.IsCode[ErrorCode](waitErr, ErrCodeIOCCloseFailed) || !strings.Contains(waitErr.Error(), "reason=wait_canceled") {
		t.Fatalf("waiter must return wait_canceled, got=%v", waitErr)
	}
	select {
	case err := <-executorResult:
		t.Fatalf("waiter cancellation must not finish executor: %v", err)
	default:
	}

	// 阶段三：释放 executor 后共享事务成功，重复 Close 返回首次 nil 结果。
	close(release)
	if err := <-executorResult; err != nil {
		t.Fatalf("executor close failed: %v", err)
	}
	if err := container.Close(context.Background()); err != nil {
		t.Fatalf("repeated close must return shared nil result: %v", err)
	}
	metrics := container.Metrics()
	if metrics.CloseJoinedTotal != 1 || metrics.CloseWaitCanceledTotal != 1 {
		t.Fatalf("unexpected waiter metrics: %+v", metrics)
	}
}

type closePanicBean struct {
	// Trace 收集 panic Closer 与后续 Closer 的执行顺序。
	Trace *[]string `ioc:"-"`
}

// Close 记录执行后触发 panic，用于验证关闭异常边界。
func (this *closePanicBean) Close(context.Context) error {
	*this.Trace = append(*this.Trace, "close-panic")
	panic("close panic")
}

// TestUseCaseClosePanicBecomesSharedErrorAndContinues 验证 Closer panic 可聚合且不阻断剩余资源。
//
// 测试方法：
// 1. 让 panic Closer 在快速 Closer 前逆序执行；
// 2. 调用 Close 并读取重复 Close；
// 3. 断言 panic 转为共享错误、后续 Closer 已执行且指标累计一次。
func TestUseCaseClosePanicBecomesSharedErrorAndContinues(t *testing.T) {
	trace := make([]string, 0, 2)
	container := New()
	if err := container.Register(&closeFastBean{Trace: &trace}, WithName("FastAfterPanic")); err != nil {
		t.Fatalf("register fast closer failed: %v", err)
	}
	if err := container.Register(&closePanicBean{Trace: &trace}, WithName("PanicCloser")); err != nil {
		t.Fatalf("register panic closer failed: %v", err)
	}
	if err := container.Build(context.Background()); err != nil {
		t.Fatalf("build failed: %v", err)
	}

	// Close 必须恢复 panic、继续逆序遍历，并把完整结果发布给重复调用。
	closeErr := container.Close(context.Background())
	repeatedErr := container.Close(context.Background())
	if !errcode.IsCode[ErrorCode](closeErr, ErrCodeIOCCloseFailed) || !strings.Contains(closeErr.Error(), "reason=close_panic") {
		t.Fatalf("panic must become close_failed, got=%v", closeErr)
	}
	if closeErr != repeatedErr {
		t.Fatalf("repeated close must return shared panic result")
	}
	if !slices.Equal(trace, []string{"close-panic", "close-fast"}) {
		t.Fatalf("close must continue after panic, got=%v", trace)
	}
	metrics := container.Metrics()
	if metrics.ClosePanicTotal != 1 || metrics.CloseFailedTotal != 1 {
		t.Fatalf("unexpected panic metrics: %+v", metrics)
	}
}
