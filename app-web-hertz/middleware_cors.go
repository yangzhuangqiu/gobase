package appwebhertz

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

const (
	// defaultCORSMaxAge 是浏览器缓存成功预检结果的默认时间。
	defaultCORSMaxAge = 12 * time.Hour
)

var (
	// defaultCORSMethods 是未显式声明时允许的安全常用 Method 集合。
	defaultCORSMethods = []string{"GET", "HEAD", "POST", "OPTIONS"}
	// defaultCORSHeaders 是未显式声明时允许浏览器发送的基础 Header 集合。
	defaultCORSHeaders = []string{"Accept", "Content-Type", "Origin"}
)

// CORSConfig 描述静态跨域策略；默认关闭且不隐式放开任意 Origin。
type CORSConfig struct {
	// Enabled 决定是否安装 CORS；默认 false。
	Enabled bool `yaml:"enabled"`
	// AllowOrigins 是允许的 http/https Origin 精确值；"*" 表示任意 Origin。
	AllowOrigins []string `yaml:"allowOrigins"`
	// AllowMethods 是预检响应允许的 HTTP Method；空值使用安全常用集合。
	AllowMethods []string `yaml:"allowMethods"`
	// AllowHeaders 是预检响应允许客户端发送的 Header；空值使用基础集合。
	AllowHeaders []string `yaml:"allowHeaders"`
	// ExposeHeaders 是普通跨域响应允许浏览器读取的 Header。
	ExposeHeaders []string `yaml:"exposeHeaders"`
	// AllowCredentials 决定是否允许 Cookie、Authorization 等凭据；不能与 "*" Origin 组合。
	AllowCredentials bool `yaml:"allowCredentials"`
	// MaxAge 是预检结果缓存时间；零值使用 12h，负值非法。
	MaxAge time.Duration `yaml:"maxAge"`
}

// resolveCORSConfig 校验静态白名单的安全组合并归一化 Method/Header。
func resolveCORSConfig(input CORSConfig) (CORSConfig, error) {
	// 第一阶段：先固定容量与 Origin 安全边界，通配符凭据组合不得进入 Handler 构造。
	if input.MaxAge < 0 {
		return CORSConfig{}, errorForConfigInvalid("op=app_web_hertz.resolve_config result=failed stage=validate_cors reason=max_age_negative")
	}
	if input.MaxAge == 0 {
		input.MaxAge = defaultCORSMaxAge
	}
	origins, allowAll, err := normalizeCORSOrigins(input.AllowOrigins)
	if err != nil {
		return CORSConfig{}, err
	}
	if allowAll && input.AllowCredentials {
		return CORSConfig{}, errorForConfigInvalid("op=app_web_hertz.resolve_config result=failed stage=validate_cors reason=wildcard_credentials_conflict")
	}
	// 第二阶段：归一化浏览器协商字段，输出 slice 与调用方输入隔离。
	methods, err := normalizeHTTPMethods(input.AllowMethods)
	if err != nil {
		return CORSConfig{}, err
	}
	allowHeaders, err := normalizeHTTPHeaders(input.AllowHeaders, defaultCORSHeaders, "allow_headers")
	if err != nil {
		return CORSConfig{}, err
	}
	exposeHeaders, err := normalizeHTTPHeaders(input.ExposeHeaders, nil, "expose_headers")
	if err != nil {
		return CORSConfig{}, err
	}
	input.AllowOrigins, input.AllowMethods = origins, methods
	input.AllowHeaders, input.ExposeHeaders = allowHeaders, exposeHeaders
	return input, nil
}

// normalizeCORSOrigins 复制并校验 Origin 精确值，返回是否包含通配符。
func normalizeCORSOrigins(input []string) ([]string, bool, error) {
	result := make([]string, 0, len(input))
	allowAll := false
	// 每个 Origin 要么是唯一通配符，要么是无用户信息、路径、查询和片段的 http/https authority。
	for _, raw := range input {
		origin := strings.TrimSpace(raw)
		if origin == "*" {
			allowAll = true
			result = append(result, origin)
			continue
		}
		parsed, err := url.Parse(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
			return nil, false, errorForConfigInvalid("op=app_web_hertz.resolve_config result=failed stage=validate_cors reason=origin_invalid")
		}
		result = append(result, strings.TrimSuffix(origin, "/"))
	}
	return deduplicateStrings(result), allowAll, nil
}

// normalizeHTTPMethods 归一化 Method；空输入使用安全默认集合。
func normalizeHTTPMethods(input []string) ([]string, error) {
	if len(input) == 0 {
		return append([]string{}, defaultCORSMethods...), nil
	}
	result := make([]string, 0, len(input))
	for _, raw := range input {
		method := strings.ToUpper(strings.TrimSpace(raw))
		if !validHTTPToken(method) {
			return nil, errorForConfigInvalid("op=app_web_hertz.resolve_config result=failed stage=validate_cors reason=method_invalid")
		}
		result = append(result, method)
	}
	return deduplicateStrings(result), nil
}

