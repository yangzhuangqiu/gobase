package app

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// configureCommandHelp 为命令注入按“框架/业务”分组的帮助输出。
func (this *App) configureCommandHelp(command *cobra.Command) {
	if command == nil {
		return
	}
	command.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		writeGroupedUsage(cmd)
	})
	command.SetUsageFunc(func(cmd *cobra.Command) error {
		writeGroupedUsage(cmd)
		return nil
	})
}

// writeGroupedUsage 输出分组后的 usage，降低新手识别框架参数与业务参数的成本。
func writeGroupedUsage(command *cobra.Command) {
	if command == nil {
		return
	}
	output := command.OutOrStderr()
	writeCommandIntro(output, command)
	writeUsageSection(output, command)
	writeAvailableCommands(output, command)

	frameworkFlags, businessFlags := collectGroupedFlags(command)
	writeFlagGroup(output, "Framework Flags", frameworkFlags)
	writeFlagGroup(output, "Business Flags", businessFlags)

	example := strings.TrimSpace(command.Example)
	if example != "" {
		_, _ = fmt.Fprintln(output)
		_, _ = fmt.Fprintln(output, "Examples:")
		_, _ = fmt.Fprintln(output, example)
	}
	if command.HasAvailableSubCommands() {
		_, _ = fmt.Fprintln(output)
		_, _ = fmt.Fprintf(output, "Use \"%s [command] --help\" for more information about a command.\n", command.CommandPath())
	}
}

// writeCommandIntro 输出命令短描述，便于帮助页第一眼识别命令用途。
func writeCommandIntro(output io.Writer, command *cobra.Command) {
	text := strings.TrimSpace(command.Long)
	if text == "" {
		text = strings.TrimSpace(command.Short)
	}
	if text == "" {
		return
	}
	_, _ = fmt.Fprintln(output, text)
	_, _ = fmt.Fprintln(output)
}

// writeUsageSection 输出 usage 主体。
func writeUsageSection(output io.Writer, command *cobra.Command) {
	_, _ = fmt.Fprintln(output, "Usage:")
	if command.Runnable() {
		_, _ = fmt.Fprintf(output, "  %s\n", command.UseLine())
	}
	if command.HasAvailableSubCommands() {
		_, _ = fmt.Fprintf(output, "  %s [command]\n", command.CommandPath())
	}
	if !command.Runnable() && !command.HasAvailableSubCommands() {
		_, _ = fmt.Fprintf(output, "  %s\n", command.UseLine())
	}
}

// writeAvailableCommands 输出可用子命令列表。
func writeAvailableCommands(output io.Writer, command *cobra.Command) {
	available := commandListForUsage(command)
	if len(available) == 0 {
		return
	}

	width := 0
	for _, item := range available {
		if item == nil {
			continue
		}
		if length := len(item.Name()); length > width {
			width = length
		}
	}

	_, _ = fmt.Fprintln(output)
	_, _ = fmt.Fprintln(output, "Available Commands:")
	for _, item := range available {
		if item == nil {
			continue
		}
		_, _ = fmt.Fprintf(output, "  %-*s %s\n", width, item.Name(), item.Short)
	}
}

// commandListForUsage 返回帮助输出中应展示的子命令列表。
func commandListForUsage(command *cobra.Command) []*cobra.Command {
	if command == nil {
		return nil
	}
	result := make([]*cobra.Command, 0, len(command.Commands()))
	for _, item := range command.Commands() {
		if item == nil || item.Hidden || !item.IsAvailableCommand() || item.IsAdditionalHelpTopicCommand() {
			continue
		}
		result = append(result, item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		return strings.Compare(result[i].Name(), result[j].Name()) < 0
	})
	return result
}

// collectGroupedFlags 按框架/业务语义收集 flags（含 inherited）。
func collectGroupedFlags(command *cobra.Command) (framework []*pflag.Flag, business []*pflag.Flag) {
	if command == nil {
		return nil, nil
	}
	seen := map[string]struct{}{}
	appendFlag := func(flag *pflag.Flag) {
		if flag == nil || flag.Hidden {
			return
		}
		name := strings.TrimSpace(flag.Name)
		if name == "" {
			return
		}
		if _, exists := seen[name]; exists {
			return
		}
		seen[name] = struct{}{}
		if isFrameworkFlag(flag) {
			framework = append(framework, flag)
			return
		}
		business = append(business, flag)
	}

	if flags := command.NonInheritedFlags(); flags != nil {
		flags.VisitAll(appendFlag)
	}
	if flags := command.InheritedFlags(); flags != nil {
		flags.VisitAll(appendFlag)
	}

	sort.SliceStable(framework, func(i, j int) bool {
		return strings.Compare(framework[i].Name, framework[j].Name) < 0
	})
	sort.SliceStable(business, func(i, j int) bool {
		return strings.Compare(business[i].Name, business[j].Name) < 0
	})
	return framework, business
}

// writeFlagGroup 输出一组 flags 的 usage 文本。
func writeFlagGroup(output io.Writer, title string, flags []*pflag.Flag) {
	usage := groupedFlagUsage(flags)
	if usage == "" {
		return
	}
	_, _ = fmt.Fprintln(output)
	_, _ = fmt.Fprintf(output, "%s:\n", title)
	_, _ = fmt.Fprintln(output, usage)
}

// groupedFlagUsage 复用 pflag 内置格式化逻辑，避免手工拼接造成样式不一致。
func groupedFlagUsage(flags []*pflag.Flag) string {
	if len(flags) == 0 {
		return ""
	}
	flagSet := pflag.NewFlagSet("grouped-help", pflag.ContinueOnError)
	flagSet.SortFlags = true
	flagSet.SetOutput(io.Discard)

	added := map[string]struct{}{}
	for _, item := range flags {
		if item == nil || item.Hidden {
			continue
		}
		if _, exists := added[item.Name]; exists {
			continue
		}
		added[item.Name] = struct{}{}

		copied := *item
		flagSet.AddFlag(&copied)
	}
	return strings.TrimRight(flagSet.FlagUsagesWrapped(120), "\n")
}
