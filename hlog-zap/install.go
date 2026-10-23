package hlogzap

import (
	"os"
	"strings"
	"sync"

	"github.com/cloudwego/hertz/pkg/common/hlog"
	hertzzap "github.com/hertz-contrib/logger/zap"
	logzap "github.com/yangzhuangqiu/gobase/log-zap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	// hlogCallerSkip 是 hlog/Zap 适配包装层到业务调用点的固定跳层数。
	//
	// 语义：
	// 1. hlog 适配边界自行锁定 caller，不再把 skip 作为业务配置；
	// 2. 该值由 caller 测试约束，避免框架内部文件污染调用位置。
	hlogCallerSkip = 3
)

// Lease 定义进程级 Hertz hlog 安装的唯一恢复权。
//
// 语义:
// 1. Close 幂等恢复安装前的 DefaultLogger，并由 Hertz 重建配对的 SystemLogger；
// 2. Lease 不拥有、不关闭传入的 log-zap Provider；
// 3. Lease 有效期间不允许再次通过本包安装另一个借用 Logger。
type Lease interface {
	// Close 恢复安装前的 Hertz Logger；重复调用返回首次结果。
	Close() error
}

type providerLease struct {
	// previous 是安装前的默认 Logger；恢复时重新建立 Default/System 配对关系。
	previous hlog.FullLogger
	// closeOnce 保证全局恢复和 owner 释放只发生一次。
	closeOnce sync.Once
	// closeErr 保存首次恢复结果，供重复调用稳定返回。
	closeErr error
}

var globalLeaseState struct {
	// mu 串行保护 active 与 hlog.SetLogger 的安装/恢复边界。
	mu sync.Mutex
	// active 是当前唯一持有全局恢复权的租约。
	active *providerLease
}

// Install 是 Hertz 场景下替换 hlog 核心的唯一推荐入口。
//
// 典型调用场景:
// 1. 业务装配层在服务启动早期读取 `log.zap` 配置后调用一次；
// 2. 替换后，Hertz 框架内部日志与业务 logger 可共享同一日志语义。
//
// 返回:
// - *hertzzap.Logger: hertz-contrib logger 实例，供调用方按需执行 Sync 或高级定制。
func Install(config Config) (*hertzzap.Logger, error) {
	globalLeaseState.mu.Lock()
	defer globalLeaseState.mu.Unlock()
	if globalLeaseState.active != nil {
		return nil, errorForInstallConflict("op=hlog_zap.install result=failed stage=acquire reason=active_lease_exists")
	}
	logger, err := NewLogger(config)
	if err != nil {
		return nil, err
	}
	hlog.SetLogger(logger)
	return logger, nil
}

// InstallFromProvider 借用既有 log-zap Provider 并安装为 Hertz 进程级 hlog。
//
// 典型调用场景：应用日志 Provider 构造成功后、IOC Build 前安装，使 Hertz 构造和运行日志共享同一底层。
//
// 参数:
// - provider: 必填；生命周期必须覆盖 Lease，并在 Lease.Close 之后关闭。
//
// 返回:
// - Lease: 成功后持有唯一恢复权，调用方必须 Close；
// - error: Provider 缺失或已有活跃租约时不修改全局 hlog。
func InstallFromProvider(provider *logzap.Provider) (Lease, error) {
	// 第一阶段：先构建借用适配器；声明无效时不得触碰进程全局状态。
	logger, err := NewFromProvider(provider)
	if err != nil {
		return nil, err
	}

	// 第二阶段：在同一临界区内检查 owner、保存旧 Logger 并发布新 Lease。
	globalLeaseState.mu.Lock()
	defer globalLeaseState.mu.Unlock()
	if globalLeaseState.active != nil {
		return nil, errorForInstallConflict("op=hlog_zap.install result=failed stage=acquire reason=active_lease_exists")
	}
	lease := &providerLease{previous: hlog.DefaultLogger()}
	hlog.SetLogger(logger)
	globalLeaseState.active = lease
	return lease, nil
}

