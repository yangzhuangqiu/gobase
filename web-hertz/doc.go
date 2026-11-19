// Package webhertz 提供 Hertz Server 到 web.StartupServer 的单周期生命周期与启动结果适配。
//
// 本包独立拥有 Hertz 启动配置和 Handler 完成合同，并直接保留 Hertz Middleware、Router 和
// Handler 编程模型；默认 bestEffort 保持有界尽力关闭，显式 required 以请求 gate 保护资源释放；
// 应用配置绑定、Controller/Filter 聚合及 hlog 安装由上层负责。
package webhertz
