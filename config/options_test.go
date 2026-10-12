package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yangzhuangqiu/gobase/errcode"
)

// TestFileOptionCollectsPathAndWatch 验证 File 二级 Option 会统一保存路径与监听策略。
// 测试方法：声明显式路径和轮询周期，断言 Builder 只产生一组一致文件状态。
func TestFileOptionCollectsPathAndWatch(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "application.yaml")
	overlayPath := filepath.Join(dir, "application-prod.yaml")
	builder := New(File(
		FilePath(basePath),
		FilePath(overlayPath),
		FileWatch(5*time.Second),
	))
	if builder.disableConfigFile {
		t.Fatalf("expected file capability enabled")
	}
	if len(builder.configFiles) != 2 || builder.configFiles[0] != basePath || builder.configFiles[1] != overlayPath {
		t.Fatalf("unexpected config file paths: %#v", builder.configFiles)
	}
	if !builder.enableLocalFileWatch || !builder.localFileWatchExplicit || builder.localFileWatchInterval != 5*time.Second {
		t.Fatalf("unexpected file watch declaration: enabled=%v explicit=%v interval=%s",
			builder.enableLocalFileWatch, builder.localFileWatchExplicit, builder.localFileWatchInterval)
	}
}

// TestFileOptionRejectsDuplicateResolvedPaths 验证同一文件不能在有序列表中重复出现。
// 测试方法：以相同路径的不同清洗形式声明两次，Build 后断言 duplicate_path。
func TestFileOptionRejectsDuplicateResolvedPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "application.yaml")
	if err := os.WriteFile(path, []byte("service:\n  name: demo\n"), 0644); err != nil {
		t.Fatalf("write config file failed: %v", err)
	}
	_, err := New(File(FilePath(path, filepath.Join(dir, ".", "application.yaml")))).Build(context.Background())
	if !errcode.IsCode[ErrorCode](err, ErrCodeConfigConfigFileInvalid) {
		t.Fatalf("expected duplicate path error, got=%v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "reason=duplicate_path") {
		t.Fatalf("expected duplicate path reason, got=%v", err)
	}
}

// TestFileOptionRejectsDisabledPathOrWatch 验证禁用文件时不能继续声明路径或监听。
// 测试方法：分别组合 disabled+path 和 disabled+watch，Build 后断言稳定 disabled_conflict 错误。
func TestFileOptionRejectsDisabledPathOrWatch(t *testing.T) {
	tests := []struct {
		name    string
		options []FileOption
	}{
		{name: "path_before_disabled", options: []FileOption{FilePath("application.yaml"), FileDisabled()}},
		{name: "path_after_disabled", options: []FileOption{FileDisabled(), FilePath("application.yaml")}},
		{name: "watch_before_disabled", options: []FileOption{FileWatch(), FileDisabled()}},
		{name: "watch_after_disabled", options: []FileOption{FileDisabled(), FileWatch()}},
	}
	for _, item := range tests {
		t.Run(item.name, func(t *testing.T) {
			_, err := New(File(item.options...)).Build(context.Background())
			if !errcode.IsCode[ErrorCode](err, ErrCodeConfigConfigFileDisabledConflict) {
				t.Fatalf("expected disabled conflict, got=%v", err)
			}
		})
	}
}

// TestFileOptionRejectsDuplicateStableDeclarations 验证稳定配置只允许一个 File 声明。
// 测试方法：连续声明两个 File Option，Build 后断言 cfg.file.invalid 与 duplicate reason。
func TestFileOptionRejectsDuplicateStableDeclarations(t *testing.T) {
	_, err := New(
		File(FilePath("first.yaml")),
		File(FileWatch()),
	).Build(context.Background())
	if !errcode.IsCode[ErrorCode](err, ErrCodeConfigConfigFileInvalid) {
		t.Fatalf("expected duplicate file declaration error, got=%v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "reason=duplicate_declaration") {
		t.Fatalf("expected duplicate declaration reason, got=%v", err)
	}
}

// TestInputFileWatchPreservesStablePaths 验证单次 File Input 只覆盖显式声明的字段。
// 测试方法：Application 声明路径，Input 只声明 watch，断言路径保留且监听开启。
func TestInputFileWatchPreservesStablePaths(t *testing.T) {
	path := filepath.Join(t.TempDir(), "application.yaml")
	options := []Option{File(FilePath(path))}
	options = append(options, Input{File: []FileOption{FileWatch(5 * time.Second)}}.Options()...)
	builder := New(options...)
	if len(builder.configFiles) != 1 || builder.configFiles[0] != path {
		t.Fatalf("expected stable file path preserved, got=%#v", builder.configFiles)
	}
	if !builder.enableLocalFileWatch || builder.localFileWatchInterval != 5*time.Second {
		t.Fatalf("expected input watch override, enabled=%v interval=%s", builder.enableLocalFileWatch, builder.localFileWatchInterval)
	}
}
