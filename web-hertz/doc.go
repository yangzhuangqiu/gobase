// Package webhertz 提供 Hertz Server 到 web.Server 的生命周期适配。
//
// 本包独立拥有 Hertz 启动配置，并直接保留 Hertz Middleware、Router 和
// Handler 编程模型；应用配置绑定、Controller/Filter 聚合及 hlog 安装由上层负责。
package webhertz