// normalizeHTTPHeaders 归一化 Header 名称；空输入按调用方给定默认值处理。
func normalizeHTTPHeaders(input, defaults []string, stage string) ([]string, error) {
	if len(input) == 0 {
		return append([]string{}, defaults...), nil
	}
	result := make([]string, 0, len(input))
	for _, raw := range input {
		header := strings.TrimSpace(raw)
		if !validHTTPToken(header) {
			return nil, errorForConfigInvalid(messagef("op=app_web_hertz.resolve_config result=failed stage=validate_cors reason=%s_invalid", stage))
		}
		result = append(result, http.CanonicalHeaderKey(header))
	}
	return deduplicateStrings(result), nil
}

// corsPolicy 保存构造期归一化且请求期只读的跨域策略。
type corsPolicy struct {
	// allowAll 表示静态 Origin 包含 "*"，仅在不允许凭据时成立。
	allowAll bool
	// origins 保存静态精确 Origin 集合。
	origins map[string]struct{}
	// methods 是预检响应的稳定 Method 列表。
	methods string
	// allowHeaders 是预检响应的稳定 Header 列表。
	allowHeaders string
	// exposeHeaders 是普通响应允许浏览器读取的 Header 列表。
	exposeHeaders string
	// allowCredentials 决定是否写回凭据许可。
	allowCredentials bool
	// maxAgeSeconds 是预检缓存秒数。
	maxAgeSeconds string
	// validator 扩展静态白名单；nil 表示只使用静态策略。
	validator CORSOriginValidator
}

// newCORSMiddleware 冻结跨域策略并返回 Hertz Handler。
// 阶段角色：Recovery 内层、Gzip 外层；策略来源缺失在 Server 构造期 fail-fast。
func newCORSMiddleware(config CORSConfig, validator CORSOriginValidator) (app.HandlerFunc, error) {
	// 第一阶段：把 slice 转为请求期只读字符串和集合，避免每次请求归一化配置。
	policy := corsPolicy{
		origins:          make(map[string]struct{}, len(config.AllowOrigins)),
		methods:          strings.Join(config.AllowMethods, ", "),
		allowHeaders:     strings.Join(config.AllowHeaders, ", "),
		exposeHeaders:    strings.Join(config.ExposeHeaders, ", "),
		allowCredentials: config.AllowCredentials,
		maxAgeSeconds:    strconv.FormatInt(int64(config.MaxAge/time.Second), 10),
		validator:        validator,
	}
	for _, origin := range config.AllowOrigins {
		if origin == "*" {
			policy.allowAll = true
			continue
		}
		policy.origins[origin] = struct{}{}
	}
	// 第二阶段：enabled 且没有任何静态/动态来源策略属于 typed 依赖缺失，而不是“拒绝全部”的静默降级。
	if !policy.allowAll && len(policy.origins) == 0 && policy.validator == nil {
		return nil, errorForDeclarationInvalid("op=app_web_hertz.middleware result=failed stage=build_cors reason=origin_policy_required")
	}
	return policy.handle, nil
}

// handle 执行 Origin 判定并区分普通跨域请求与浏览器预检。
func (this corsPolicy) handle(ctx context.Context, request *app.RequestContext) {
	origin := strings.TrimSpace(request.Request.Header.Get("Origin"))
	if origin == "" {
		request.Next(ctx)
		return
	}
	if !this.originAllowed(ctx, request, origin) {
		request.AbortWithStatus(consts.StatusForbidden)
		return
	}

	// 通过白名单后先写通用响应头；精确 Origin 必须合并 Vary，避免共享缓存串用。
	if this.allowAll {
		request.Response.Header.Set("Access-Control-Allow-Origin", "*")
	} else {
		request.Response.Header.Set("Access-Control-Allow-Origin", origin)
		appendVary(&request.Response.Header, "Origin")
	}
	if this.allowCredentials {
		request.Response.Header.Set("Access-Control-Allow-Credentials", "true")
	}

	// 只有携带 Access-Control-Request-Method 的 OPTIONS 才是预检，普通 OPTIONS 继续业务路由。
	if string(request.Request.Method()) == consts.MethodOptions && request.Request.Header.Get("Access-Control-Request-Method") != "" {
		request.Response.Header.Set("Access-Control-Allow-Methods", this.methods)
		request.Response.Header.Set("Access-Control-Allow-Headers", this.allowHeaders)
		request.Response.Header.Set("Access-Control-Max-Age", this.maxAgeSeconds)
		request.AbortWithStatus(consts.StatusNoContent)
		return
	}
	if this.exposeHeaders != "" {
		request.Response.Header.Set("Access-Control-Expose-Headers", this.exposeHeaders)
	}
	request.Next(ctx)
}

// originAllowed 判断静态、通配与 typed 动态策略，任一允许即通过。
func (this corsPolicy) originAllowed(ctx context.Context, request *app.RequestContext, origin string) bool {
	if this.allowAll {
		return true
	}
	if _, exists := this.origins[origin]; exists {
		return true
	}
	return this.validator != nil && this.validator(ctx, request, origin)
}
