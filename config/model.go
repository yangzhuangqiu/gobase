package config

// SourceKind 表示最终配置值来自哪个来源层。
type SourceKind string

const (
	// SourceKindDefault 表示值来自进程内默认配置。
	SourceKindDefault SourceKind = "default"
	// SourceKindFile 表示值来自某个有序本地 YAML 文件层。
	SourceKindFile SourceKind = "file"
	// SourceKindRemote 表示值来自远端配置源。
	SourceKindRemote SourceKind = "remote"
	// SourceKindPinned 表示值来自调用方显式固定的最高优先级配置。
	SourceKindPinned SourceKind = "pinned"
)

const (
	// BootstrapRootKey 是配置文件中启动配置块的根 key。
	BootstrapRootKey = "bootstrap"
	// BootstrapKeyConfigFile 是启动阶段使用的本地配置文件路径 key。
	BootstrapKeyConfigFile = "config"
	// BootstrapKeyConfigFileWatch 是本地配置文件监听 bootstrap 子块根 key（仅配置文件生效）。
	BootstrapKeyConfigFileWatch = "configFile"
	// BootstrapKeyConfigFileWatchInterval 是本地配置文件监听轮询间隔。
	//
	// 语义：
	// 1. 仅在启用 File(FileWatch()) 且未显式传入 interval 参数时使用；
	// 2. 仅允许来自配置文件 bootstrap，不接受 env/override/remote 覆盖；
	// 3. 可解析常用单位：ms（毫秒）/s（秒）/min（分），无单位按秒；
	// 4. 合法区间为 [1s,60min]，缺失或非法时回退默认值（30s）。
	BootstrapKeyConfigFileWatchInterval = "configFile.watchInterval"
)

// ValueMeta 描述某个扁平 key（dotted key）的来源元信息。
type ValueMeta struct {
	// Source 是归一化来源类别，用于诊断 default/file/remote/pinned 覆盖关系。
	Source SourceKind
	// Layer 是具体来源层标识；remote 通常包含 provider/source 定位信息。
	Layer string
	// Priority 是合并阶段实际使用的层优先级，数值越大覆盖顺序越后。
	Priority int
	// Pinned 表示该值来自不可被 refresh 覆盖的最终固定层。
	Pinned bool
	// Version 是该元信息所属 Snapshot 的版本，与 Snapshot.Version 一致。
	Version uint64
}

// ChangeKind 表示一个 dotted key 在相邻 Snapshot 之间的状态迁移。
type ChangeKind string

const (
	// ChangeKindAdded 表示 key 从不存在变为存在，包括新增值为 nil 的场景。
	ChangeKindAdded ChangeKind = "added"
	// ChangeKindUpdated 表示 key 在两个版本中都存在，但值或来源语义发生变化。
	ChangeKindUpdated ChangeKind = "updated"
	// ChangeKindRemoved 表示 key 从存在变为不存在，包括旧值为 nil 的场景。
	ChangeKindRemoved ChangeKind = "removed"
)

// Change 描述两个版本快照之间某个 dotted key 的差异。
type Change struct {
	// Key 是发生新增、修改或删除的 dotted 配置路径。
	Key string
	// Kind 明确区分新增、修改和删除，避免 nil 值与 key 不存在产生歧义。
	Kind ChangeKind
	// Old 是上一 Snapshot 的深拷贝值；Kind=added 时为 nil。
	Old any
	// New 是当前 Snapshot 的深拷贝值；Kind=removed 时为 nil。
	New any
	// OldMeta 是 Old 所属来源元信息；Kind=added 时为零值。
	OldMeta ValueMeta
	// NewMeta 是 New 所属来源元信息；Kind=removed 时为零值。
	NewMeta ValueMeta
}
