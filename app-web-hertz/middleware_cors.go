package appwebhertz

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

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
