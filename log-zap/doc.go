// Package zap 提供不感知配置来源的 Zap 日志 Provider。
//
// Provider 保留强类型字段、Core.Check 与输出资源管理语义；宿主通过 RuntimeConfig
// 显式推送无需重建核心的运行期 level，并负责自己的配置订阅生命周期。
package zap
