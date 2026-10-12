// Package appwebhertz 提供标准 Middleware、应用 Controller/Filter 到 web-hertz 的配置化装配适配。
//
// 本包既支持显式值构造，也提供 IOC Module：容器在 Build 阶段自动注入 Controller、
// Filter 和配置 Manager，从同一 Snapshot 冻结 Server/Middleware，创建并托管一个声明为 web.Server 的单周期 Hertz Server。
package appwebhertz
