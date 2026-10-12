package hlogzap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/yangzhuangqiu/gobase/errcode"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
)

// TestResolveConfigUsesLogZapDefaults 验证 hlog-zap 共享 log-zap 默认值语义。
//
// 测试意图：
// - 锁定 hlog-zap 与 log-zap 共享统一默认语义，调用方无需重复填值。
//
// 测试方法：
// - 传入空 Config 调用 log-zap ResolveConfig；
// - 断言 level/mode/format/caller/rotate 默认值。
func TestResolveConfigUsesLogZapDefaults(t *testing.T) {
	resolved, err := logzap.ResolveConfig(logzap.Config(Config{}))
	if err != nil {
		t.Fatalf("resolve config failed: %v", err)
	}
	if resolved.Level != "info" {
		t.Fatalf("unexpected default level: got=%s want=info", resolved.Level)
	}
	if resolved.Mode != ModeConsole {
		t.Fatalf("unexpected default mode: got=%s want=%s", resolved.Mode, ModeConsole)
	}
	if resolved.Format != FormatJSON {
		t.Fatalf("unexpected default format: got=%s want=%s", resolved.Format, FormatJSON)
	}
	if resolved.DisableCaller {
		t.Fatalf("unexpected default disableCaller: got=true want=false")
	}
	if resolved.Rotate.MaxSize != 10 {
		t.Fatalf("unexpected default rotate.maxSize: got=%d want=10", resolved.Rotate.MaxSize)
	}
	if resolved.Rotate.MaxBackups != 50 {
		t.Fatalf("unexpected default rotate.maxBackups: got=%d want=50", resolved.Rotate.MaxBackups)
	}
	if resolved.Rotate.MaxAge != 3 {
		t.Fatalf("unexpected default rotate.maxAge: got=%d want=3", resolved.Rotate.MaxAge)
	}
}

// TestNewLoggerRejectPanicLevel 验证 panic 级别非法。
//
// 测试意图：
// - 锁定 hlog 集成层仅接受 debug/info/warn/error/fatal（含兼容别名）。
//
// 测试方法：
// - 传入 panic 级别创建 logger；
// - 断言返回 log.level.invalid 错误码。
func TestNewLoggerRejectPanicLevel(t *testing.T) {
	_, err := NewLogger(Config{
		Level: "panic",
		Mode:  ModeConsole,
	})
	if !errcode.IsCode[ErrorCode](err, ErrCodeLogLevelInvalid) {
		t.Fatalf("expected invalid level error code, got=%v", err)
	}
}

// TestParseHlogLevelTreatsTraceAsDebugAlias 验证 Hertz 适配层不声明独立 Trace 级别。
//
// 测试方法：分别解析 trace 与 debug，断言两者得到相同 Zap 级别且均无错误。
func TestParseHlogLevelTreatsTraceAsDebugAlias(t *testing.T) {
	traceLevel, traceErr := parseZapLevel("trace")
	debugLevel, debugErr := parseZapLevel("debug")
	if traceErr != nil || debugErr != nil {
		t.Fatalf("parse compatibility level failed: trace=%v debug=%v", traceErr, debugErr)
	}
	if traceLevel != debugLevel {
		t.Fatalf("trace must alias debug: trace=%v debug=%v", traceLevel, debugLevel)
	}
}

// TestInstallWritesFile 验证 Install 后 hlog 输出可落盘。
//
// 测试意图：
// 1. 锁定装配层通过 Install 可一次性替换 Hertz 默认 hlog；
// 2. 保证 hlog 格式化调用仍由 Hertz 适配边界正确写入。
//
// 测试方法：
// 1. 保存旧 logger，安装 file 模式新 logger；
// 2. 通过 hlog.Infof 打印日志并执行 Sync；
// 3. 读取文件断言 message。
func TestInstallWritesFile(t *testing.T) {
	original := hlog.DefaultLogger()
	logFile := filepath.Join(
		os.TempDir(),
		fmt.Sprintf("hlog-zap-install-%d.log", time.Now().UnixNano()),
	)
	t.Cleanup(func() {
		hlog.SetLogger(original)
		_ = os.Remove(logFile)
	})

	logger, err := Install(Config{
		Level:  "info",
		Mode:   ModeFile,
		Format: FormatJSON,
		Rotate: RotateConfig{
			Filename: logFile,
		},
	})
	if err != nil {
		t.Fatalf("install hlog logger failed: %v", err)
	}

	hlog.Infof("install success op=%s result=%s", "bootstrap", "ok")
	logger.Sync()

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file failed: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "install success") {
		t.Fatalf("expected message in output, content=%s", text)
	}
}

// TestInstallFixedCallerBoundaryMatchesBusinessCall 验证固定适配边界可指向业务调用点。
//
// 测试意图：
// 1. 锁定 hlog-zap 会自动叠加 hlog 包装层 caller 偏移；
// 2. 保证调用方无需配置内部跳层也能获得可用 caller。
//
// 测试方法：
// 1. 使用固定适配边界安装 file 模式 logger；
// 2. 触发一次 hlog.Infof 并读取最后一条 JSON 日志；
// 3. 断言 caller 指向当前测试文件，而非 hertzzap 内部实现文件。
func TestInstallFixedCallerBoundaryMatchesBusinessCall(t *testing.T) {
	original := hlog.DefaultLogger()
	logFile := filepath.Join(
		os.TempDir(),
		fmt.Sprintf("hlog-zap-caller-%d.log", time.Now().UnixNano()),
	)
	t.Cleanup(func() {
		hlog.SetLogger(original)
		_ = os.Remove(logFile)
	})

	logger, err := Install(Config{
		Level:  "info",
		Mode:   ModeFile,
		Format: FormatJSON,
		Rotate: RotateConfig{
			Filename: logFile,
		},
	})
	if err != nil {
		t.Fatalf("install hlog logger failed: %v", err)
	}

	hlog.Infof("caller check")
	logger.Sync()

	caller, err := readCallerFromLogFile(logFile)
	if err != nil {
		t.Fatalf("read caller failed: %v", err)
	}
	if !strings.Contains(caller, "config_test.go") {
		t.Fatalf("expected caller in business test file, caller=%s", caller)
	}
	if strings.Contains(caller, "logger.go") {
		t.Fatalf("caller should not point to hertzzap internal logger, caller=%s", caller)
	}
}

// readCallerFromLogFile 读取日志文件最后一行 JSON 的 caller 字段。
func readCallerFromLogFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) == 0 {
		return "", nil
	}
	item := map[string]any{}
	if err = json.Unmarshal([]byte(lines[len(lines)-1]), &item); err != nil {
		return "", err
	}
	caller, _ := item["caller"].(string)
	return caller, nil
}
