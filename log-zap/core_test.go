package zap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gozap "go.uber.org/zap"
)

// TestProviderCoreSharesDynamicLevelAndCloseBoundary 验证借用 Core 始终跟随 Provider 当前运行状态。
// 测试方法：先以 error 级别过滤 Debug，再动态切到 debug 并写入，关闭后继续写入，断言只有更新后、关闭前事件落盘。
func TestProviderCoreSharesDynamicLevelAndCloseBoundary(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "shared-core.log")
	provider := newFileProviderForTest(t, logFile, "error")
	logger := gozap.New(provider.Core()).With(gozap.String("source", "borrowed-core"))
	// 第一阶段：同一个稳定 Core 必须立即看到 Provider level 更新和关闭状态。
	logger.Debug("debug before update")
	if err := provider.ApplyRuntimeConfig(RuntimeConfig{Level: "debug"}); err != nil {
		t.Fatalf("apply runtime config: %v", err)
	}
	logger.Debug("debug after update")
	if err := provider.Close(); err != nil {
		t.Fatalf("close provider: %v", err)
	}
	logger.Error("error after close")

	// 第二阶段：输出内容同时证明动态过滤、With 字段和关闭后停写。
	raw, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read shared core log: %v", err)
	}
	content := string(raw)
	if strings.Contains(content, "debug before update") || strings.Contains(content, "error after close") {
		t.Fatalf("borrowed core crossed provider lifecycle boundary: %s", content)
	}
	if !strings.Contains(content, "debug after update") || !strings.Contains(content, `"source":"borrowed-core"`) {
		t.Fatalf("borrowed core did not share provider output: %s", content)
	}
}

// TestProviderCoreSyncDoesNotTakeResourceOwnership 验证借用 Core 的 Sync 不刷新或关闭 Provider writer。
// 测试方法：通过 Core 写入并调用 Sync，再通过业务 Factory 写入，最终由 Provider Close 收敛并断言两条事件均存在。
func TestProviderCoreSyncDoesNotTakeResourceOwnership(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "shared-core-sync.log")
	provider := newFileProviderForTest(t, logFile, "info")
	logger := gozap.New(provider.Core())
	// 第一阶段：借用方 Sync 只完成接口调用，不触碰 Provider 持有的 writer。
	logger.Info("framework before borrowed sync")
	if err := logger.Sync(); err != nil {
		t.Fatalf("borrowed core sync: %v", err)
	}
	provider.Factory().Get("business").Info(context.Background(), "business after borrowed sync")
	// 第二阶段：只有 Provider Close 执行真实刷新与资源释放。
	if err := provider.Close(); err != nil {
		t.Fatalf("close provider: %v", err)
	}

	raw, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read sync ownership log: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "framework before borrowed sync") || !strings.Contains(content, "business after borrowed sync") {
		t.Fatalf("borrowed sync changed provider ownership: %s", content)
	}
}
