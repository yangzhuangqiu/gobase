package hlogzap

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/yangzhuangqiu/gobase/errcode"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
)

// TestNewFromProviderRejectsNilProvider 验证共享后端入口不会接受缺失 Provider。
// 测试方法：传入 nil，断言返回 nil Logger 和稳定 provider.invalid 错误码。
func TestNewFromProviderRejectsNilProvider(t *testing.T) {
	logger, err := NewFromProvider(nil)
	if logger != nil || !errcode.IsCode[ErrorCode](err, ErrCodeProviderInvalid) {
		t.Fatalf("expected provider invalid error, logger=%v err=%v", logger, err)
	}
}

// TestNewFromProviderSharesLevelWriterAndCloseBoundary 验证 Hertz 与业务日志使用同一 Provider 运行时。
// 测试方法：动态更新共享 level 后从两个入口写入，再关闭 Provider 并继续写入，断言过滤、输出字段和关闭边界一致。
func TestNewFromProviderSharesLevelWriterAndCloseBoundary(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "provider-shared.log")
	provider := newProviderForAdapterTest(t, logFile, "error")
	logger, err := NewFromProvider(provider)
	if err != nil {
		t.Fatalf("new logger from provider: %v", err)
	}
	var alternateOutput bytes.Buffer
	logger.SetOutput(&alternateOutput)
	logger.Debug("hertz before update")

	// 第一阶段：通过 Hertz Control 更新共享 Provider，业务与框架入口应同时看到新 level。
	logger.SetLevel(hlog.LevelDebug)
	logger.Debug("hertz after update")
	provider.Factory().Get("business").Debug(context.Background(), "business after update")
	if err := provider.Close(); err != nil {
		t.Fatalf("close provider: %v", err)
	}
	logger.Error("hertz after close")

	// 第二阶段：读取唯一输出文件，验证没有独立 writer 或关闭后旁路写入。
	events := readProviderEvents(t, logFile)
	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read shared provider log: %v", err)
	}
	text := string(content)
	if strings.Contains(text, "hertz before update") || strings.Contains(text, "hertz after close") {
		t.Fatalf("adapter crossed shared level/close boundary: %s", text)
	}
	if !strings.Contains(text, "hertz after update") || !strings.Contains(text, "business after update") {
		t.Fatalf("shared provider events missing: %s", text)
	}
	if alternateOutput.Len() != 0 {
		t.Fatalf("SetOutput bypassed provider ownership: %s", alternateOutput.String())
	}
	if !eventHasField(events, "hertz after update", "logger", providerLoggerName) {
		t.Fatalf("expected stable Hertz logger field, events=%+v", events)
	}
}

// TestNewFromProviderCallerPointsToHlogCallSite 验证共享 Core 不会把 caller 留在适配器内部。
// 测试方法：临时安装返回 Logger，通过 hlog.Infof 写入，恢复全局状态后解析 JSON，断言 caller 指向当前测试文件。
func TestNewFromProviderCallerPointsToHlogCallSite(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "provider-caller.log")
	provider := newProviderForAdapterTest(t, logFile, "info")
	logger, err := NewFromProvider(provider)
	if err != nil {
		t.Fatalf("new logger from provider: %v", err)
	}
	// 第一阶段：只在单条调用期间替换进程全局 Logger，随后立即恢复。
	previous := hlog.DefaultLogger()
	hlog.SetLogger(logger)
	hlog.Infof("provider caller check")
	hlog.SetLogger(previous)
	if err := provider.Close(); err != nil {
		t.Fatalf("close provider: %v", err)
	}

	// 第二阶段：从共享文件中定位 Hertz 事件并核对调用位置。
	events := readProviderEvents(t, logFile)
	for _, event := range events {
		if event["msg"] != "provider caller check" {
			continue
		}
		caller, _ := event["caller"].(string)
		if !strings.Contains(caller, "provider_logger_test.go") || strings.Contains(caller, "provider_logger.go") {
			t.Fatalf("unexpected shared provider caller: %s", caller)
		}
		return
	}
	t.Fatalf("provider caller event not found: %+v", events)
}

// newProviderForAdapterTest 构建由测试显式关闭的文件模式 log-zap Provider。
// 阶段角色：固定 JSON 格式和 caller，使共享底层断言可重复。
func newProviderForAdapterTest(t *testing.T, path string, level string) *logzap.Provider {
	t.Helper()
	provider, err := logzap.NewProvider(logzap.Config{
		Level:  level,
		Mode:   logzap.ModeFile,
		Format: logzap.FormatJSON,
		Rotate: logzap.RotateConfig{Filename: path},
	})
	if err != nil {
		t.Fatalf("new log-zap provider: %v", err)
	}
	return provider
}

// readProviderEvents 解码文件中的逐行 JSON 日志事件。
func readProviderEvents(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read provider events: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	result := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("decode provider event: %v", err)
		}
		result = append(result, event)
	}
	return result
}

// eventHasField 判断指定消息事件是否携带期望字段值。
func eventHasField(events []map[string]any, message string, key string, value any) bool {
	for _, event := range events {
		if event["msg"] == message && event[key] == value {
			return true
		}
	}
	return false
}
