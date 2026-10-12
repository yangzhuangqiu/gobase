package hlogzap

import logzap "github.com/yangzhuangqiu/gobase/log-zap"

const (
	// ModeConsole 表示输出到标准输出。
	ModeConsole = logzap.ModeConsole
	// ModeFile 表示输出到文件。
	ModeFile = logzap.ModeFile
	// FormatJSON 表示 JSON 结构化日志。
	FormatJSON = logzap.FormatJSON
	// FormatText 表示 Console 文本日志。
	FormatText = logzap.FormatText
)

// RotateConfig 是 hlog-zap 集成层沿用的轮转配置模型。
//
// 语义：
// 1. 直接复用 log-zap 的字段与 yaml 标签；
// 2. 避免在 hlog 集成层维护重复配置语义。
type RotateConfig = logzap.RotateConfig

// Config 是 hlog-zap 集成层输入配置。
//
// 语义：
// 1. 直接复用 log-zap.Config，装配层可共享同一份 `log.zap` 配置；
// 2. hlog 集成层不再维护独立的 `log.hlog` 配置模型。
type Config = logzap.Config