// Close 恢复安装前的 Default/System Logger 配对并释放全局租约。
// 阶段角色：必须在全部 Hertz Server 停止后、底层 Provider 关闭前执行。
func (this *providerLease) Close() error {
	if this == nil {
		return nil
	}
	this.closeOnce.Do(func() {
		// 恢复和 owner 释放必须原子完成，避免并行安装观察到中间状态。
		globalLeaseState.mu.Lock()
		defer globalLeaseState.mu.Unlock()
		if globalLeaseState.active != this {
			this.closeErr = errorForInstallConflict("op=hlog_zap.restore result=failed stage=release reason=lease_not_active")
			return
		}
		hlog.SetLogger(this.previous)
		globalLeaseState.active = nil
	})
	return this.closeErr
}

// NewLogger 基于统一 `log.zap` 配置构建 hlog 可用的 zap logger。
//
// 语义:
// 1. 入参直接复用 log-zap 配置对象；
// 2. 仅构建 logger，不修改全局 hlog 状态；
// 3. 默认值、合并与校验统一复用 log-zap ResolveConfig。
func NewLogger(config Config) (*hertzzap.Logger, error) {
	resolved, err := logzap.ResolveConfig(logzap.Config(config))
	if err != nil {
		return nil, err
	}
	level, err := parseZapLevel(resolved.Level)
	if err != nil {
		return nil, err
	}
	return buildLogger(resolved, level), nil
}

// buildLogger 构建 hertz-contrib zap logger。
//
// 流程:
// 1. 基于已解析配置构建 encoder/writeSyncer/level；
// 2. 应用 hlog 适配边界固定 caller；
// 3. 返回不修改全局状态的 hertzzap Logger。
func buildLogger(config logzap.Config, level zapcore.Level) *hertzzap.Logger {
	options := make([]hertzzap.Option, 0, 6)
	options = append(options, hertzzap.WithCoreEnc(buildEncoder(config.Format)))
	options = append(options, hertzzap.WithCoreWs(buildWriteSyncer(config)))
	options = append(options, hertzzap.WithCoreLevel(zap.NewAtomicLevelAt(level)))
	if !config.DisableCaller {
		zapOptions := []zap.Option{zap.AddCaller(), zap.AddCallerSkip(hlogCallerSkip)}
		options = append(options, hertzzap.WithZapOptions(zapOptions...))
	}
	return hertzzap.NewLogger(options...)
}

// buildWriteSyncer 构建日志输出目标（stdout 或 lumberjack 文件轮转）。
func buildWriteSyncer(config logzap.Config) zapcore.WriteSyncer {
	if config.Mode == ModeConsole {
		return zapcore.AddSync(os.Stdout)
	}
	rotator := &lumberjack.Logger{
		Filename:   config.Rotate.Filename,
		MaxSize:    config.Rotate.MaxSize,
		MaxBackups: config.Rotate.MaxBackups,
		MaxAge:     config.Rotate.MaxAge,
		Compress:   config.Rotate.Compress,
		LocalTime:  config.Rotate.LocalTime,
	}
	return zapcore.AddSync(rotator)
}

// buildEncoder 构建 json/text 编码器。
func buildEncoder(format string) zapcore.Encoder {
	config := zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		TimeKey:        "ts",
		NameKey:        "name",
		CallerKey:      "caller",
		FunctionKey:    "func",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	if format == FormatText {
		return zapcore.NewConsoleEncoder(config)
	}
	return zapcore.NewJSONEncoder(config)
}

// parseZapLevel 解析级别并映射到 zapcore.Level。
func parseZapLevel(raw string) (zapcore.Level, error) {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "", "info", "notice":
		return zapcore.InfoLevel, nil
	case "trace", "debug":
		// trace 仅作为 Hertz 既有配置的 Debug 兼容别名，不扩展 Zap 级别模型。
		return zapcore.DebugLevel, nil
	case "warn", "warning":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	default:
		return zapcore.InfoLevel, errorForLogLevelInvalid(messagef("invalid level=%s", raw))
	}
}
