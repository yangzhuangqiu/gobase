// Package appruntime 提供与命令行框架无关的应用运行时。
//
// 运行时统一托管 config、log 与 ioc 的构建和关闭，并把显式
// Beans/Modules 声明转换为 bean 注册动作。包内不依赖 app/Cobra，
// 可由命令行、测试或嵌入式入口复用。
package appruntime
