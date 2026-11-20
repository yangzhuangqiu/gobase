package webhertzobserve

import (
	"context"
	"strings"

	baseconfig "github.com/yangzhuangqiu/gobase/config"
	"github.com/yangzhuangqiu/gobase/errcode"
)

const (
	// DefaultConfigScope 是 Web Hertz 可观测模块读取统一启动快照的默认路径。
	DefaultConfigScope = "observability.webHertz"
)

// Config 定义一次 Runtime Build 使用的完整 Web Hertz 可观测策略快照。
//
// 语义:
// 1. 默认 scope 缺失时使用 DefaultConfig；
// 2. 自定义 scope 缺失时 fail-fast；
// 3. 配置只在 IOC Init 读取一次，不支持运行期热切换指标名、bucket 或 Trace 策略。
type Config struct {
	// Prometheus 定义指标合同、Histogram 分桶和独立抓取 Server。
	Prometheus PrometheusConfig `yaml:"prometheus"`
	// Tracing 定义已安装 OTel 能力的可序列化运行策略，不负责创建 Provider 或启用 OTel。
	Tracing TracingConfig `yaml:"tracing"`
}

// PrometheusConfig 定义 HTTP Server 指标的稳定命名、分桶和抓取端口策略。
type PrometheusConfig struct {
	// Namespace 是全部指标名的顶级前缀；默认 gobase，修改会改变 Dashboard/Alert 查询合同。
	Namespace string `yaml:"namespace"`
	// Subsystem 是 HTTP Server 指标的二级前缀；默认 http_server。
	Subsystem string `yaml:"subsystem"`
	// Buckets 定义耗时和 body size Histogram 的累计边界。
	Buckets PrometheusBucketsConfig `yaml:"buckets"`
	// Server 定义独立 Prometheus 抓取端口；listener 在 IOC Init 预绑定。
	Server MetricsServerConfig `yaml:"server"`
}

// PrometheusBucketsConfig 定义三类 Histogram 的有序正数边界。
type PrometheusBucketsConfig struct {
	// Duration 使用秒为单位；默认 prometheus.DefBuckets。
	Duration []float64 `yaml:"duration"`
	// RequestBytes 使用 byte 为单位；默认覆盖 128B 到约 2MB。
	RequestBytes []float64 `yaml:"requestBytes"`
	// ResponseBytes 使用 byte 为单位；默认覆盖 128B 到约 2MB。
	ResponseBytes []float64 `yaml:"responseBytes"`
}

// TracingConfig 定义已安装 OTel Hertz 插桩的可序列化启动策略。
type TracingConfig struct {
	// IgnoredPaths 是不创建 Server Span 的精确绝对路径；默认空集合。
	IgnoredPaths []string `yaml:"ignoredPaths"`
	// SamplingPolicyReference 校验 borrowed Provider 已冻结的 Sampler 策略；空值表示不校验。
	SamplingPolicyReference string `yaml:"samplingPolicyReference"`
}

// configLocator 保存 Module 注册前冻结的统一配置路径语义。
type configLocator struct {
	// configScope 是 IOC Init 阶段读取一次的 dotted path。
	configScope string
	// customConfigScope 标记非默认路径；缺失时必须 fail-fast。
	customConfigScope bool
}

// DefaultConfig 返回不依赖外部配置即可启动的完整安全快照。
func DefaultConfig() Config {
	return Config{
		Prometheus: defaultPrometheusConfig(),
		Tracing:    TracingConfig{},
	}
}

// resolveConfigLocator 把空路径归一化为默认 scope，并冻结缺失路径语义。
// 阶段角色：Module 注册前生成可跨 Container 重放的纯值定位策略。
func resolveConfigLocator(configScope string) configLocator {
	configScope = strings.TrimSpace(configScope)
	if configScope == "" {
		configScope = DefaultConfigScope
	}
	return configLocator{
		configScope:       configScope,
		customConfigScope: configScope != DefaultConfigScope,
	}
}

// loadConfig 从当前 Snapshot 读取、补齐并校验一次完整可观测启动快照。
// 阶段角色：默认 scope 缺失使用内建默认值，自定义 scope 缺失或任何字段非法时 fail-fast。
func loadConfig(ctx context.Context, manager baseconfig.Manager, locator configLocator) (Config, error) {
	if ctx == nil {
		return Config{}, errorForConfigLoadFailed(
			"op=app_observe_webhertz.config result=failed stage=precheck reason=context_required",
		)
	}
	if err := ctx.Err(); err != nil {
		return Config{}, errorForConfigLoadFailed(
			"op=app_observe_webhertz.config result=failed stage=precheck reason=context_done",
			err,
		)
	}

	// 第一阶段：独立 IOC 没有 ConfigManager 时仅允许默认 scope，并直接使用完整默认快照。
	input := DefaultConfig()
	if isNilValue(manager) {
		if locator.customConfigScope {
			return Config{}, errorForConfigLoadFailed(messagef(
				"op=app_observe_webhertz.config result=failed stage=precheck scope=%s reason=manager_required",
				locator.configScope,
			))
		}
		return resolveConfig(input)
	}
	snapshot := manager.Current()
	if snapshot == nil {
		return Config{}, errorForConfigLoadFailed(
			"op=app_observe_webhertz.config result=failed stage=current reason=snapshot_not_initialized",
		)
	}

	// 第二阶段：以完整默认值为 decode 基底，配置只覆盖显式提供的字段。
	if err := snapshot.Decode(locator.configScope, &input); err != nil {
		missingDefault := !locator.customConfigScope &&
			errcode.IsCode[baseconfig.ErrorCode](err, baseconfig.ErrCodeConfigDecodeScopeNotFound)
		if !missingDefault {
			return Config{}, errorForConfigLoadFailed(
				messagef(
					"op=app_observe_webhertz.config result=failed stage=decode scope=%s reason=decode_failed",
					locator.configScope,
				),
				err,
			)
		}
	}

	// 第三阶段：统一归一化和校验，禁止把错误延迟到 Collector、Tracer 或 listener 创建阶段。
	resolved, err := resolveConfig(input)
	if err != nil {
		return Config{}, errorForConfigLoadFailed(
			messagef(
				"op=app_observe_webhertz.config result=failed stage=validate scope=%s reason=config_invalid",
				locator.configScope,
			),
			err,
		)
	}
	return resolved, nil
}

// resolveConfig 归一化完整配置并复用各能力的领域校验器。
// 阶段角色：ConfigManager 与显式构造测试共享的纯值验证边界。
func resolveConfig(input Config) (Config, error) {
	prometheusConfig, err := resolvePrometheusConfig(input.Prometheus)
	if err != nil {
		return Config{}, err
	}
	tracingConfig, err := resolveTracingConfig(input.Tracing)
	if err != nil {
		return Config{}, err
	}
	return Config{Prometheus: prometheusConfig, Tracing: tracingConfig}, nil
}
