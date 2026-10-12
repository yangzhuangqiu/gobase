// Package app 提供不泄漏 Cobra/Pflag 公共模型的命令行应用启动门面。
//
// 普通用户通过 Run/Main、WithRunner 与高频 Runtime Option 完成启动；
// config、log、ioc 的资源所有权由 app-runtime 承担。Cobra/Pflag 仅作为包内私有适配实现。
package app
