package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yangzhuangqiu/gobase/ioc"
)

// testWorkload 通过函数槽控制 Workload 的 Run/Shutdown 行为。
type testWorkload struct {
	// name 是声明期稳定名称。
	name string
	// run 是测试注入的主动运行逻辑。
	run func(context.Context, Runtime, CommandContext) error
	// shutdown 是测试注入的停止逻辑。
	shutdown func(context.Context, Runtime, CommandContext) error
}

// workloadOrderResource 验证 IOC Resource Close 只能发生在 Workload Run 收敛之后。
type workloadOrderResource struct {
	// runDone 是 Workload Run 的真实完成点。
	runDone <-chan struct{}
	// closed 在 IOC 调用 Close 时关闭。
	closed chan<- struct{}
}

// Close 检查主动工作完成点后再发布被动资源释放事件。
func (this *workloadOrderResource) Close(context.Context) error {
	select {
	case <-this.runDone:
		close(this.closed)
		return nil
	default:
		return errors.New("resource closed before workload run converged")
	}
}

// Name 返回测试工作负载的稳定名称。
func (this *testWorkload) Name() string {
	return this.name
}

// Run 委托测试注入的主动运行逻辑。
func (this *testWorkload) Run(ctx context.Context, runtime Runtime, command CommandContext) error {
	return this.run(ctx, runtime, command)
}

// Shutdown 委托测试注入的停止逻辑。
func (this *testWorkload) Shutdown(ctx context.Context, runtime Runtime, command CommandContext) error {
	return this.shutdown(ctx, runtime, command)
}

// TestRunnerWrapsOneShotFunction 验证一次性 root 函数无需实现 Shutdown 即可进入统一 Workload 模型。
//
// 测试方法：用 Runner 包装函数，通过 app 执行后断言固定名称为 runner、函数只调用一次且正常关闭 Runtime。
func TestRunnerWrapsOneShotFunction(t *testing.T) {
	called := 0
	workload := Runner(func(context.Context, Runtime, CommandContext) error {
		called++
		return nil
	})
	if workload == nil || workload.Name() != runnerModuleName {
		t.Fatalf("unexpected runner module: %#v", workload)
	}
	application := New(WithModules(workload))
	if err := application.ExecuteContext(context.Background(), nil); err != nil {
		t.Fatalf("execute runner workload: %v", err)
	}
	if called != 1 {
		t.Fatalf("runner must execute once, called=%d", called)
	}
}

// TestWorkloadSupervisorFirstCompletionStopsAndWaitsAll 验证多 Workload 的首个完成会关闭全组并等待全部 Run 收敛。
//
// 测试方法：启动两个受控 Workload，让 first 返回首因、second 只在 Shutdown 后退出；
// 断言 Shutdown 调用两者、等待 second Run，并聚合非触发 Workload 的错误。
func TestWorkloadSupervisorFirstCompletionStopsAndWaitsAll(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	firstReturn := make(chan struct{})
	secondReturn := make(chan struct{})
	firstShutdown := make(chan struct{})
	secondShutdown := make(chan struct{})
	firstCause := errors.New("first stopped")
	secondCause := errors.New("second stopped")
	first := &testWorkload{
		name: "first",
		run: func(context.Context, Runtime, CommandContext) error {
			close(firstStarted)
			<-firstReturn
			return firstCause
		},
		shutdown: func(context.Context, Runtime, CommandContext) error {
			close(firstShutdown)
			return nil
		},
	}
	second := &testWorkload{
		name: "second",
		run: func(context.Context, Runtime, CommandContext) error {
			close(secondStarted)
			<-secondReturn
			return secondCause
		},
		shutdown: func(context.Context, Runtime, CommandContext) error {
			close(secondShutdown)
			close(secondReturn)
			return nil
		},
	}
	supervisor := newWorkloadSupervisor([]Workload{first, second})
	runDone := make(chan error, 1)
	go func() {
		runDone <- supervisor.Run(context.Background(), nil, nil)
	}()
	<-firstStarted
	<-secondStarted
	close(firstReturn)
	if err := <-runDone; !errors.Is(err, firstCause) {
		t.Fatalf("expected first workload cause, got=%v", err)
	}

	shutdownErr := supervisor.Shutdown(context.Background(), nil, nil)
	if !errors.Is(shutdownErr, secondCause) {
		t.Fatalf("expected secondary workload cause, got=%v", shutdownErr)
	}
	select {
	case <-firstShutdown:
	default:
		t.Fatal("expected first workload shutdown")
	}
	select {
	case <-secondShutdown:
	default:
		t.Fatal("expected second workload shutdown")
	}
}

