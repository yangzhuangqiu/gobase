package zap

import (
	"strings"

	"go.uber.org/zap/zapcore"
)

const (
	// ModeConsole 表示输出到标准输出。
	ModeConsole = "console"
	// ModeFile 表示输出到文件。
	ModeFile = "file"
	// FormatJSON 表示 JSON 结构化日志。
	FormatJSON = "json"
	// FormatText 表示 Console 文本日志。
	FormatText = "text"

	defaultRotateMaxSize = 10
	defaultRotateBackups = 50
	defaultRotateMaxAge  = 3
)

// RotateConfig 定义文件输出场景下的轮转参数。
type RotateConfig struct {
	// Filename 是日志文件路径（mode=file 时必填）。
	Filename string `yaml:"filename"`
	// MaxSize 是单个日志文件最大尺寸（MB）。
	MaxSize int `yaml:"maxSize"`
	// MaxBackups 是保留历史文件数量上限。
	MaxBackups int `yaml:"maxBackups"`
	// MaxAge 是历史文件最大保留天数。
	MaxAge int `yaml:"maxAge"`
	// Compress 控制历史文件是否压缩。
	Compress bool `yaml:"compress"`
	// LocalTime 控制轮转时间是否使用本地时区。
	LocalTime bool `yaml:"localTime"`
}

// Config 是 zap provider 的启动配置。
//
// 语义：
// 1. 字段可直接通过 YAML 绑定；
// 2. provider 在初始化阶段统一做默认值回填与校验。
type Config struct {
	// Level 是最低输出级别；默认 info，运行期允许动态更新。
	Level string `yaml:"level"`
	// Mode 是输出模式；默认 console，可选 console/file。
	Mode string `yaml:"mode"`
	// Format 可选值：json/text。
	Format string `yaml:"format"`
	// DisableCaller 控制是否关闭 caller 信息。
	DisableCaller bool `yaml:"disableCaller"`
	// Rotate 定义文件模式下的轮转策略。
	Rotate RotateConfig `yaml:"rotate"`
}

// RuntimeConfig 定义 Provider 运行期允许原子更新的窄配置契约。
//
// 语义：
// 1. 仅包含无需重建输出资源即可安全更新的字段；
// 2. mode/format/rotate 等启动配置不属于该契约；
// 3. 调用方应由自己的配置来源适配层构造该值，Provider 不感知 ConfigManager。
type RuntimeConfig struct {
	// Level 是新的最低输出级别；空值表示保持当前级别不变。
	Level string
}

// DefaultConfig 返回 zap provider 推荐默认配置。
//
// 语义：
// 1. 调用方可按需覆盖必要字段，其余字段复用默认值；
// 2. 默认值采用“开箱可观测”策略：info + console + json + caller。
func DefaultConfig() Config {
	return Config{
		Level:         "info",
		Mode:          ModeConsole,
		Format:        FormatJSON,
		DisableCaller: false,
		Rotate: RotateConfig{
			MaxSize:    defaultRotateMaxSize,
			MaxBackups: defaultRotateBackups,
			MaxAge:     defaultRotateMaxAge,
		},
	}
}

// ResolveConfig 把调用方输入与默认值合并，并完成标准化与校验。
//
// 流程：
// 1. 以 DefaultConfig 为基线；
// 2. 合并调用方显式输入；
// 3. 标准化并校验 level/mode/format/file 必填项。
func ResolveConfig(input Config) (Config, error) {
	result := DefaultConfig()
	mergeConfig(&result, input)
	return normalizeConfig(result)
}

