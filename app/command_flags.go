package app

import (
	"time"

	"github.com/spf13/pflag"
)

// CommandFlags 定义业务 Flag 声明的抽象绑定接口。
//
// 语义：
// 1. 对外屏蔽 pflag/cobra，业务层通过该接口声明命令参数；
// 2. Config* 声明进入配置覆盖链的 Flag；无前缀方法声明只影响当前命令行为的普通 Flag；
// 3. app 在内部把该接口适配到具体 CLI 框架。
type CommandFlags interface {
	// Config* 声明“配置参数”，只声明 key/type，不绑定业务变量；显式赋值后会进入 pinned 层。
	// ConfigString 声明 string 类型的配置覆盖 Flag。
	ConfigString(name string, usage string)
	// ConfigInt 声明 int 类型的配置覆盖 Flag。
	ConfigInt(name string, usage string)
	// ConfigInt64 声明 int64 类型的配置覆盖 Flag。
	ConfigInt64(name string, usage string)
	// ConfigUint 声明 uint 类型的配置覆盖 Flag。
	ConfigUint(name string, usage string)
	// ConfigUint64 声明 uint64 类型的配置覆盖 Flag。
	ConfigUint64(name string, usage string)
	// ConfigBool 声明 bool 类型的配置覆盖 Flag。
	ConfigBool(name string, usage string)
	// ConfigFloat64 声明 float64 类型的配置覆盖 Flag。
	ConfigFloat64(name string, usage string)
	// ConfigDuration 声明 time.Duration 类型的配置覆盖 Flag。
	ConfigDuration(name string, usage string)
	// ConfigStringArray 声明可重复 string 数组类型的配置覆盖 Flag。
	ConfigStringArray(name string, usage string)

	// 无前缀方法声明“命令参数”，绑定业务变量且不进入 pinned 层。
	// String 声明并绑定 string 类型的普通业务 Flag。
	String(name string, target *string, defaultValue string, usage string)
	// Int 声明并绑定 int 类型的普通业务 Flag。
	Int(name string, target *int, defaultValue int, usage string)
	// Int64 声明并绑定 int64 类型的普通业务 Flag。
	Int64(name string, target *int64, defaultValue int64, usage string)
	// Uint 声明并绑定 uint 类型的普通业务 Flag。
	Uint(name string, target *uint, defaultValue uint, usage string)
	// Uint64 声明并绑定 uint64 类型的普通业务 Flag。
	Uint64(name string, target *uint64, defaultValue uint64, usage string)
	// Bool 声明并绑定 bool 类型的普通业务 Flag。
	Bool(name string, target *bool, defaultValue bool, usage string)
	// Float64 声明并绑定 float64 类型的普通业务 Flag。
	Float64(name string, target *float64, defaultValue float64, usage string)
	// Duration 声明并绑定 time.Duration 类型的普通业务 Flag。
	Duration(name string, target *time.Duration, defaultValue time.Duration, usage string)
	// StringArray 声明并绑定可重复 string 数组类型的普通业务 Flag。
	StringArray(name string, target *[]string, defaultValue []string, usage string)
}

type pflagCommandFlags struct {
	fs *pflag.FlagSet
}

// newPFlagCommandFlags 创建仅在 app 内部暴露 Pflag 的业务 Flag 适配器。
func newPFlagCommandFlags(fs *pflag.FlagSet) CommandFlags {
	return pflagCommandFlags{fs: fs}
}

// ConfigString 注册 string 配置 Flag 并标记其进入 pinned 收集阶段。
func (this pflagCommandFlags) ConfigString(name string, usage string) {
	var value string
	this.fs.StringVar(&value, name, "", usage)
	this.markFlagScope(name, commandFlagScopeConfig)
}

// ConfigInt 注册 int 配置 Flag 并标记其进入 pinned 收集阶段。
func (this pflagCommandFlags) ConfigInt(name string, usage string) {
	var value int
	this.fs.IntVar(&value, name, 0, usage)
	this.markFlagScope(name, commandFlagScopeConfig)
}

// ConfigInt64 注册 int64 配置 Flag 并标记其进入 pinned 收集阶段。
func (this pflagCommandFlags) ConfigInt64(name string, usage string) {
	var value int64
	this.fs.Int64Var(&value, name, 0, usage)
	this.markFlagScope(name, commandFlagScopeConfig)
}

