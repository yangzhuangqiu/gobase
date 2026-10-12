// Package global 提供拥有 ConfigManager 与组合日志 Provider 的零配置全局入口。
//
// 包初始化阶段按 ConfigManager、Provider 顺序获取资源，失败时降级为纯 Zap Provider；
// 调用方应在进程退出前调用 Close，并处理可按 app_log_zap.global.* 分支的关闭错误。
package global
