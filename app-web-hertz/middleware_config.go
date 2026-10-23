package appwebhertz

import "strings"

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
