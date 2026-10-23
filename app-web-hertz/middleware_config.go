package appwebhertz

import (
	"compress/gzip"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// defaultRequestIDHeader 是 Request ID 启用后的默认请求与响应 Header。
	defaultRequestIDHeader = "X-Request-ID"
	// defaultCORSMaxAge 是浏览器缓存成功预检结果的默认时间。
	defaultCORSMaxAge = 12 * time.Hour
	// defaultGzipMinLength 是响应进入压缩阶段的默认最小字节数。
	defaultGzipMinLength = 1024
	// defaultGzipLevel 使用标准库速度与压缩率平衡策略。
	defaultGzipLevel = gzip.DefaultCompression
)

var (
	// defaultCORSMethods 是未显式声明时允许的安全常用 Method 集合。
	defaultCORSMethods = []string{"GET", "HEAD", "POST", "OPTIONS"}
	// defaultCORSHeaders 是未显式声明时允许浏览器发送的基础 Header 集合。
	defaultCORSHeaders = []string{"Accept", "Content-Type", "Origin"}
	// defaultGzipExcludedContentTypes 是默认不重复压缩的二进制媒体类型前缀或精确值。
	defaultGzipExcludedContentTypes = []string{
		"image/", "audio/", "video/", "application/gzip", "application/pdf", "application/zip", "application/octet-stream",
	}
)

// RecoveryResponseStrategy 定义内建 Recovery 返回给客户端的稳定响应格式。
type RecoveryResponseStrategy string

const (
	// RecoveryResponseJSON 返回 JSON 错误码且不暴露 panic 内容；默认策略。
	RecoveryResponseJSON RecoveryResponseStrategy = "json"
	// RecoveryResponseText 返回固定纯文本，不暴露 panic 内容。
	RecoveryResponseText RecoveryResponseStrategy = "text"
	// RecoveryResponseEmpty 只返回 HTTP 500 状态。
	RecoveryResponseEmpty RecoveryResponseStrategy = "empty"
)

// RecoveryConfig 描述 panic 恢复能力；默认关闭。
type RecoveryConfig struct {
	// Enabled 决定是否安装 Recovery；默认 false，旧 New 行为不变。
	Enabled bool `yaml:"enabled"`
	// Response 选择内建 500 响应；空值默认 json，自定义 Handler 声明后由 Handler 接管。
	Response RecoveryResponseStrategy `yaml:"response"`
}

// RequestIDConfig 描述请求关联标识能力；默认关闭。
type RequestIDConfig struct {
	// Enabled 决定是否安装 Request ID；默认 false。
	Enabled bool `yaml:"enabled"`
	// Header 是读取和回写标识的 HTTP Header；空值默认 X-Request-ID。
	Header string `yaml:"header"`
	// TrustIncoming 决定是否复用语法合法的入站值；默认 false，避免客户端伪造内部关联标识。
	TrustIncoming bool `yaml:"trustIncoming"`
}

// AccessLogConfig 描述完成态请求日志的启用和应用侧过滤策略；默认关闭。
type AccessLogConfig struct {
	// Enabled 决定是否把完成态快照写入根 LoggerFactory；默认 false。
	Enabled bool `yaml:"enabled"`
	// SkipPaths 是不写访问日志的精确请求路径；不支持通配符且不得包含 query/fragment。
	SkipPaths []string `yaml:"skipPaths"`
	// SlowThreshold 是慢请求判定阈值；零值表示不区分慢请求，负值非法。
	SlowThreshold time.Duration `yaml:"slowThreshold"`
}

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

// GzipConfig 描述普通内存响应压缩策略；默认关闭。
type GzipConfig struct {
	// Enabled 决定是否安装 Gzip；默认 false。
	Enabled bool `yaml:"enabled"`
	// Level 是 1..9 的 gzip 压缩级别；0 或 -1 使用标准库默认级别。
	Level int `yaml:"level"`
	// MinLength 是进入压缩的最小原始响应字节数；零值使用 1024，负值非法。
	MinLength int `yaml:"minLength"`
	// ExcludedContentTypes 是不压缩的媒体类型前缀或精确值；空值使用模块安全默认集合。
	ExcludedContentTypes []string `yaml:"excludedContentTypes"`
}

