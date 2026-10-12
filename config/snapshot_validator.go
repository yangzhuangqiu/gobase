package config

// SnapshotValidator 定义快照发布前校验契约。
//
// 语义：
// 1. 在 initial install 与 refresh publish 前执行；
// 2. 返回非 nil error 时拒绝发布新快照，保留旧快照。
type SnapshotValidator func(snapshot Snapshot) error
