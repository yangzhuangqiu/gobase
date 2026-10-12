package appwebhertz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

const (
	// requestIDContextKey 是 RequestContext 中保存已校验 Request ID 的私有键。
	requestIDContextKey = "gobase.app_web_hertz.request_id"
	// maxRequestIDLength 限制入站和生成标识容量，避免日志与 Header 放大。
	maxRequestIDLength = 128
)

// newRequestIDMiddleware 构造无进程级状态的请求标识 Middleware。
// 阶段角色：标准链最外层，使 Recovery、CORS、Gzip、Filter 和业务 Handler 均能读取同一标识。
func newRequestIDMiddleware(config RequestIDConfig, generator RequestIDGenerator) app.HandlerFunc {
	if generator == nil {
		generator = defaultRequestIDGenerator
	}
	return func(ctx context.Context, request *app.RequestContext) {
		// 第一阶段：只在显式信任且语法受控时复用入站值，否则创建新的服务端标识。
		requestID := ""
		if config.TrustIncoming {
			candidate := strings.TrimSpace(request.Request.Header.Get(config.Header))
			if validRequestID(candidate) {
				requestID = candidate
			}
		}
		if requestID == "" {
			generated, err := generator(ctx, request)
			if err != nil || !validRequestID(generated) {
				hlog.SystemLogger().CtxErrorf(ctx, "op=app_web_hertz.request_id result=failed reason=generation_failed")
				request.AbortWithStatusJSON(consts.StatusInternalServerError, map[string]string{"code": "request_id_generation_failed"})
				return
			}
			requestID = generated
		}

		// 第二阶段：写回请求、响应和池内属性，再继续完整请求链。
		request.Request.Header.Set(config.Header, requestID)
		request.Response.Header.Set(config.Header, requestID)
		request.Set(requestIDContextKey, requestID)
		request.Next(ctx)
	}
}

// GetRequestID 返回标准 Middleware 已写入当前请求的关联标识。
//
// 参数:
// - request: 当前 Hertz RequestContext；nil 或未启用 Request ID 时返回空字符串。
func GetRequestID(request *app.RequestContext) string {
	if request == nil {
		return ""
	}
	value, exists := request.Get(requestIDContextKey)
	if !exists {
		return ""
	}
	requestID, _ := value.(string)
	return requestID
}

// defaultRequestIDGenerator 使用 crypto/rand 生成 128-bit 十六进制标识。
// 阶段角色：Request ID 启用且没有可信入站值或 typed Generator 时执行。
func defaultRequestIDGenerator(_ context.Context, _ *app.RequestContext) (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

// validRequestID 限制标识为非空、有限长度且不含空白、控制或非 ASCII 字节。
func validRequestID(value string) bool {
	if len(value) == 0 || len(value) > maxRequestIDLength {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x21 || value[index] > 0x7e {
			return false
		}
	}
	return true
}
