package app

import (
	"github.com/spf13/cobra"

	"github.com/yangzhuangqiu/gobase/config"
)

const (
	// configFlagName 是显式配置文件 flag 名。
	configFlagName = config.BootstrapKeyConfigFile
	// bootstrapFlagName 是可重复 bootstrap 覆盖 flag 名。
	bootstrapFlagName = config.BootstrapRootKey
	// bootstrapEnvPrefixFlagName 是环境变量前缀 flag 名。
	bootstrapEnvPrefixFlagName = "bootstrap-env-prefix"
	// bootstrapWithoutConfigFileFlagName 是严格禁用配置文件 flag 名。
	bootstrapWithoutConfigFileFlagName = "bootstrap-without-config-file"
	// overrideFlagName 是运行时 pinned 覆盖 flag 名。
	overrideFlagName = "override"
	// listBootstrapKeysFlagName 是 bootstrap schema 发现 flag 名。
	listBootstrapKeysFlagName = "list-bootstrap-keys"
	// versionFlagName 是版本输出 flag 名；root 和子命令都必须在 Runtime 前短路。
	versionFlagName = "version"
	// commandFrameworkFlagAnnotationKey 标记框架自有 flags。
	commandFrameworkFlagAnnotationKey = "app.flag.framework"
)

// buildCommand 构建缓存命令树，并让全部节点回调读取同一个本次执行绑定。
func (this *App) buildCommand(binding *commandExecutionBinding) *cobra.Command {
	rootPath := []commandConfiguration{this.rootCommand}
	cmd := this.buildCommandNode(binding, rootPath, true)
	if this.output != nil {
		cmd.SetOut(this.output)
	}
	if this.errorOutput != nil {
		cmd.SetErr(this.errorOutput)
	}

	// 框架启动 flags 属于进程级输入，只有 root 声明且对子命令持久继承。
	cmd.PersistentFlags().StringP(configFlagName, "c", "", "Path of local config file.")
	cmd.PersistentFlags().StringArray(bootstrapFlagName, nil, "Bootstrap override in key=value form (repeatable).")
	cmd.PersistentFlags().String(bootstrapEnvPrefixFlagName, "", "Bootstrap env prefix (for example APP_).")
	cmd.PersistentFlags().Bool(bootstrapWithoutConfigFileFlagName, false, "Strictly disable local config file loading and discovery.")
	cmd.PersistentFlags().StringArray(overrideFlagName, nil, "Runtime override in key=value form (repeatable).")
	cmd.PersistentFlags().String(listBootstrapKeysFlagName, "", "Print supported bootstrap keys and quit. Optional mode: table|json.")
	cmd.PersistentFlags().Bool(versionFlagName, false, "Print version and quit.")
	markFrameworkFlags(cmd.PersistentFlags(), configFlagName, bootstrapFlagName, bootstrapEnvPrefixFlagName,
		bootstrapWithoutConfigFileFlagName, overrideFlagName, listBootstrapKeysFlagName, versionFlagName)
	if flag := cmd.PersistentFlags().Lookup(listBootstrapKeysFlagName); flag != nil {
		flag.NoOptDefVal = "table"
	}
	return cmd
}

// buildCommandNode 递归把一个语义节点转换为私有 Cobra 节点。
// 阶段角色：命令树只构建一次，FlagBinder.Bind 因此仍保持每个节点一次的调用次数。
func (this *App) buildCommandNode(
	binding *commandExecutionBinding,
	path []commandConfiguration,
	isRoot bool,
) *cobra.Command {
	node := path[len(path)-1]
	plan := buildExecutionPlan(path)
	command := &cobra.Command{
		Use:           node.Use,
		Short:         node.Short,
		Long:          node.Long,
		Example:       node.Example,
		Aliases:       append([]string{}, node.Aliases...),
		SilenceUsage:  isRoot,
		SilenceErrors: isRoot,
		RunE: func(cmd *cobra.Command, args []string) error {
			return this.runCommand(binding.currentSession(), plan, cmd, args)
		},
	}
	command.InitDefaultHelpFlag()
	setFrameworkFlag(command.Flags().Lookup("help"))
	if node.ValidateArgs != nil {
		command.Args = func(_ *cobra.Command, args []string) error {
			return node.ValidateArgs(append([]string{}, args...))
		}
	}

	// 节点业务 Flag 只在本节点声明；Complete/Validate 也只随选中节点执行。
	if node.FlagBinder != nil {
		node.FlagBinder.Bind(newPFlagCommandFlags(command.Flags()))
	}
	for _, child := range node.Commands {
		childPath := append(append([]commandConfiguration{}, path...), child.configuration)
		command.AddCommand(this.buildCommandNode(binding, childPath, false))
	}
	this.configureCommandHelp(command)
	return command
}
