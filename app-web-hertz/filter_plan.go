package appwebhertz

import (
	"sort"

	hertzapp "github.com/cloudwego/hertz/pkg/app"
)

// filterEntry 保存 Filter 的稳定排序信息和已解析原生 Handler。
type filterEntry struct {
	// order 是 Filter 在所属作用域内的 Middleware 优先级，数值越小越先执行。
	order int
	// inputIndex 是 IOC 注册顺序，用于相同 order 的稳定排序。
	inputIndex int
	// handler 是 Filter.DoFilter 的原生 Hertz 方法值。
	handler hertzapp.HandlerFunc
}

// groupedFilterPlan 保存一个逻辑分组及其已排序 Middleware。
type groupedFilterPlan struct {
	// groupPath 是归一化后的逻辑分组标识，根分组固定为 `/`。
	groupPath string
	// handlers 按 Order 和输入顺序排列，只安装到当前逻辑分组。
	handlers []hertzapp.HandlerFunc
}

// filterPlan 保存全局和分组 Filter 的不可变构造计划。
type filterPlan struct {
	// globalHandlers 注册到 Hertz Engine，对路由和 fallback 全局生效。
	globalHandlers []hertzapp.HandlerFunc
	// groupedFilters 按分组首次出现顺序保存共享基础分组 Middleware。
	groupedFilters []groupedFilterPlan
}

// prepareFilters 按接口类型划分全局与分组 Filter，并在各自作用域内稳定排序。
// 阶段角色：全部声明在 provider 构造前 fail-fast，避免根分组被误装成全局 Middleware。
func prepareFilters(filters []Filter) (filterPlan, error) {
	globalEntries := make([]filterEntry, 0, len(filters))
	groupEntries := make(map[string][]filterEntry)
	groupOrder := make([]string, 0)

	// 第一阶段：接口类型决定作用域；GroupPath 只标识 GroupedFilter 的逻辑分组。
	for index, item := range filters {
		if isNilInterface(item) {
			return filterPlan{}, errorForDeclarationInvalid(messagef("op=app_web_hertz.build result=failed stage=validate_filters reason=filter_required filter_index=%d", index))
		}
		entry := filterEntry{order: item.Order(), inputIndex: index, handler: item.DoFilter}
		grouped, groupedScope := item.(GroupedFilter)
		if !groupedScope {
			globalEntries = append(globalEntries, entry)
			continue
		}
		groupPath, valid := normalizeGroupPath(grouped.GroupPath())
		if !valid {
			return filterPlan{}, errorForDeclarationInvalid(messagef("op=app_web_hertz.build result=failed stage=validate_filter_group reason=leading_slash_required filter_index=%d group_path=%q", index, groupPath))
		}
		if _, exists := groupEntries[groupPath]; !exists {
			groupOrder = append(groupOrder, groupPath)
		}
		groupEntries[groupPath] = append(groupEntries[groupPath], entry)
	}

	// 第二阶段：全局链和每个逻辑分组分别排序，分组之间不建立隐含优先级。
	plan := filterPlan{globalHandlers: sortedFilterHandlers(globalEntries)}
	plan.groupedFilters = make([]groupedFilterPlan, 0, len(groupOrder))
	for _, groupPath := range groupOrder {
		plan.groupedFilters = append(plan.groupedFilters, groupedFilterPlan{
			groupPath: groupPath,
			handlers:  sortedFilterHandlers(groupEntries[groupPath]),
		})
	}
	return plan, nil
}

// sortedFilterHandlers 按 Order 和原始输入位置生成一个作用域内的稳定 Handler 链。
// 阶段角色：声明已校验后只做确定性排序和原生方法值投影。
func sortedFilterHandlers(entries []filterEntry) []hertzapp.HandlerFunc {
	sort.SliceStable(entries, func(left, right int) bool {
		if entries[left].order == entries[right].order {
			return entries[left].inputIndex < entries[right].inputIndex
		}
		return entries[left].order < entries[right].order
	})
	handlers := make([]hertzapp.HandlerFunc, 0, len(entries))
	for _, entry := range entries {
		handlers = append(handlers, entry.handler)
	}
	return handlers
}
