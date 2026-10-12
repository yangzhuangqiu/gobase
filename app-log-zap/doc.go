// Package applogzap 提供 config.Manager 与纯 log-zap Provider 的装配适配层。
//
// 该模块拥有日志 scope subscription（默认 log.zap），只向核心推送可热更新的 level，
// 并通过结构化 Build 契约接入 app-runtime，而不依赖 app-runtime 模块。
package applogzap
