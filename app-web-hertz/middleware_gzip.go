package appwebhertz

import (
	"bytes"
	"compress/gzip"
	"context"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	baselog "github.com/yangzhuangqiu/gobase/log"
)

const (
	// defaultGzipMinLength 是响应进入压缩阶段的默认最小字节数。
	defaultGzipMinLength = 1024
	// defaultGzipLevel 使用标准库速度与压缩率平衡策略。
	defaultGzipLevel = gzip.DefaultCompression
)

var (
	// defaultGzipExcludedContentTypes 是默认不重复压缩的二进制媒体类型前缀或精确值。
	defaultGzipExcludedContentTypes = []string{
		"image/", "audio/", "video/", "application/gzip", "application/pdf", "application/zip", "application/octet-stream",
	}
)

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

// newGzipMiddleware 构造只持有当前配置快照的普通响应压缩 Handler。
// 阶段角色：标准链最内层、应用 Filter 外层；只处理完整内存响应，不接管流式响应。
func newGzipMiddleware(config GzipConfig, predicate GzipPredicate, logger baselog.Logger) app.HandlerFunc {
	excludedContentTypes := append([]string{}, config.ExcludedContentTypes...)
	return func(ctx context.Context, request *app.RequestContext) {
		request.Next(ctx)
		if !shouldCompressResponse(request, config.MinLength, excludedContentTypes) {
			return
		}
		if predicate != nil && !predicate(ctx, request) {
			return
		}
		if err := compressResponse(request, config.Level); err != nil {
			// 压缩失败保留未压缩响应，不把实现错误升级为第二次请求失败。
			logger.Error(ctx, "Response compression failed",
				baselog.F("op", "app_web_hertz.gzip"), baselog.F("result", "failed"), baselog.F("reason", "compression_failed"),
				baselog.Err(err),
			)
		}
	}
}

// shouldCompressResponse 执行协议、状态、容量和媒体类型基础检查。
func shouldCompressResponse(request *app.RequestContext, minLength int, excludedContentTypes []string) bool {
	// 第一阶段：请求协商、Method 和最终状态必须允许变换，流式响应不能在此物化。
	if request == nil || string(request.Request.Method()) == consts.MethodHead || !acceptsGzip(request.Request.Header.Get("Accept-Encoding")) {
		return false
	}
	status := request.Response.StatusCode()
	if status < 200 || status == consts.StatusNoContent || status == consts.StatusNotModified || request.Response.IsBodyStream() {
		return false
	}
	if request.Request.Header.Get("Range") != "" || request.Response.Header.Get("Content-Encoding") != "" || request.Response.Header.Get("Content-Range") != "" || strings.Contains(strings.ToLower(request.Response.Header.Get("Cache-Control")), "no-transform") {
		return false
	}
	// 第二阶段：只对达到阈值且媒体类型未排除的完整 Body 执行压缩。
	body := request.Response.BodyBytes()
	if len(body) < minLength {
		return false
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.SplitN(string(request.Response.Header.ContentType()), ";", 2)[0]))
	return !contentTypeExcluded(contentType, excludedContentTypes)
}

// compressResponse 使用当前 Server 独占配置压缩响应，并保持 Content-Length/Vary 一致。
func compressResponse(request *app.RequestContext, level int) error {
	// 第一阶段：在独立 buffer 中完成压缩；失败时原响应保持不变。
	body := request.Response.BodyBytes()
	buffer := bytes.NewBuffer(make([]byte, 0, len(body)/2))
	writer, err := gzip.NewWriterLevel(buffer, level)
	if err != nil {
		return errorForMiddlewareExecutionFailed("op=app_web_hertz.gzip result=failed stage=create_writer reason=writer_create_failed", err)
	}
	if _, err = writer.Write(body); err != nil {
		_ = writer.Close()
		return errorForMiddlewareExecutionFailed("op=app_web_hertz.gzip result=failed stage=compress_body reason=writer_write_failed", err)
	}
	if err = writer.Close(); err != nil {
		return errorForMiddlewareExecutionFailed("op=app_web_hertz.gzip result=failed stage=finalize_body reason=writer_close_failed", err)
	}
	// 第二阶段：压缩全部成功后一次性提交 Body/Header，避免发布半压缩响应。
	compressed := buffer.Bytes()
	request.Response.SetBody(compressed)
	request.Response.Header.SetContentLength(len(compressed))
	request.Response.Header.Set("Content-Encoding", "gzip")
	appendVary(&request.Response.Header, "Accept-Encoding")
	return nil
}

// acceptsGzip 解析 Accept-Encoding，并尊重 gzip;q=0 和通配符质量值。
func acceptsGzip(header string) bool {
	wildcardAllowed := false
	// 显式 gzip token 优先于通配符；非法或越界 q 值按不接受处理。
	for _, item := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(item), ";")
		encoding := strings.ToLower(strings.TrimSpace(parts[0]))
		quality := 1.0
		for _, parameter := range parts[1:] {
			keyValue := strings.SplitN(strings.TrimSpace(parameter), "=", 2)
			if len(keyValue) == 2 && strings.EqualFold(keyValue[0], "q") {
				parsed, err := strconv.ParseFloat(keyValue[1], 64)
				if err != nil {
					quality = 0
				} else {
					quality = parsed
				}
			}
		}
		if quality < 0 || quality > 1 {
			quality = 0
		}
		if encoding == "gzip" {
			return quality > 0
		}
		if encoding == "*" && quality > 0 {
			wildcardAllowed = true
		}
	}
	return wildcardAllowed
}

// contentTypeExcluded 判断精确媒体类型或以斜杠结尾的类型前缀。
func contentTypeExcluded(contentType string, excluded []string) bool {
	for _, value := range excluded {
		if strings.HasSuffix(value, "/") && strings.HasPrefix(contentType, value) {
			return true
		}
		if contentType == value {
			return true
		}
	}
	return false
}

// appendVary 在不覆盖 CORS 等既有值的前提下追加唯一 Vary token。
func appendVary(header *protocol.ResponseHeader, value string) {
	existing := header.Get("Vary")
	for _, item := range strings.Split(existing, ",") {
		if strings.EqualFold(strings.TrimSpace(item), value) {
			return
		}
	}
	if strings.TrimSpace(existing) == "" {
		header.Set("Vary", value)
		return
	}
	header.Set("Vary", existing+", "+value)
}