// resolveMiddlewareConfig 解析全部标准 Middleware 默认值并复制 slice，避免持有调用方可变输入。
// 阶段角色：Server 构造事务的配置冻结阶段，不创建 Handler 或外部资源。
func resolveMiddlewareConfig(input MiddlewareConfig) (MiddlewareConfig, error) {
	// 第一阶段：解析各能力自身字段，禁用状态也拒绝明显非法值，避免配置启用后才暴露潜伏错误。
	recoveryConfig, err := resolveRecoveryConfig(input.Recovery)
	if err != nil {
		return MiddlewareConfig{}, err
	}
	requestIDConfig, err := resolveRequestIDConfig(input.RequestID)
	if err != nil {
		return MiddlewareConfig{}, err
	}
	accessLogConfig, err := resolveAccessLogConfig(input.AccessLog)
	if err != nil {
		return MiddlewareConfig{}, err
	}
	corsConfig, err := resolveCORSConfig(input.CORS)
	if err != nil {
		return MiddlewareConfig{}, err
	}
	gzipConfig, err := resolveGzipConfig(input.Gzip)
	if err != nil {
		return MiddlewareConfig{}, err
	}
	return MiddlewareConfig{Recovery: recoveryConfig, RequestID: requestIDConfig, AccessLog: accessLogConfig, CORS: corsConfig, Gzip: gzipConfig}, nil
}

// resolveRecoveryConfig 固化内建响应策略并拒绝未知未来值。
func resolveRecoveryConfig(input RecoveryConfig) (RecoveryConfig, error) {
	if input.Response == "" {
		input.Response = RecoveryResponseJSON
	}
	switch input.Response {
	case RecoveryResponseJSON, RecoveryResponseText, RecoveryResponseEmpty:
		return input, nil
	default:
		return RecoveryConfig{}, errorForConfigInvalid(messagef("op=app_web_hertz.resolve_config result=failed stage=validate_recovery reason=response_unsupported response=%s", input.Response))
	}
}

// resolveRequestIDConfig 校验 Header token 并固定默认 Header。
func resolveRequestIDConfig(input RequestIDConfig) (RequestIDConfig, error) {
	input.Header = strings.TrimSpace(input.Header)
	if input.Header == "" {
		input.Header = defaultRequestIDHeader
	}
	if !validHTTPToken(input.Header) {
		return RequestIDConfig{}, errorForConfigInvalid("op=app_web_hertz.resolve_config result=failed stage=validate_request_id reason=header_invalid")
	}
	return input, nil
}

// resolveAccessLogConfig 校验慢请求阈值并把跳过路径冻结为去重的精确路径集合。
func resolveAccessLogConfig(input AccessLogConfig) (AccessLogConfig, error) {
	if input.SlowThreshold < 0 {
		return AccessLogConfig{}, errorForConfigInvalid("op=app_web_hertz.resolve_config result=failed stage=validate_access_log reason=slow_threshold_negative")
	}
	paths := make([]string, 0, len(input.SkipPaths))
	for _, raw := range input.SkipPaths {
		path := strings.TrimSpace(raw)
		if path == "" || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
			return AccessLogConfig{}, errorForConfigInvalid("op=app_web_hertz.resolve_config result=failed stage=validate_access_log reason=skip_path_invalid")
		}
		paths = append(paths, path)
	}
	input.SkipPaths = deduplicateStrings(paths)
	return input, nil
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

// resolveGzipConfig 校验压缩级别和容量阈值，并复制排除类型。
func resolveGzipConfig(input GzipConfig) (GzipConfig, error) {
	if input.Level == 0 || input.Level == defaultGzipLevel {
		input.Level = defaultGzipLevel
	} else if input.Level < gzip.BestSpeed || input.Level > gzip.BestCompression {
		return GzipConfig{}, errorForConfigInvalid(messagef("op=app_web_hertz.resolve_config result=failed stage=validate_gzip reason=level_invalid level=%d", input.Level))
	}
	if input.MinLength < 0 {
		return GzipConfig{}, errorForConfigInvalid("op=app_web_hertz.resolve_config result=failed stage=validate_gzip reason=min_length_negative")
	}
	if input.MinLength == 0 {
		input.MinLength = defaultGzipMinLength
	}
	if len(input.ExcludedContentTypes) == 0 {
		input.ExcludedContentTypes = append([]string{}, defaultGzipExcludedContentTypes...)
	} else {
		input.ExcludedContentTypes = normalizeMediaTypes(input.ExcludedContentTypes)
	}
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

// validHTTPToken 判断输入是否满足 RFC 7230 token 字符集合。
func validHTTPToken(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(character)) {
			continue
		}
		return false
	}
	return true
}

// normalizeMediaTypes 归一化并去重 Gzip 排除类型；空项被忽略。
func normalizeMediaTypes(input []string) []string {
	result := make([]string, 0, len(input))
	for _, raw := range input {
		mediaType := strings.ToLower(strings.TrimSpace(raw))
		if mediaType != "" {
			result = append(result, mediaType)
		}
	}
	return deduplicateStrings(result)
}

// deduplicateStrings 保持首次声明顺序并移除重复值。
func deduplicateStrings(input []string) []string {
	seen := make(map[string]struct{}, len(input))
	result := make([]string, 0, len(input))
	for _, value := range input {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