// ConfigUint 注册 uint 配置 Flag 并标记其进入 pinned 收集阶段。
func (this pflagCommandFlags) ConfigUint(name string, usage string) {
	var value uint
	this.fs.UintVar(&value, name, 0, usage)
	this.markFlagScope(name, commandFlagScopeConfig)
}

// ConfigUint64 注册 uint64 配置 Flag 并标记其进入 pinned 收集阶段。
func (this pflagCommandFlags) ConfigUint64(name string, usage string) {
	var value uint64
	this.fs.Uint64Var(&value, name, 0, usage)
	this.markFlagScope(name, commandFlagScopeConfig)
}

// ConfigBool 注册 bool 配置 Flag 并标记其进入 pinned 收集阶段。
func (this pflagCommandFlags) ConfigBool(name string, usage string) {
	var value bool
	this.fs.BoolVar(&value, name, false, usage)
	this.markFlagScope(name, commandFlagScopeConfig)
}

// ConfigFloat64 注册 float64 配置 Flag 并标记其进入 pinned 收集阶段。
func (this pflagCommandFlags) ConfigFloat64(name string, usage string) {
	var value float64
	this.fs.Float64Var(&value, name, 0, usage)
	this.markFlagScope(name, commandFlagScopeConfig)
}

// ConfigDuration 注册 duration 配置 Flag 并标记其进入 pinned 收集阶段。
func (this pflagCommandFlags) ConfigDuration(name string, usage string) {
	var value time.Duration
	this.fs.DurationVar(&value, name, 0, usage)
	this.markFlagScope(name, commandFlagScopeConfig)
}

// ConfigStringArray 注册 string 数组配置 Flag 并标记其进入 pinned 收集阶段。
func (this pflagCommandFlags) ConfigStringArray(name string, usage string) {
	var value []string
	this.fs.StringArrayVar(&value, name, nil, usage)
	this.markFlagScope(name, commandFlagScopeConfig)
}

// String 注册并绑定 string 普通业务 Flag。
func (this pflagCommandFlags) String(name string, target *string, defaultValue string, usage string) {
	this.fs.StringVar(target, name, defaultValue, usage)
	this.markFlagScope(name, commandFlagScopeFlag)
}

// Int 注册并绑定 int 普通业务 Flag。
func (this pflagCommandFlags) Int(name string, target *int, defaultValue int, usage string) {
	this.fs.IntVar(target, name, defaultValue, usage)
	this.markFlagScope(name, commandFlagScopeFlag)
}

// Int64 注册并绑定 int64 普通业务 Flag。
func (this pflagCommandFlags) Int64(name string, target *int64, defaultValue int64, usage string) {
	this.fs.Int64Var(target, name, defaultValue, usage)
	this.markFlagScope(name, commandFlagScopeFlag)
}

// Uint 注册并绑定 uint 普通业务 Flag。
func (this pflagCommandFlags) Uint(name string, target *uint, defaultValue uint, usage string) {
	this.fs.UintVar(target, name, defaultValue, usage)
	this.markFlagScope(name, commandFlagScopeFlag)
}

// Uint64 注册并绑定 uint64 普通业务 Flag。
func (this pflagCommandFlags) Uint64(name string, target *uint64, defaultValue uint64, usage string) {
	this.fs.Uint64Var(target, name, defaultValue, usage)
	this.markFlagScope(name, commandFlagScopeFlag)
}

// Bool 注册并绑定 bool 普通业务 Flag。
func (this pflagCommandFlags) Bool(name string, target *bool, defaultValue bool, usage string) {
	this.fs.BoolVar(target, name, defaultValue, usage)
	this.markFlagScope(name, commandFlagScopeFlag)
}

// Float64 注册并绑定 float64 普通业务 Flag。
func (this pflagCommandFlags) Float64(name string, target *float64, defaultValue float64, usage string) {
	this.fs.Float64Var(target, name, defaultValue, usage)
	this.markFlagScope(name, commandFlagScopeFlag)
}

// Duration 注册并绑定 duration 普通业务 Flag。
func (this pflagCommandFlags) Duration(name string, target *time.Duration, defaultValue time.Duration, usage string) {
	this.fs.DurationVar(target, name, defaultValue, usage)
	this.markFlagScope(name, commandFlagScopeFlag)
}

// StringArray 注册并绑定可重复 string 数组普通业务 Flag。
func (this pflagCommandFlags) StringArray(name string, target *[]string, defaultValue []string, usage string) {
	this.fs.StringArrayVar(target, name, defaultValue, usage)
	this.markFlagScope(name, commandFlagScopeFlag)
}
