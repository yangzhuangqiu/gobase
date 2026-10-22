package appwebhertz

import "strings"

// normalizeGroupPath 把 Controller 与 GroupedFilter 的分组声明转换为统一逻辑标识。
// 阶段角色：构造计划校验前消除空白和根路径表达差异，不改变非根路径的业务含义。
func normalizeGroupPath(path string) (string, bool) {
	normalized := strings.TrimSpace(path)
	if normalized == "" || normalized == "/" {
		return "/", true
	}
	return normalized, strings.HasPrefix(normalized, "/")
}
