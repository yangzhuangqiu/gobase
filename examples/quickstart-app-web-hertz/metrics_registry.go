package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	webhertzobserve "github.com/yangzhuangqiu/gobase/app-observe-webhertz"
	appruntime "github.com/yangzhuangqiu/gobase/app-runtime"
	"github.com/yangzhuangqiu/gobase/ioc"
)

const (
	// applicationMetricsModuleName 是 quickstart 私有 Registry 声明的稳定模块名。
	applicationMetricsModuleName = "quickstart-application-metrics"
	// applicationMetricsRegistryBeanName 是 IOC 注入 shared Registry 的稳定 bean 名称。
	applicationMetricsRegistryBeanName = "QuickstartApplicationMetricsRegistry"
)

// applicationMetricsConfig 显式选择进入应用 Registry 的非 HTTP Collector。
type applicationMetricsConfig struct {
	// IncludeGo 决定是否注册 Go runtime Collector；默认 false。
	IncludeGo bool
	// IncludeProcess 决定是否注册 Process Collector；默认 false。
	IncludeProcess bool
	// BusinessCollectors 是应用显式提供并由本 Registry 注册/注销的 Collector。
	BusinessCollectors []prometheus.Collector
}

// applicationMetricsRegistry 拥有 private Registry 和应用选择的 Collector registrations。
type applicationMetricsRegistry struct {
	// registry 从不指向 DefaultRegistry，由当前应用实例独占。
	registry *prometheus.Registry
	// config 是 Module 注册时复制的 Collector 选择。
	config applicationMetricsConfig
	// registered 保存本 bean 成功注册且应在 Close 注销的 Collector。
	registered []prometheus.Collector
}

var (
	_ webhertzobserve.PrometheusRegistry = (*applicationMetricsRegistry)(nil)
	_ ioc.Initializer                    = (*applicationMetricsRegistry)(nil)
	_ ioc.Closer                         = (*applicationMetricsRegistry)(nil)
)

// newApplicationMetricsModule 创建应用私有聚合 Registry 的可重放 Module 声明。
func newApplicationMetricsModule(config applicationMetricsConfig) appruntime.Module {
	resolved := config
	resolved.BusinessCollectors = append([]prometheus.Collector{}, config.BusinessCollectors...)
	return appruntime.NewModule(applicationMetricsModuleName, func(registry ioc.Registry) error {
		bean := &applicationMetricsRegistry{registry: prometheus.NewRegistry(), config: resolved}
		if err := registry.Register(
			bean,
			ioc.WithName(applicationMetricsRegistryBeanName),
			ioc.WithDeclare[webhertzobserve.PrometheusRegistry](),
		); err != nil {
			return fmt.Errorf("op=quickstart.metrics result=failed stage=register_bean reason=ioc_register_failed: %w", err)
		}
		return nil
	})
}

// Init 原子注册显式选择的 Go、Process 和业务 Collector。
func (this *applicationMetricsRegistry) Init(_ context.Context) error {
	if this == nil || this.registry == nil {
		return fmt.Errorf("op=quickstart.metrics result=failed stage=init reason=registry_required")
	}
	collectors := make([]prometheus.Collector, 0, 2+len(this.config.BusinessCollectors))
	if this.config.IncludeGo {
		collectors = append(collectors, prometheus.NewGoCollector())
	}
	if this.config.IncludeProcess {
		collectors = append(collectors, prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))
	}
	collectors = append(collectors, this.config.BusinessCollectors...)

	// 全部候选先校验 typed nil，再逐项注册；失败时逆序注销已成功项，避免半聚合 Registry 发布。
	for index, collector := range collectors {
		if isNilCollector(collector) {
			this.rollbackRegistered()
			return fmt.Errorf("op=quickstart.metrics result=failed stage=register_collectors reason=collector_required index=%d", index)
		}
		if err := this.registry.Register(collector); err != nil {
			this.rollbackRegistered()
			return fmt.Errorf("op=quickstart.metrics result=failed stage=register_collectors reason=registry_rejected index=%d: %w", index, err)
		}
		this.registered = append(this.registered, collector)
	}
	return nil
}

// Register 把后续 HTTP Collector 注册到当前应用私有 Registry。
func (this *applicationMetricsRegistry) Register(collector prometheus.Collector) error {
	return this.registry.Register(collector)
}

// MustRegister 把 Prometheus 的 panic 风格入口限定在当前应用私有 Registry。
func (this *applicationMetricsRegistry) MustRegister(collectors ...prometheus.Collector) {
	this.registry.MustRegister(collectors...)
}

// Unregister 从当前应用私有 Registry 注销调用方拥有的 Collector。
func (this *applicationMetricsRegistry) Unregister(collector prometheus.Collector) bool {
	return this.registry.Unregister(collector)
}

// Gather 聚合 HTTP、runtime 和显式业务 Collector，不访问 DefaultGatherer。
func (this *applicationMetricsRegistry) Gather() ([]*dto.MetricFamily, error) {
	return this.registry.Gather()
}

// Close 逆序注销本 bean 拥有的 Collector；HTTP Adapter 会因依赖关系先注销自身 Collector。
func (this *applicationMetricsRegistry) Close(_ context.Context) error {
	if this == nil || this.registry == nil {
		return nil
	}
	var errs []error
	for index := len(this.registered) - 1; index >= 0; index-- {
		if !this.registry.Unregister(this.registered[index]) {
			errs = append(errs, fmt.Errorf("op=quickstart.metrics result=failed stage=close reason=collector_not_registered index=%d", index))
		}
	}
	this.registered = nil
	return errors.Join(errs...)
}

// rollbackRegistered 补偿 Init 阶段已成功的 registration，并清空发布前状态。
func (this *applicationMetricsRegistry) rollbackRegistered() {
	for index := len(this.registered) - 1; index >= 0; index-- {
		this.registry.Unregister(this.registered[index])
	}
	this.registered = nil
}

// isNilCollector 识别 interface 中的 typed nil Collector 声明。
func isNilCollector(collector prometheus.Collector) bool {
	if collector == nil {
		return true
	}
	value := reflect.ValueOf(collector)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
