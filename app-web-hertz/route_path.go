package appwebhertz

import (
	"strings"
	"unicode"
)

// routePath 把 Controller 方法后缀转换为当前风格的根相对 URL。
// 阶段角色：自动路由计划构建期生成不可变路径，运行期不再执行字符串转换。
func (this RoutePathStyle) routePath(suffix string) string {
	words := splitIdentifierWords(suffix)
	switch this {
	case RoutePathStyleCamel:
		return "/" + joinLowerCamel(words)
	case RoutePathStyleSnake:
		return "/" + strings.Join(lowerWords(words), "_")
	default:
		return "/" + strings.Join(lowerWords(words), "-")
	}
}

// splitIdentifierWords 按 Go 导出方法的驼峰和连续大写缩写边界拆分单词。
// 阶段角色：统一保证 URL 风格间只改变连接方式，不改变词义边界。
func splitIdentifierWords(value string) []string {
	runes := []rune(value)
	if len(runes) == 0 {
		return nil
	}

	// 连续大写在后接小写时把最后一个大写归入新词，例如 URLValue -> URL + Value。
	words := make([]string, 0, 4)
	start := 0
	for index := 1; index < len(runes); index++ {
		currentUpper := unicode.IsUpper(runes[index])
		previousUpper := unicode.IsUpper(runes[index-1])
		nextLower := index+1 < len(runes) && unicode.IsLower(runes[index+1])
		if currentUpper && (!previousUpper || nextLower) {
			words = append(words, string(runes[start:index]))
			start = index
		}
	}
	return append(words, string(runes[start:]))
}

// lowerWords 返回全部小写的新切片，避免修改拆词结果。
// 阶段角色：供 kebab 和 snake 两种规范共享确定性的大小写归一化。
func lowerWords(words []string) []string {
	result := make([]string, 0, len(words))
	for _, word := range words {
		result = append(result, strings.ToLower(word))
	}
	return result
}

// joinLowerCamel 把单词序列归一化为 lower camel case。
// 阶段角色：自动路由计划构建期实现 RoutePathStyleCamel 映射。
func joinLowerCamel(words []string) string {
	if len(words) == 0 {
		return ""
	}
	result := strings.ToLower(words[0])
	for _, word := range words[1:] {
		lower := []rune(strings.ToLower(word))
		lower[0] = unicode.ToUpper(lower[0])
		result += string(lower)
	}
	return result
}