// TestWorkloadSupervisorIgnoresSecondaryCancellation 验证正常关闭不会把其他 Workload 的协作式取消误报为业务失败。
//
// 测试方法：两个 Workload 都等待同一 run context，取消后先取得一个触发结果，再断言 Shutdown 聚合结果为空。
func TestWorkloadSupervisorIgnoresSecondaryCancellation(t *testing.T) {
	started := make(chan struct{}, 2)
	newCanceledWorkload := func(name string) Workload {
		return &testWorkload{
			name: name,
			run: func(ctx context.Context, _ Runtime, _ CommandContext) error {
				started <- struct{}{}
				<-ctx.Done()
				return ctx.Err()
			},
			shutdown: func(context.Context, Runtime, CommandContext) error { return nil },
		}
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	supervisor := newWorkloadSupervisor([]Workload{newCanceledWorkload("first"), newCanceledWorkload("second")})
	runDone := make(chan error, 1)
	go func() { runDone <- supervisor.Run(ctx, nil, nil) }()
	<-started
	<-started
	cancel(errors.New("host stopping"))
	if err := <-runDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected first cancellation result, got=%v", err)
	}
	if err := supervisor.Shutdown(context.Background(), nil, nil); err != nil {
		t.Fatalf("secondary cancellation must not become shutdown failure: %v", err)
	}
}

// TestAppClosesResourceAfterWorkloadRunConverges 验证 app 的主动/被动关闭阶段存在强顺序屏障。
//
// 测试方法：宿主取消后让 Shutdown 立即返回但人为阻塞 Run；断言 IOC Resource 未关闭，
// 再放行 Run 并检查应用返回宿主原因且 Resource 随后完成 Close。
func TestAppClosesResourceAfterWorkloadRunConverges(t *testing.T) {
	hostCtx, cancelHost := context.WithCancelCause(context.Background())
	hostCause := errors.New("host stopping")
	started := make(chan struct{})
	allowRunReturn := make(chan struct{})
	runDone := make(chan struct{})
	shutdownCalled := make(chan struct{})
	resourceClosed := make(chan struct{})
	workload := &testWorkload{
		name: "server",
		run: func(ctx context.Context, _ Runtime, _ CommandContext) error {
			close(started)
			<-ctx.Done()
			<-allowRunReturn
			close(runDone)
			return ctx.Err()
		},
		shutdown: func(context.Context, Runtime, CommandContext) error {
			close(shutdownCalled)
			return nil
		},
	}
	application := New(
		WithBeanDefinitions(ioc.Define(&workloadOrderResource{runDone: runDone, closed: resourceClosed})),
		WithModules(newTestWorkloadModule("server-module", workload)),
	)
	result := make(chan error, 1)
	go func() { result <- application.ExecuteContext(hostCtx, nil) }()
	<-started
	cancelHost(hostCause)
	<-shutdownCalled
	select {
	case <-resourceClosed:
		t.Fatal("resource closed before workload run convergence")
	default:
	}
	select {
	case err := <-result:
		t.Fatalf("app returned before workload run convergence: %v", err)
	default:
	}

	close(allowRunReturn)
	if err := <-result; !errors.Is(err, hostCause) {
		t.Fatalf("expected host cause after graceful convergence, got=%v", err)
	}
	select {
	case <-resourceClosed:
	default:
		t.Fatal("resource was not closed after workload convergence")
	}
}

