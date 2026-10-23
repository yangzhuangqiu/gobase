// Package apphlogzap 装饰既有应用 Logging，提供 app Runtime 的统一 Hertz 日志资源。
//
// 该子包通过 ZapProvider 窄能力让业务 LoggerFactory 与 Hertz 全局 hlog 共享同一个 Zap Provider，
// 并固定 IOC/Server 停止、hlog 恢复、Provider 关闭的逆序释放合同。
package apphlogzap
