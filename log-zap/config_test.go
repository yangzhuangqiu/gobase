package zap

import (
	"testing"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// TestResolveConfigUsesDefaults 验证空配置会命中默认值。
//
// 测试意图：
// 1. 锁定调用方可通过最小配置（甚至空配置）直接启动；
// 2. 避免默认值语义在后续重构中漂移。
//
// 测试方法：
// 1. 传入空 Config 调用 ResolveConfig；
// 2. 断言 level/mode/format/caller/rotate 默认值。
func TestResolveConfigUsesDefaults(t *testing.T) {
	resolved, err := ResolveConfig(Config{})
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
	if resolved.Rotate.MaxSize != defaultRotateMaxSize {
		t.Fatalf("unexpected default rotate.maxSize: got=%d want=%d", resolved.Rotate.MaxSize, defaultRotateMaxSize)
	}
	if resolved.Rotate.MaxBackups != defaultRotateBackups {
		t.Fatalf("unexpected default rotate.maxBackups: got=%d want=%d", resolved.Rotate.MaxBackups, defaultRotateBackups)
	}
	if resolved.Rotate.MaxAge != defaultRotateMaxAge {
		t.Fatalf("unexpected default rotate.maxAge: got=%d want=%d", resolved.Rotate.MaxAge, defaultRotateMaxAge)
	}
}

// TestResolveConfigFileModeUsesRotateDefaults 验证 file 模式仅填 filename 即可。
//
// 测试意图：
// - 锁定调用方在 file 模式只需提供最小必要字段 `rotate.filename`。
//
// 测试方法：
// - 传入 mode=file 且只填 rotate.filename；
// - 断言其余 rotate 参数自动回填默认值。
func TestResolveConfigFileModeUsesRotateDefaults(t *testing.T) {
	resolved, err := ResolveConfig(Config{
		Mode: ModeFile,
		Rotate: RotateConfig{
			Filename: "app.log",
		},
	})
	if err != nil {
		t.Fatalf("resolve file mode failed: %v", err)
	}
	if resolved.Rotate.Filename != "app.log" {
		t.Fatalf("unexpected rotate.filename: got=%s want=app.log", resolved.Rotate.Filename)
	}
	if resolved.Rotate.MaxSize != defaultRotateMaxSize {
		t.Fatalf("unexpected default rotate.maxSize: got=%d want=%d", resolved.Rotate.MaxSize, defaultRotateMaxSize)
	}
	if resolved.Rotate.MaxBackups != defaultRotateBackups {
		t.Fatalf("unexpected default rotate.maxBackups: got=%d want=%d", resolved.Rotate.MaxBackups, defaultRotateBackups)
	}
	if resolved.Rotate.MaxAge != defaultRotateMaxAge {
		t.Fatalf("unexpected default rotate.maxAge: got=%d want=%d", resolved.Rotate.MaxAge, defaultRotateMaxAge)
	}
}

// TestNormalizeConfigRejectTerminalLevel 验证终止进程级别非法。
//
// 测试意图：
// - 锁定 zap provider 接受 debug/info/warn/error，并把 trace 视为 Debug 兼容别名；不暴露 fatal/panic。
//
// 测试方法：
// - 分别传入 fatal/panic 初始化 provider；
// - 断言都返回 log.level.invalid 错误码。
func TestNormalizeConfigRejectTerminalLevel(t *testing.T) {
	for _, level := range []string{"fatal", "panic"} {
		_, err := NewProvider(Config{Level: level})
		if !errcode.IsCode[ErrorCode](err, ErrCodeLogLevelInvalid) {
			t.Fatalf("expected invalid level code, level=%s err=%v", level, err)
		}
	}
}

// TestParseZapLevelTreatsTraceAsDebugAlias 验证 trace 只保留为 Debug 配置兼容别名。
//
// 测试方法：分别解析 trace 与 debug，断言两者得到相同 Zap 级别且均无错误。
func TestParseZapLevelTreatsTraceAsDebugAlias(t *testing.T) {
	traceLevel, traceErr := parseZapLevel("trace")
	debugLevel, debugErr := parseZapLevel("debug")
	if traceErr != nil || debugErr != nil {
		t.Fatalf("parse compatibility level failed: trace=%v debug=%v", traceErr, debugErr)
	}
	if traceLevel != debugLevel {
		t.Fatalf("trace must alias debug: trace=%v debug=%v", traceLevel, debugLevel)
	}
}