// TestModuleWorkloadsRejectAmbiguousDeclarations 验证两阶段 Module 会在获取 Runtime 资源前拒绝歧义输入。
//
// 测试方法：分别声明两个固定名称 Runner、跨 Module 重复 Workload 名称和 typed nil，
// 执行命令并检查构建期或收集期错误保留可搜索 reason。
func TestModuleWorkloadsRejectAmbiguousDeclarations(t *testing.T) {
	noop := func(context.Context, Runtime, CommandContext) error { return nil }
	workload := &testWorkload{name: "server", run: noop, shutdown: noop}
	var typedNil *testWorkload
	cases := []struct {
		name   string
		app    *App
		reason string
	}{
		{
			name: "runner duplicate name",
			app: New(WithModules(
				Runner(func(context.Context, Runtime, CommandContext) error { return nil }),
				Runner(func(context.Context, Runtime, CommandContext) error { return nil }),
			)),
			reason: "name_duplicate",
		},
		{
			name: "duplicate name",
			app: New(WithModules(
				newTestWorkloadModule("first", workload),
				newTestWorkloadModule("second", &testWorkload{name: "server", run: noop, shutdown: noop}),
			)),
			reason: "name_duplicate",
		},
		{
			name:   "typed nil",
			app:    New(WithModules(newTestWorkloadModule("typed-nil", typedNil))),
			reason: "workload_is_nil",
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			err := item.app.ExecuteContext(context.Background(), nil)
			if err == nil || !strings.Contains(err.Error(), item.reason) {
				t.Fatalf("expected declaration reason %q, got=%v", item.reason, err)
			}
		})
	}
}

// TestModuleWorkloadsValidateBeforeRuntimeBuild 验证 Workload 声明错误不会触发任何 Module.Register。
//
// 测试方法：声明一个会记录 Register 的 Module，但让 Workloads 返回 typed nil；执行后断言
// 错误来自收集阶段且 Register 计数仍为零，固定“两阶段合同先校验、后 Build”的顺序。
func TestModuleWorkloadsValidateBeforeRuntimeBuild(t *testing.T) {
	registerCount := 0
	var typedNil *testWorkload
	module := &testWorkloadModule{
		name:      "invalid-workloads",
		workloads: []Workload{typedNil},
		register: func(ioc.Registry) error {
			registerCount++
			return nil
		},
	}
	err := New(WithModules(module)).ExecuteContext(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "reason=workload_is_nil") {
		t.Fatalf("expected workload collection failure, got=%v", err)
	}
	if registerCount != 0 {
		t.Fatalf("module registered before workload validation: count=%d", registerCount)
	}
}

// TestShutdownPolicyAppliesUnifiedDefaultsAndOverrides 验证统一 Policy 的默认值、覆盖与重复声明语义。
//
// 测试方法：检查默认策略快照，再应用自定义两个预算，并用第二个 Policy 重复声明验证 fail-fast。
func TestShutdownPolicyAppliesUnifiedDefaultsAndOverrides(t *testing.T) {
	defaults := DefaultShutdownPolicy()
	if len(defaults.Signals) == 0 || defaults.WorkloadGracefulTimeout <= 0 || defaults.RuntimeCloseTimeout <= 0 {
		t.Fatalf("unexpected default shutdown policy: %+v", defaults)
	}
	application := New(
		WithShutdownPolicy(ShutdownPolicy{
			WorkloadGracefulTimeout: 3 * time.Second,
			RuntimeCloseTimeout:     2 * time.Second,
		}),
		WithShutdownPolicy(ShutdownPolicy{WorkloadGracefulTimeout: time.Second}),
	)
	if application.shutdown.WorkloadGracefulTimeout != 3*time.Second || application.shutdown.RuntimeCloseTimeout != 2*time.Second {
		t.Fatalf("unexpected normalized policy: %+v", application.shutdown)
	}
	if application.inputErr == nil || !strings.Contains(application.inputErr.Error(), "duplicate_declaration") {
		t.Fatalf("expected duplicate policy declaration error, got=%v", application.inputErr)
	}
}
