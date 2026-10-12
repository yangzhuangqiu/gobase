package webhertzobserve

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/model"
	webhertz "github.com/yangzhuangqiu/gobase/web-hertz"
)

const (
	// defaultPrometheusNamespace 固定 gobase HTTP 指标的默认顶级命名空间。
	defaultPrometheusNamespace = "gobase"
	// defaultPrometheusSubsystem 固定 server 请求指标的默认子系统。
	defaultPrometheusSubsystem = "http_server"
)

var (
	// defaultRequestByteBuckets 覆盖 128B 到约 2MB 的常见请求体范围。
	defaultRequestByteBuckets = prometheus.ExponentialBuckets(128, 4, 8)
	// defaultResponseByteBuckets 覆盖 128B 到约 2MB 的常见响应体范围。
	defaultResponseByteBuckets = prometheus.ExponentialBuckets(128, 4, 8)
)

// PrometheusOption 定义 HTTP 指标命名和 Histogram bucket 的纯构造策略。
type PrometheusOption func(options *prometheusOptions) error

// prometheusOptions 保存一次 Collector 构造的不可变配置。
type prometheusOptions struct {
	// namespace 是全部 HTTP 指标的顶级名称。
	namespace string
	// subsystem 是 server 指标的子系统名称。
	subsystem string
	// durationBuckets 使用秒为单位。
	durationBuckets []float64
	// requestByteBuckets 使用 byte 为单位。
	requestByteBuckets []float64
	// responseByteBuckets 使用 byte 为单位。
	responseByteBuckets []float64
}

// PrometheusObserver 把完成态快照映射为有限基数 Prometheus 指标。
type PrometheusObserver struct {
	// requests 统计 method/route/status/outcome 组合的完成请求数。
	requests *prometheus.CounterVec
	// duration 观察完成请求耗时秒数。
	duration *prometheus.HistogramVec
	// requestBytes 观察已知请求体大小。
	requestBytes *prometheus.HistogramVec
	// responseBytes 观察已知响应体大小。
	responseBytes *prometheus.HistogramVec
	// inFlight 记录按归一化 Method 分组的当前请求数。
	inFlight *prometheus.GaugeVec
}

var (
	_ webhertz.RequestObserver          = (*PrometheusObserver)(nil)
	_ webhertz.RequestLifecycleObserver = (*PrometheusObserver)(nil)
	_ prometheus.Collector              = (*PrometheusObserver)(nil)
)

// WithPrometheusNamespace 设置指标 namespace；空白值回退 gobase。
func WithPrometheusNamespace(namespace string) PrometheusOption {
	return func(options *prometheusOptions) error {
		options.namespace = strings.TrimSpace(namespace)
		return nil
	}
}

// WithPrometheusSubsystem 设置指标 subsystem；空白值回退 http_server。
func WithPrometheusSubsystem(subsystem string) PrometheusOption {
	return func(options *prometheusOptions) error {
		options.subsystem = strings.TrimSpace(subsystem)
		return nil
	}
}

// WithDurationBuckets 设置请求耗时秒数 bucket；必须严格递增且全部大于零。
func WithDurationBuckets(buckets ...float64) PrometheusOption {
	return setPrometheusBuckets("duration", buckets, func(options *prometheusOptions, values []float64) { options.durationBuckets = values })
}

// WithRequestByteBuckets 设置请求体 byte bucket；必须严格递增且全部大于零。
func WithRequestByteBuckets(buckets ...float64) PrometheusOption {
	return setPrometheusBuckets("request_bytes", buckets, func(options *prometheusOptions, values []float64) { options.requestByteBuckets = values })
}

// WithResponseByteBuckets 设置响应体 byte bucket；必须严格递增且全部大于零。
func WithResponseByteBuckets(buckets ...float64) PrometheusOption {
	return setPrometheusBuckets("response_bytes", buckets, func(options *prometheusOptions, values []float64) { options.responseByteBuckets = values })
}

// setPrometheusBuckets 创建复制并校验 bucket 的 Option，防止调用方后续修改生效配置。
func setPrometheusBuckets(stage string, buckets []float64, assign func(*prometheusOptions, []float64)) PrometheusOption {
	return func(options *prometheusOptions) error {
		values := append([]float64{}, buckets...)
		if !validBuckets(values) {
			return buildError(ErrCodeDeclarationInvalid, messagef("op=app_observe_webhertz.prometheus result=failed stage=validate_%s_buckets reason=buckets_invalid", stage))
		}
		assign(options, values)
		return nil
	}
}

// NewPrometheusObserver 创建尚未注册到任何 Registry 的 HTTP Collector/Observer。
//
// 返回对象不拥有 Registry；调用方必须显式 Register，或使用 NewPrivatePrometheusRuntime/NewSharedPrometheusRuntime 管理注册生命周期。
func NewPrometheusObserver(options ...PrometheusOption) (*PrometheusObserver, error) {
	resolved, err := resolvePrometheusOptions(options)
	if err != nil {
		return nil, err
	}
	return newPrometheusObserver(resolved), nil
}

