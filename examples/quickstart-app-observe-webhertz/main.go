package main

import (
	"fmt"
	"os"
	"strings"

	baseapp "github.com/yangzhuangqiu/gobase/app"
	applogzap "github.com/yangzhuangqiu/gobase/app-log-zap"
	webhertzobserve "github.com/yangzhuangqiu/gobase/app-observe-webhertz"
	appwebhertz "github.com/yangzhuangqiu/gobase/app-web-hertz"
	baseconfig "github.com/yangzhuangqiu/gobase/config"
	apphlogzap "github.com/yangzhuangqiu/gobase/hlog-zap/app"
	"github.com/yangzhuangqiu/gobase/ioc"
	"go.opentelemetry.io/otel/propagation"
)

const (
	// businessAddressEnv 是业务 Hertz Server 监听地址的可选环境变量。
	businessAddressEnv = "APP_OBSERVE_WEBHERTZ_ADDRESS"
	// metricsAddressEnv 是独立 Metrics 抓取端口的可选环境变量。
	metricsAddressEnv = "APP_OBSERVE_WEBHERTZ_METRICS_ADDRESS"
	// defaultBusinessAddress 是本地教学用业务端口。
	defaultBusinessAddress = "127.0.0.1:18082"
	// defaultMetricsAddress 是本地教学用 Metrics 端口，不应直接暴露到公网。
	defaultMetricsAddress = "127.0.0.1:19082"
	// alwaysOnSamplingPolicy 是 Provider owner 与 adapter 共同校验的稳定采样策略引用。
	alwaysOnSamplingPolicy = "always_on"
)

// main 是高级 Web 可观测性示例入口，统一向 stderr 报告最终失败。
func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "quickstart-app-observe-webhertz failed: %v\n", err)
		os.Exit(1)
	}
}

// run 组合应用拥有的 Provider、观测 adapter、Hertz Server 与复合 Workload。
func run() error {
	return baseapp.Run(
		baseapp.WithName("quickstart-app-observe-webhertz"),
		baseapp.WithDescription("app-web-hertz metrics and tracing advanced quickstart"),
		baseapp.WithConfig(
			baseconfig.File(baseconfig.FileDisabled()),
			baseconfig.Defaults(defaultConfig()),
		),
		baseapp.WithLogging(apphlogzap.Wrap(applogzap.New())),
		baseapp.WithBeanDefinitions(applicationDefinitions()...),
		baseapp.WithModules(observationModule(), appwebhertz.NewModule()),
		baseapp.WithWorkloads(webhertzobserve.Workload()),
	)
}

// applicationDefinitions 声明业务 Controller、应用 owned TracerProvider 与显式传播器。
func applicationDefinitions() []ioc.Definition {
	return []ioc.Definition{
		appwebhertz.DefineController(&observabilityController{}),
		appwebhertz.DefineController(&healthController{}),
		ioc.Define(&traceProviderOwner{}, ioc.WithDeclare[webhertzobserve.TracerProvider]()),
		ioc.Define(propagation.TraceContext{}, ioc.WithDeclare[propagation.TextMapPropagator]()),
	}
}

// observationModule 声明私有 Prometheus Registry 和借用应用 Provider 的 OTel 插桩。
func observationModule() webhertzobserve.Module {
	return webhertzobserve.NewModule(
		webhertzobserve.WithPrivatePrometheus(
			webhertzobserve.WithPrometheusNamespace("quickstart"),
		),
		webhertzobserve.WithMetricsServer(),
		webhertzobserve.WithOpenTelemetry(
			webhertzobserve.WithSamplingPolicyReference(alwaysOnSamplingPolicy),
			webhertzobserve.WithTracingIgnoredPaths("/healthz"),
		),
	)
}

// defaultConfig 返回业务 Server 的最低优先级配置；观测 adapter 保持对象化声明，不进入 YAML scope。
func defaultConfig() map[string]any {
	return map[string]any{
		"log": map[string]any{
			"zap": map[string]any{
				"level":  "info",
				"mode":   "console",
				"format": "json",
			},
		},
		"web": map[string]any{
			"hertz": map[string]any{
				"address": loadBusinessAddress(),
				"middleware": map[string]any{
					"recovery":  map[string]any{"enabled": true, "response": "json"},
					"requestId": map[string]any{"enabled": true, "header": "X-Request-ID"},
					"accessLog": map[string]any{
						"enabled":       true,
						"skipPaths":     []string{"/healthz"},
						"slowThreshold": "500ms",
					},
				},
			},
		},
		"observability": map[string]any{
			"metrics": map[string]any{
				"server": map[string]any{
					"address":           loadMetricsAddress(),
					"path":              "/metrics",
					"readHeaderTimeout": "3s",
				},
			},
		},
	}
}

// loadBusinessAddress 读取并清理业务监听地址，空值回退固定教学端口。
func loadBusinessAddress() string {
	return loadAddress(businessAddressEnv, defaultBusinessAddress)
}

// loadMetricsAddress 读取并清理 Metrics 监听地址，空值回退独立教学端口。
func loadMetricsAddress() string {
	return loadAddress(metricsAddressEnv, defaultMetricsAddress)
}

// loadAddress 统一环境覆盖规则，避免业务和 Metrics 地址产生不同空白语义。
func loadAddress(environmentName string, fallback string) string {
	address := strings.TrimSpace(os.Getenv(environmentName))
	if address == "" {
		return fallback
	}
	return address
}
