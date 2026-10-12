package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/ioc"
)

type facadeConfig struct {
	// Service 是门面测试使用的配置域。
	Service struct {
		// Name 验证默认配置经 Runtime 门面发布。
		Name string `yaml:"name"`
	} `yaml:"service"`
}

type facadeBean struct {
	// Value 是自动配置注册结果的稳定断言值。
	Value string
}

type facadeModule struct{}

// Name 返回门面测试自动配置的稳定名称。
func (this *facadeModule) Name() string {
	return "app.facade.test"
}

// Register 声明门面测试 bean，验证 app alias 可直接实现 Module 契约。
func (this *facadeModule) Register(registry ioc.Registry) error {
	return registry.Register(&facadeBean{Value: "ready"})
}

// TestUseCasePackageRunUsesRuntimeFacade 验证普通启动路径只通过 app 完成 Runtime 配置与执行。
//
// 测试方法：
// 1. 使用 app 门面声明默认配置、校验器、自动配置和 Runner；
// 2. 调用包级 Run，并通过 CLI 单向禁用配置文件；
// 3. 断言应用名传播、配置发布、bean 注册和 Runner 执行全部成功。
func TestUseCasePackageRunUsesRuntimeFacade(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"facade", "--bootstrap-without-config-file"}

	defaultConfig := facadeConfig{}
	defaultConfig.Service.Name = "facade-service"
	validatorCalled := false
	runnerCalled := false
	err := Run(
		WithName("facade-service"),
		WithConfig(config.Defaults(defaultConfig), config.Validate(func(config *facadeConfig) []error {
			validatorCalled = true
			if config.Service.Name == "" {
				return []error{errors.New("service.name is required")}
			}
			return nil
		})),
		WithModules(&facadeModule{}),
		WithRunner(func(_ context.Context, runtime Runtime) error {
			runnerCalled = true
			if runtime.Info().Name != "facade-service" {
				return errors.New("runtime application name mismatch")
			}
			resolved := &facadeConfig{}
			if applyErr := runtime.ConfigManager().Current().Decode("", resolved); applyErr != nil {
				return applyErr
			}
			if resolved.Service.Name != "facade-service" {
				return errors.New("runtime default config mismatch")
			}
			var bean *facadeBean
			if resolveErr := runtime.Container().Resolve(&bean); resolveErr != nil {
				return resolveErr
			}
			if bean.Value != "ready" {
				return errors.New("auto configuration bean mismatch")
			}
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("run facade application failed: %v", err)
	}
	if !validatorCalled || !runnerCalled {
		t.Fatalf("facade callbacks missing: validator=%t runner=%t", validatorCalled, runnerCalled)
	}
}

// TestUseCasePackageRunSupportsCommandOnlyApplication 验证极简门面支持只有子命令、没有 root Runner 的应用。
//
// 测试方法：通过 WithCommands 声明无 Runtime 的 status 子命令，执行后断言参数、命令路径和 nil Runtime 语义。
func TestUseCasePackageRunSupportsCommandOnlyApplication(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"orderctl", "status", "payment"}

	called := false
	err := Run(
		WithName("orderctl"),
		WithCommands(Command{
			Use:         "status [service]",
			Short:       "Show service status",
			RuntimeMode: RuntimeModeDisabled,
			ValidateArgs: func(args []string) error {
				if len(args) != 1 {
					return errors.New("service argument is required")
				}
				return nil
			},
			Runner: func(_ context.Context, runtime Runtime, command CommandContext) error {
				called = true
				if runtime != nil {
					return errors.New("disabled command runtime must be nil")
				}
				if command.Name() != "status" || command.Path() != "orderctl status" {
					return errors.New("command identity mismatch")
				}
				args := command.Args()
				if len(args) != 1 || args[0] != "payment" {
					return errors.New("command arguments mismatch")
				}
				return nil
			},
		}),
	)
	if err != nil {
		t.Fatalf("run command-only application failed: %v", err)
	}
	if !called {
		t.Fatalf("command runner was not called")
	}
}

// TestUseCaseCommandOnlyRootShowsHelp 验证 command-only 应用执行 root 时展示帮助而非要求占位 Runner。
//
// 测试方法：只声明一个子命令并执行空参数 root，断言返回成功且帮助页包含子命令。
func TestUseCaseCommandOnlyRootShowsHelp(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"orderctl"}

	var output strings.Builder
	err := Run(
		WithName("orderctl"),
		WithDescription("Order command line"),
		WithOutput(&output),
		WithCommands(Command{
			Use:         "status",
			Short:       "Show service status",
			RuntimeMode: RuntimeModeDisabled,
			Runner: func(context.Context, Runtime, CommandContext) error {
				return nil
			},
		}),
	)
	if err != nil {
		t.Fatalf("run command-only root failed: %v", err)
	}
	text := output.String()
	if !strings.Contains(text, "Available Commands:") || !strings.Contains(text, "status") {
		t.Fatalf("command-only root help mismatch: %s", text)
	}
}

// TestUseCaseMainReturnsAfterSuccessfulRun 验证进程级 Main 在成功场景完成执行后正常返回。
//
// 测试方法：设置无配置文件参数并提供 Runner，调用 Main 后断言 Runner 已执行；失败分支由 Main 的进程退出契约负责。
func TestUseCaseMainReturnsAfterSuccessfulRun(t *testing.T) {
	originalArgs := os.Args
	t.Cleanup(func() { os.Args = originalArgs })
	os.Args = []string{"facade-main", "--bootstrap-without-config-file"}

	called := false
	Main(
		WithName("facade-main"),
		WithRunner(func(context.Context, Runtime) error {
			called = true
			return nil
		}),
	)
	if !called {
		t.Fatalf("main runner was not called")
	}
}

// TestUseCaseMainWritesErrorAndExits 验证进程级 Main 的失败输出与退出码契约。
//
// 测试方法：通过子进程执行返回错误的 Runner，断言 Main 把错误写到 stderr 并以状态码 1 退出。
func TestUseCaseMainWritesErrorAndExits(t *testing.T) {
	if os.Getenv("GOBASE_TEST_APP_MAIN_FAILURE") == "1" {
		// 子进程阶段：重置测试参数，确保错误来源是 Runner 而非 testing flags。
		os.Args = []string{"facade-main", "--bootstrap-without-config-file"}
		Main(
			WithName("facade-main"),
			WithRunner(func(context.Context, Runtime) error {
				return errors.New("expected main failure")
			}),
		)
		return
	}

	// 主测试阶段：启动当前测试二进制的单用例子进程并检查可观察结果。
	command := exec.Command(os.Args[0], "-test.run=^TestUseCaseMainWritesErrorAndExits$")
	command.Env = append(os.Environ(), "GOBASE_TEST_APP_MAIN_FAILURE=1")
	output, err := command.CombinedOutput()
	exitErr, ok := err.(*exec.ExitError)
	if !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("main exit code mismatch: err=%v output=%s", err, output)
	}
	if !strings.Contains(string(output), "expected main failure") {
		t.Fatalf("main error output mismatch: %s", output)
	}
}