// newPrometheusObserver 从已冻结纯值配置创建尚未注册的 Collector，不重放 Option。
func newPrometheusObserver(resolved prometheusOptions) *PrometheusObserver {
	// 指标名、Help 和 Label 是外部 Dashboard/Alert 合同，集中在构造阶段冻结。
	return &PrometheusObserver{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: resolved.namespace, Subsystem: resolved.subsystem, Name: "requests_total", Help: "Total completed HTTP server requests.",
		}, []string{"method", "route", "status", "outcome"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: resolved.namespace, Subsystem: resolved.subsystem, Name: "request_duration_seconds", Help: "HTTP server request duration in seconds.", Buckets: resolved.durationBuckets,
		}, []string{"method", "route", "outcome"}),
		requestBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: resolved.namespace, Subsystem: resolved.subsystem, Name: "request_bytes", Help: "HTTP server request body size in bytes.", Buckets: resolved.requestByteBuckets,
		}, []string{"method", "route"}),
		responseBytes: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: resolved.namespace, Subsystem: resolved.subsystem, Name: "response_bytes", Help: "HTTP server response body size in bytes.", Buckets: resolved.responseByteBuckets,
		}, []string{"method", "route", "status"}),
		inFlight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: resolved.namespace, Subsystem: resolved.subsystem, Name: "in_flight_requests", Help: "Current in-flight HTTP server requests.",
		}, []string{"method"}),
	}
}

// RequestStarted 增加归一化 Method 对应的 in-flight Gauge。
func (this *PrometheusObserver) RequestStarted(_ context.Context, method string) {
	this.inFlight.WithLabelValues(method).Inc()
}

// Observe 更新完成数、耗时、已知大小并结束对应 in-flight 状态。
func (this *PrometheusObserver) Observe(_ context.Context, observation webhertz.HTTPRequestObservation) {
	status := strconv.Itoa(observation.Status)
	outcome := string(observation.Outcome)
	this.inFlight.WithLabelValues(observation.Method).Dec()
	this.requests.WithLabelValues(observation.Method, observation.RoutePattern, status, outcome).Inc()
	this.duration.WithLabelValues(observation.Method, observation.RoutePattern, outcome).Observe(observation.Duration.Seconds())
	if observation.RequestBytes != webhertz.UnknownBodySize {
		this.requestBytes.WithLabelValues(observation.Method, observation.RoutePattern).Observe(float64(observation.RequestBytes))
	}
	if observation.ResponseBytes != webhertz.UnknownBodySize {
		this.responseBytes.WithLabelValues(observation.Method, observation.RoutePattern, status).Observe(float64(observation.ResponseBytes))
	}
}

// Describe 实现 prometheus.Collector 并稳定发布全部 HTTP 指标描述。
func (this *PrometheusObserver) Describe(channel chan<- *prometheus.Desc) {
	this.requests.Describe(channel)
	this.duration.Describe(channel)
	this.requestBytes.Describe(channel)
	this.responseBytes.Describe(channel)
	this.inFlight.Describe(channel)
}

// Collect 实现 prometheus.Collector，从并发安全的 Vec 采集当前指标。
func (this *PrometheusObserver) Collect(channel chan<- prometheus.Metric) {
	this.requests.Collect(channel)
	this.duration.Collect(channel)
	this.requestBytes.Collect(channel)
	this.responseBytes.Collect(channel)
	this.inFlight.Collect(channel)
}

// resolvePrometheusOptions 校验全部 Option 并复制默认 bucket。
func resolvePrometheusOptions(options []PrometheusOption) (prometheusOptions, error) {
	resolved := prometheusOptions{
		namespace: defaultPrometheusNamespace, subsystem: defaultPrometheusSubsystem,
		durationBuckets:     append([]float64{}, prometheus.DefBuckets...),
		requestByteBuckets:  append([]float64{}, defaultRequestByteBuckets...),
		responseByteBuckets: append([]float64{}, defaultResponseByteBuckets...),
	}
	// 第一阶段：顺序执行 Option 并拒绝 nil，bucket slice 已由各 Option 复制。
	for index, option := range options {
		if option == nil {
			return prometheusOptions{}, buildError(ErrCodeDeclarationInvalid, messagef("op=app_observe_webhertz.prometheus result=failed stage=resolve_options reason=option_required option_index=%d", index))
		}
		if err := option(&resolved); err != nil {
			return prometheusOptions{}, err
		}
	}
	if resolved.namespace == "" {
		resolved.namespace = defaultPrometheusNamespace
	}
	if resolved.subsystem == "" {
		resolved.subsystem = defaultPrometheusSubsystem
	}
	// 第二阶段：在创建 Desc 前校验最终完整名称，避免错误延迟到 Registry Register。
	metricName := prometheus.BuildFQName(resolved.namespace, resolved.subsystem, "requests_total")
	if !model.IsValidMetricName(model.LabelValue(metricName)) {
		return prometheusOptions{}, buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.prometheus result=failed stage=validate_metric_name reason=name_invalid")
	}
	return resolved, nil
}

