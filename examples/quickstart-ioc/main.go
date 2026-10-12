package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/yangzhuangqiu/gobase/ioc"
	baselog "github.com/yangzhuangqiu/gobase/log"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
)

const (
	quickstartIOCModeEnv       = "QUICKSTART_IOC_MODE"
	quickstartIOCRunSecondsEnv = "QUICKSTART_IOC_RUN_SECONDS"
	defaultRunSeconds          = 20
)

type greeter interface {
	Greet(name string) string
}

type primaryGreeter struct{}

func (this *primaryGreeter) Greet(name string) string {
	return "primary hello, " + name
}

type backupGreeter struct{}

func (this *backupGreeter) Greet(name string) string {
	return "backup hello, " + name
}

type greetService struct {
	// 多实现按字段名分流：字段名 Primary 对应 bean 名称 Primary。
	Primary greeter
	// 切片字段会聚合同声明类型的全部实现。
	All []greeter
	// map 字段按 bean 名称聚合实例。
	AllMap map[string]greeter
}

type cycleNodeA struct {
	B *cycleNodeB
}

type cycleNodeB struct {
	A *cycleNodeA
}

type appConfig struct {
	RunSeconds int
}

type taskRepository struct{}

func (this *taskRepository) FetchPendingTasks() []string {
	return []string{"email:welcome", "email:invoice"}
}

type taskService struct {
	Logger baselog.Logger
	Repo   *taskRepository
}

func (this *taskService) ProcessOnce() {
	tasks := this.Repo.FetchPendingTasks()
	this.Logger.Info(nil, "pending tasks fetched",
		baselog.Op("quickstart_ioc.prod.process"), baselog.Result(baselog.ResultOK),
		baselog.F("stage", "fetch"), baselog.F("count", len(tasks)),
	)
	for _, task := range tasks {
		this.Logger.Info(nil, "task sent",
			baselog.Op("quickstart_ioc.prod.process"), baselog.Result(baselog.ResultOK),
			baselog.F("stage", "send"), baselog.F("task", task),
		)
	}
}

type worker struct {
	Logger  baselog.Logger
	Service *taskService
	stopCh  chan struct{}
}

func (this *worker) Init(ctx context.Context) error {
	this.stopCh = make(chan struct{})
	this.Logger.Info(ctx, "worker initialized",
		baselog.Op("quickstart_ioc.prod.worker_init"), baselog.Result(baselog.ResultOK),
	)
	return nil
}

func (this *worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-this.stopCh:
			return nil
		case <-ticker.C:
			this.Service.ProcessOnce()
		}
	}
}

func (this *worker) Close(_ context.Context) error {
	if this.stopCh == nil {
		return nil
	}
	close(this.stopCh)
	return nil
}

func main() {
	mode := strings.TrimSpace(strings.ToLower(os.Getenv(quickstartIOCModeEnv)))
	if mode == "" {
		mode = "strict"
	}

	var err error
	switch mode {
	case "strict":
		err = runStrictMode()
	case "allow-circular":
		err = runAllowCircularMode()
	case "prod":
		err = runProductionMode()
	default:
		err = fmt.Errorf("invalid %s=%s, supported: strict|allow-circular|prod", quickstartIOCModeEnv, mode)
	}
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "quickstart-ioc run failed: %v\n", err)
		os.Exit(1)
	}
}

// runStrictMode 演示默认强语义：多实现注入 + 解析能力。
func runStrictMode() error {
	logger := baselog.NewLoggerFactory(nil).Get("quickstart-ioc.strict")
	container := ioc.New(ioc.WithLogger(logger))

	if err := container.Register(&primaryGreeter{}, ioc.WithName("Primary"), ioc.WithDeclare[greeter]()); err != nil {
		return err
	}
	if err := container.Register(&backupGreeter{}, ioc.WithName("Backup"), ioc.WithDeclare[greeter]()); err != nil {
		return err
	}

	service := &greetService{}
	if err := container.Register(service); err != nil {
		return err
	}
	if err := container.Build(context.Background()); err != nil {
		return err
	}
	defer func() {
		_ = container.Close(context.Background())
	}()

	fmt.Println(service.Primary.Greet("gobase"))
	fmt.Printf("all greeter count: %d\n", len(service.All))
	fmt.Printf("all map keys: %v\n", sortedMapKeys(service.AllMap))

	var byName greeter
	if err := container.ResolveByName("Backup", &byName); err != nil {
		return err
	}
	fmt.Println(byName.Greet("gobase"))
	return nil
}

