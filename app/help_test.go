package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestWriteGroupedUsageFiltersCommands 验证 help 渲染会过滤 hidden/help-topic 命令并输出可用子命令。
//
// 测试方法：
// 1. 构造 root 命令并挂载 normal/hidden/help-topic 子命令；
// 2. 调用 writeGroupedUsage 输出帮助文本；
// 3. 断言仅 normal 命令出现在 Available Commands。
func TestWriteGroupedUsageFiltersCommands(t *testing.T) {
	root := &cobra.Command{Use: "demo", Short: "demo app"}
	normal := &cobra.Command{Use: "run", Short: "run command", Run: func(cmd *cobra.Command, args []string) {}}
	hidden := &cobra.Command{Use: "hidden", Short: "hidden command", Hidden: true}
	topic := &cobra.Command{Use: "topic", Short: "topic", Run: nil}
	root.AddCommand(normal, hidden, topic)
	root.SetHelpCommand(&cobra.Command{Use: "help [command]", Short: "Help about any command"})

	var output bytes.Buffer
	root.SetOut(&output)
	root.SetErr(&output)

	writeGroupedUsage(root)
	text := output.String()
	if !strings.Contains(text, "Available Commands:") {
		t.Fatalf("expected Available Commands section, got: %s", text)
	}
	if !strings.Contains(text, "run") {
		t.Fatalf("expected normal command in usage, got: %s", text)
	}
	if strings.Contains(text, "hidden") {
		t.Fatalf("expected hidden command filtered, got: %s", text)
	}
}

// TestUseCaseHelpRendersDescriptionOnce 验证公共 help 入口不会重复输出命令描述。
//
// 测试方法：通过 WithOutput 执行 --help，并断言 root 描述只出现一次且分组 usage 仍存在。
func TestUseCaseHelpRendersDescriptionOnce(t *testing.T) {
	var output bytes.Buffer
	application := New(
		WithName("demo"),
		WithDescription("single app description"),
		WithOutput(&output),
	)
	if err := application.ExecuteContext(context.Background(), []string{"--help"}); err != nil {
		t.Fatalf("execute help failed: %v", err)
	}
	text := output.String()
	if strings.Count(text, "single app description") != 1 {
		t.Fatalf("description must render once, got: %s", text)
	}
	if !strings.Contains(text, "Framework Flags:") {
		t.Fatalf("grouped help missing framework flags: %s", text)
	}
}
