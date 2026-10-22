// Package appwebhertz 提供标准 Middleware、应用组件到 web-hertz 的配置化与 IOC 装配适配。
//
// 本包提供 IOC Module：容器在 Build 阶段自动注入 Controller、Filter、fallback handler 和配置 Manager，
// 从同一 Snapshot 冻结 Server/Middleware 并托管单周期 Hertz Server；非 IOC 场景直接使用 web-hertz。
package appwebhertz