// validBuckets 验证 bucket 非空、正数且严格递增。
func validBuckets(values []float64) bool {
	if len(values) == 0 {
		return false
	}
	previous := float64(0)
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value <= previous {
			return false
		}
		previous = value
	}
	return true
}

// PrometheusRuntime owned Collector registration 并提供不拥有 Server 的 Handler。
type PrometheusRuntime struct {
	// observer 同时是完成态 Observer 和注册到 Registry 的 Collector。
	observer *PrometheusObserver
	// registry 在 shared 模式 borrowed，在 private 模式由本 Runtime owned。
	registry PrometheusRegistry
	// handler 只借用 registry 作为 Gatherer。
	handler http.Handler
	// closeOnce 保证 Collector 只注销一次。
	closeOnce sync.Once
	// closeErr 保存首次注销失败语义。
	closeErr error
}

var (
	_ PrometheusHandler                 = (*PrometheusRuntime)(nil)
	_ webhertz.RequestObserver          = (*PrometheusRuntime)(nil)
	_ webhertz.RequestLifecycleObserver = (*PrometheusRuntime)(nil)
)

// NewPrivatePrometheusRuntime 创建 owned-private Registry、Observer、注册和 Handler 原子组合。
func NewPrivatePrometheusRuntime(options ...PrometheusOption) (*PrometheusRuntime, error) {
	return newPrometheusRuntime(prometheus.NewRegistry(), options)
}

// NewSharedPrometheusRuntime 在 borrowed-shared Registry 中注册独占 Observer，并由 Runtime 负责注销自身 Collector。
func NewSharedPrometheusRuntime(registry PrometheusRegistry, options ...PrometheusOption) (*PrometheusRuntime, error) {
	if isNilValue(registry) {
		return nil, buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.prometheus result=failed stage=precheck reason=registry_required")
	}
	return newPrometheusRuntime(registry, options)
}

// newPrometheusRuntime 原子创建 Observer、注册 Collector，并在成功后发布 Handler。
func newPrometheusRuntime(registry PrometheusRegistry, options []PrometheusOption) (*PrometheusRuntime, error) {
	resolved, err := resolvePrometheusOptions(options)
	if err != nil {
		return nil, err
	}
	return newPrometheusRuntimeFromConfig(registry, resolved)
}

// newPrometheusRuntimeFromConfig 原子注册已冻结配置创建的 Observer，并在成功后发布 Handler。
func newPrometheusRuntimeFromConfig(registry PrometheusRegistry, resolved prometheusOptions) (*PrometheusRuntime, error) {
	if isNilValue(registry) {
		return nil, buildError(ErrCodeDeclarationInvalid, "op=app_observe_webhertz.prometheus result=failed stage=precheck reason=registry_required")
	}
	observer := newPrometheusObserver(resolved)
	if err := registry.Register(observer); err != nil {
		return nil, buildError(ErrCodeRegistrationFailed, "op=app_observe_webhertz.prometheus result=failed stage=register_collector reason=registry_rejected", err)
	}
	return &PrometheusRuntime{
		observer: observer,
		registry: registry,
		handler:  promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
	}, nil
}

// RequestStarted 委托 Observer 更新 in-flight Gauge。
func (this *PrometheusRuntime) RequestStarted(ctx context.Context, method string) {
	this.observer.RequestStarted(ctx, method)
}

// Observe 委托 Observer 更新完成态指标。
func (this *PrometheusRuntime) Observe(ctx context.Context, observation webhertz.HTTPRequestObservation) {
	this.observer.Observe(ctx, observation)
}

// ServeHTTP 通过当前 Runtime 的 private/shared Gatherer 暴露指标，不启动 HTTP Server。
func (this *PrometheusRuntime) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	this.handler.ServeHTTP(writer, request)
}

// Collector 返回已注册的 HTTP Collector，供测试和显式诊断装配读取身份。
func (this *PrometheusRuntime) Collector() prometheus.Collector {
	return this.observer
}

// Close 从 Registry 注销本 Runtime 的 Collector；不会关闭 borrowed Registry 或任何 HTTP Server。
func (this *PrometheusRuntime) Close() error {
	this.closeOnce.Do(func() {
		if !this.registry.Unregister(this.observer) {
			this.closeErr = buildError(ErrCodeRegistrationFailed, "op=app_observe_webhertz.prometheus result=failed stage=unregister_collector reason=collector_not_registered")
		}
	})
	return this.closeErr
}
