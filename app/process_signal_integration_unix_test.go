//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package app

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

const (
	// processSignalHelperModeEnv 选择当前测试进程是否进入信号子进程模式。
	processSignalHelperModeEnv = "GOBASE_APP_SIGNAL_HELPER_MODE"
	// processSignalHelperReady 表示子进程已经安装信号监听并进入 Runner。
	processSignalHelperReady = "APP_SIGNAL_READY"
	// processSignalHelperHook 表示首次信号已经启动 Shutdown Hook。
	processSignalHelperHook = "APP_SIGNAL_HOOK_STARTED"
	// processSignalHelperExited 表示完整优雅关闭已经正常返回。
	processSignalHelperExited = "APP_SIGNAL_EXITED"
)

// TestProcessSignalsGracefullyExitSubprocess 验证真实 SIGINT/SIGTERM 能驱动完整优雅关闭并以状态码 0 退出。
//
// 测试方法：为每种 Unix 默认信号启动当前测试二进制子进程，等待 Runner ready 后发送信号，断言 Hook 和正常退出标记均出现。
func TestProcessSignalsGracefullyExitSubprocess(t *testing.T) {
	if os.Getenv(processSignalHelperModeEnv) == "graceful" {
		runProcessSignalHelper(false)
		return
	}

	for _, processSignal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		processSignal := processSignal
		t.Run(processSignal.String(), func(t *testing.T) {
			command, lines, stderr := startProcessSignalHelper(t, "graceful", "TestProcessSignalsGracefullyExitSubprocess")
			waitForProcessSignalLine(t, lines, processSignalHelperReady, stderr)
			if err := command.Process.Signal(processSignal); err != nil {
				t.Fatalf("send %s to helper: %v", processSignal, err)
			}
			waitForProcessSignalLine(t, lines, processSignalHelperHook, stderr)
			waitForProcessSignalLine(t, lines, processSignalHelperExited, stderr)
			if err := command.Wait(); err != nil {
				t.Fatalf("helper should exit successfully after %s: %v stderr=%s", processSignal, err, stderr.String())
			}
		})
	}
}

// TestSecondProcessSignalForcesSubprocessExit 验证首次信号后已恢复默认行为，第二次同类信号会强制终止进程。
//
// 测试方法：子进程 Hook 在首次 SIGTERM 后故意阻塞；确认 Hook 启动后再次发送 SIGTERM，断言进程由该信号终止。
func TestSecondProcessSignalForcesSubprocessExit(t *testing.T) {
	if os.Getenv(processSignalHelperModeEnv) == "blocking" {
		runProcessSignalHelper(true)
		return
	}

	command, lines, stderr := startProcessSignalHelper(t, "blocking", "TestSecondProcessSignalForcesSubprocessExit")
	waitForProcessSignalLine(t, lines, processSignalHelperReady, stderr)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send first SIGTERM to helper: %v", err)
	}
	waitForProcessSignalLine(t, lines, processSignalHelperHook, stderr)
	if err := command.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send second SIGTERM to helper: %v", err)
	}
	err := command.Wait()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("helper should be terminated by second SIGTERM, got=%v stderr=%s", err, stderr.String())
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGTERM {
		t.Fatalf("unexpected forced exit status=%v stderr=%s", exitErr.Sys(), stderr.String())
	}
}

// runProcessSignalHelper 运行真实 App.Run；blockingHook 用于验证第二次信号的系统默认行为。
func runProcessSignalHelper(blockingHook bool) {
	os.Args = []string{"app-signal-helper", "serve"}
	application := New(
		WithCommands(Command{
			Use:         "serve",
			RuntimeMode: RuntimeModeDisabled,
			Runner: func(ctx context.Context, _ Runtime, _ CommandContext) error {
				_, _ = fmt.Fprintln(os.Stdout, processSignalHelperReady)
				<-ctx.Done()
				return ctx.Err()
			},
		}),
		WithShutdownHook("signal-helper", func(context.Context, Runtime, CommandContext) error {
			_, _ = fmt.Fprintln(os.Stdout, processSignalHelperHook)
			if blockingHook {
				select {}
			}
			return nil
		}),
	)
	if err := application.Run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "signal helper failed: %v\n", err)
		os.Exit(23)
	}
	_, _ = fmt.Fprintln(os.Stdout, processSignalHelperExited)
}

// startProcessSignalHelper 启动指定测试函数的当前测试二进制，并异步发布其标准输出行。
func startProcessSignalHelper(t *testing.T, mode string, testName string) (*exec.Cmd, <-chan string, *bytes.Buffer) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
	command.Env = append(os.Environ(), processSignalHelperModeEnv+"="+mode)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("create helper stdout pipe: %v", err)
	}
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start signal helper: %v", err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })
	lines := make(chan string, 8)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	return command, lines, stderr
}

// waitForProcessSignalLine 在有限时间内等待子进程发布指定生命周期标记。
func waitForProcessSignalLine(t *testing.T, lines <-chan string, expected string, stderr *bytes.Buffer) {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case line, open := <-lines:
			if !open {
				t.Fatalf("helper exited before %s stderr=%s", expected, stderr.String())
			}
			if line == expected {
				return
			}
		case <-timeout.C:
			t.Fatalf("timeout waiting for %s stderr=%s", expected, stderr.String())
		}
	}
}