// runAllowCircularMode 演示弱约束模式：允许循环依赖降级执行。
func runAllowCircularMode() error {
	logger := baselog.NewLoggerFactory(nil).Get("quickstart-ioc.allow-circular")
	container := ioc.New(
		ioc.WithLogger(logger),
		ioc.WithAllowCircularDependency(true),
	)
	nodeA := &cycleNodeA{}
	nodeB := &cycleNodeB{}
	if err := container.Register(nodeA); err != nil {
		return err
	}
	if err := container.Register(nodeB); err != nil {
		return err
	}
	if err := container.Build(context.Background()); err != nil {
		return err
	}
	defer func() {
		_ = container.Close(context.Background())
	}()
	fmt.Printf("circular injected: nodeA.B!=nil(%t), nodeB.A!=nil(%t)\n", nodeA.B != nil, nodeB.A != nil)
	return nil
}

// runProductionMode 演示多模块装配（log-zap + log + ioc）及优雅关闭流程。
func runProductionMode() error {
	config, err := loadRunConfig()
	if err != nil {
		return err
	}

	provider, err := logzap.NewProvider(logzap.Config{
		Level:  "info",
		Mode:   "console",
		Format: "text",
	})
	if err != nil {
		return err
	}
	factory := provider.Factory()
	rootLogger := factory.Get("quickstart-ioc.prod")
	containerLogger := factory.Get("quickstart-ioc.prod.ioc")

	container := ioc.New(
		ioc.WithLogger(containerLogger),
		ioc.WithCloseTimeout(3*time.Second),
	)
	if err = registerProductionBeans(container, rootLogger, config); err != nil {
		_ = provider.Close()
		return err
	}
	if err = container.Build(context.Background()); err != nil {
		_ = provider.Close()
		return err
	}

	cleaned := false
	defer func() {
		if cleaned {
			return
		}
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = container.Close(closeCtx)
		_ = provider.Close()
	}()

	runner, err := ioc.ResolveAs[*worker](container)
	if err != nil {
		return err
	}
	ctx, stopSignal := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignal()

	runErr := make(chan error, 1)
	go func() {
		runErr <- runner.Run(ctx)
	}()
	rootLogger.Info(ctx, "production quickstart running",
		baselog.Op("quickstart_ioc.prod.run"), baselog.Result(baselog.ResultRunning),
		baselog.F("run_seconds", config.RunSeconds),
	)

	// 生产化流程：信号优先，其次按配置时长自动退出，确保示例可收敛。
	var stopReason string
	select {
	case err = <-runErr:
		if err != nil {
			return err
		}
		stopReason = "worker_finished"
	case <-ctx.Done():
		stopReason = "signal"
	case <-time.After(time.Duration(config.RunSeconds) * time.Second):
		stopReason = "timer"
	}

	stopSignal()
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	closeErr := container.Close(closeCtx)
	providerErr := provider.Close()
	cleaned = true
	if closeErr != nil || providerErr != nil {
		return errors.Join(closeErr, providerErr)
	}
	rootLogger.Info(ctx, "production quickstart stopped",
		baselog.Op("quickstart_ioc.prod.run"), baselog.Result(baselog.ResultOK),
		baselog.Reason(stopReason),
	)
	return nil
}

// registerProductionBeans 负责生产化模式的容器装配注册。
func registerProductionBeans(container ioc.Container, logger baselog.Logger, config appConfig) error {
	if err := container.Register(config, ioc.WithName("AppConfig")); err != nil {
		return err
	}
	if err := container.Register(logger, ioc.WithName("Logger"), ioc.WithDeclare[baselog.Logger]()); err != nil {
		return err
	}
	if err := container.Register(&taskRepository{}); err != nil {
		return err
	}
	if err := container.Register(&taskService{}); err != nil {
		return err
	}
	if err := container.Register(&worker{}); err != nil {
		return err
	}
	return nil
}

// loadRunConfig 从环境变量读取生产模式运行参数并做最小校验。
func loadRunConfig() (appConfig, error) {
	raw := strings.TrimSpace(os.Getenv(quickstartIOCRunSecondsEnv))
	if raw == "" {
		return appConfig{RunSeconds: defaultRunSeconds}, nil
	}
	var seconds int
	if _, err := fmt.Sscanf(raw, "%d", &seconds); err != nil {
		return appConfig{}, fmt.Errorf("invalid %s=%s: %w", quickstartIOCRunSecondsEnv, raw, err)
	}
	if seconds <= 0 {
		return appConfig{}, fmt.Errorf("%s must be > 0, got=%d", quickstartIOCRunSecondsEnv, seconds)
	}
	return appConfig{RunSeconds: seconds}, nil
}

// sortedMapKeys 返回 map key 的稳定排序结果，便于示例输出可预测。
func sortedMapKeys(items map[string]greeter) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	// 不引入额外依赖，示例里直接使用 strings.Compare 的冒泡排序。
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if strings.Compare(keys[i], keys[j]) > 0 {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
