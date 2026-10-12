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

// buildCommand 构建 root 命令并挂载所有子命令。
func (this *App) buildCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:           this.name,
		Short:         this.description,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          this.runRootCommand,
	}
	cmd.InitDefaultHelpFlag()
	if this.output != nil {
		cmd.SetOut(this.output)
	}
	if this.errorOutput != nil {
		cmd.SetErr(this.errorOutput)
	}

	// 阶段一：声明稳定框架 flags，并标注 framework scope。
	cmd.PersistentFlags().StringP(configFlagName, "c", "", "Path of local config file.")
	cmd.PersistentFlags().StringArray(bootstrapFlagName, nil, "Bootstrap override in key=value form (repeatable).")
	cmd.PersistentFlags().String(bootstrapEnvPrefixFlagName, "", "Bootstrap env prefix (for example APP_).")
	cmd.PersistentFlags().Bool(bootstrapWithoutConfigFileFlagName, false, "Strictly disable local config file loading and discovery.")
	cmd.PersistentFlags().StringArray(overrideFlagName, nil, "Runtime override in key=value form (repeatable).")
	cmd.PersistentFlags().String(listBootstrapKeysFlagName, "", "Print supported bootstrap keys and quit. Optional mode: table|json.")
	cmd.PersistentFlags().Bool(versionFlagName, false, "Print version and quit.")
	markFrameworkFlags(cmd.PersistentFlags(), configFlagName, bootstrapFlagName, bootstrapEnvPrefixFlagName,
		bootstrapWithoutConfigFileFlagName, overrideFlagName, listBootstrapKeysFlagName, versionFlagName)
	setFrameworkFlag(cmd.Flags().Lookup("help"))
	if flag := cmd.PersistentFlags().Lookup(listBootstrapKeysFlagName); flag != nil {
		flag.NoOptDefVal = "table"
	}

	// 阶段二：绑定业务 flags、子命令和稳定 help 输出。
	if this.rootFlagBinder != nil {
		this.rootFlagBinder.Bind(newPFlagCommandFlags(cmd.Flags()))
	}
	for _, sub := range this.commands {
		cmd.AddCommand(this.buildSubCommand(sub))
	}
	this.configureCommandHelp(cmd)
	return cmd
}

// buildSubCommand 基于公共命令描述构建私有 Cobra 子命令。
func (this *App) buildSubCommand(sub Command) *cobra.Command {
	command := &cobra.Command{
		Use:     sub.Use,
		Short:   sub.Short,
		Long:    sub.Long,
		Example: sub.Example,
		Aliases: append([]string{}, sub.Aliases...),
		RunE: func(cmd *cobra.Command, args []string) error {
			return this.runSubCommand(sub, cmd, args)
		},
	}
	command.InitDefaultHelpFlag()
	setFrameworkFlag(command.Flags().Lookup("help"))
	if sub.ValidateArgs != nil {
		command.Args = func(_ *cobra.Command, args []string) error {
			return sub.ValidateArgs(append([]string{}, args...))
		}
	}
	if sub.FlagBinder != nil {
		sub.FlagBinder.Bind(newPFlagCommandFlags(command.Flags()))
	}
	this.configureCommandHelp(command)
	return command
}