// mergeConfig 把输入配置覆盖到默认配置上。
//
// 流程：
// 1. 仅合并调用方显式提供的字段，避免覆盖默认值；
// 2. bool 字段按直接赋值语义处理；
// 3. rotate 子配置按字段级别合并。
func mergeConfig(target *Config, input Config) {
	if target == nil {
		return
	}
	if strings.TrimSpace(input.Level) != "" {
		target.Level = input.Level
	}
	if strings.TrimSpace(input.Mode) != "" {
		target.Mode = input.Mode
	}
	if strings.TrimSpace(input.Format) != "" {
		target.Format = input.Format
	}
	target.DisableCaller = input.DisableCaller
	if strings.TrimSpace(input.Rotate.Filename) != "" {
		target.Rotate.Filename = input.Rotate.Filename
	}
	if input.Rotate.MaxSize > 0 {
		target.Rotate.MaxSize = input.Rotate.MaxSize
	}
	if input.Rotate.MaxBackups > 0 {
		target.Rotate.MaxBackups = input.Rotate.MaxBackups
	}
	if input.Rotate.MaxAge > 0 {
		target.Rotate.MaxAge = input.Rotate.MaxAge
	}
	target.Rotate.Compress = input.Rotate.Compress
	target.Rotate.LocalTime = input.Rotate.LocalTime
}

// normalizeConfig 标准化并校验配置。
func normalizeConfig(input Config) (Config, error) {
	level, err := normalizeLevel(input.Level)
	if err != nil {
		return Config{}, err
	}
	mode, err := normalizeMode(input.Mode)
	if err != nil {
		return Config{}, err
	}
	format, err := normalizeFormat(input.Format)
	if err != nil {
		return Config{}, err
	}
	rotate := input.Rotate
	rotate.Filename = strings.TrimSpace(rotate.Filename)
	if rotate.MaxSize <= 0 {
		rotate.MaxSize = defaultRotateMaxSize
	}
	if rotate.MaxBackups <= 0 {
		rotate.MaxBackups = defaultRotateBackups
	}
	if rotate.MaxAge <= 0 {
		rotate.MaxAge = defaultRotateMaxAge
	}
	if mode == ModeFile && rotate.Filename == "" {
		return Config{}, errorForLogFileFilenameRequired(messagef("mode=file requires rotate.filename"))
	}
	return Config{
		Level:         level,
		Mode:          mode,
		Format:        format,
		DisableCaller: input.DisableCaller,
		Rotate:        rotate,
	}, nil
}

// normalizeRuntimeConfig 解析运行期窄配置。
// 阶段角色：校验无需重建输出资源即可动态变更的 level。
func normalizeRuntimeConfig(input RuntimeConfig) (string, bool, error) {
	level := strings.TrimSpace(input.Level)
	if level == "" {
		return "", false, nil
	}
	normalizedLevel, err := normalizeLevel(level)
	if err != nil {
		return "", false, err
	}
	return normalizedLevel, true, nil
}

// normalizeLevel 标准化级别字符串并校验合法性。
func normalizeLevel(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return "info", nil
	}
	if _, err := parseZapLevel(raw); err != nil {
		return "", err
	}
	return raw, nil
}

// normalizeMode 标准化输出模式并校验合法性。
func normalizeMode(raw string) (string, error) {
	mode := strings.TrimSpace(strings.ToLower(raw))
	if mode == "" {
		mode = ModeConsole
	}
	switch mode {
	case ModeConsole, ModeFile:
		return mode, nil
	default:
		return "", errorForLogModeInvalid(messagef("invalid mode=%s", raw))
	}
}

// normalizeFormat 标准化编码格式并校验合法性。
func normalizeFormat(raw string) (string, error) {
	format := strings.TrimSpace(strings.ToLower(raw))
	if format == "" {
		format = FormatJSON
	}
	switch format {
	case FormatJSON, FormatText:
		return format, nil
	default:
		return "", errorForLogFormatInvalid(messagef("invalid format=%s", raw))
	}
}

// parseZapLevel 解析级别字符串并映射到 zapcore.Level。
func parseZapLevel(raw string) (zapcore.Level, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", "info", "notice":
		return zapcore.InfoLevel, nil
	case "trace", "debug":
		// trace 仅作为既有配置的 Debug 兼容别名；根日志契约不承诺独立 Trace 级别。
		return zapcore.DebugLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	default:
		return zapcore.InfoLevel, errorForLogLevelInvalid(messagef("invalid level=%s", raw))
	}
}
