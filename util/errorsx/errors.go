package errorsx

import (
	"errors"
	"fmt"
	"strings"
)

// Kind 表示可分支处理的错误语义键。
//
// 语义:
// - 推荐使用 namespaced 形式（例如 randx.invalid_range），避免跨包冲突；
// - 作为对外长期契约，调用方应使用稳定字符串做分支判断。
type Kind string

// Error 是 util 模块统一错误载体。
//
// 语义:
// - Op: 错误发生的操作名（建议 package.func）；
// - Kind: 可分支匹配的稳定语义键；
// - Err: 内部原因链；
// - Detail: 便于排障的上下文信息；
type Error struct {
	// Op 表示失败发生的操作或内部阶段，推荐 package.func 或 package.type.method。
	Op string
	// Kind 表示调用方可稳定分支的失败原因，不应包含动态值或内部步骤名称。
	Kind Kind
	// Err 保存底层 cause，供 errors.Is/As 沿错误链继续判断。
	Err error
	// Detail 保存不参与分支的诊断上下文，例如 path、field 或长度。
	Detail string
}

// Error 返回统一错误字符串，便于日志检索与链路排障。
//
// 典型调用场景:
// - 上层日志直接输出 error 时，保留 op/kind/detail/cause 语义。
func (this *Error) Error() string {
	if this == nil {
		return "op=unknown result=failed kind=unknown.unknown err=nil"
	}

	// 元数据阶段：空字段使用稳定占位值，保证日志键始终可检索。
	op := strings.TrimSpace(this.Op)
	if op == "" {
		op = "unknown"
	}
	kind := strings.TrimSpace(string(this.Kind))
	if kind == "" {
		kind = "unknown.unknown"
	}

	base := fmt.Sprintf("op=%s result=failed kind=%s", op, kind)
	// 上下文阶段：detail 和 cause 仅用于诊断，不改变 Kind 分支语义。
	detail := strings.TrimSpace(this.Detail)
	if detail != "" {
		base += " " + detail
	}
	if this.Err != nil {
		base += " err=" + this.Err.Error()
	}
	return base
}

// Unwrap 返回内部原因链，兼容 errors.Is / errors.As。
//
// 典型调用场景:
// - 业务层需要透传或匹配底层错误（如 context.Canceled、net.Error）。
func (this *Error) Unwrap() error {
	if this == nil {
		return nil
	}
	return this.Err
}

// New 创建无内部原因链的统一错误。
//
// 参数:
// - op: 当前失败操作名，建议使用 package.func 或 package.type.method。
// - kind: 可分支匹配的稳定语义键（建议 namespaced）。
func New(op string, kind Kind) error {
	return &Error{
		Op:   op,
		Kind: kind,
	}
}

// Newf 创建带 detail 的无内部原因链错误。
//
// 参数:
// - op: 当前失败操作名。
// - kind: 可分支匹配的稳定语义键。
// - detailFormat/args: 便于排障的上下文信息。
func Newf(op string, kind Kind, detailFormat string, args ...any) error {
	return &Error{
		Op:     op,
		Kind:   kind,
		Detail: strings.TrimSpace(fmt.Sprintf(detailFormat, args...)),
	}
}

// Wrap 在错误链上附加统一错误语义。
//
// 参数:
// - err: 需要包装的原始错误；为 nil 时返回 nil。
// - op: 当前失败操作名。
// - kind: 可分支匹配的稳定语义键。
func Wrap(err error, op string, kind Kind) error {
	if err == nil {
		return nil
	}
	return &Error{
		Op:   op,
		Kind: kind,
		Err:  err,
	}
}

// Wrapf 在错误链上附加统一错误语义与 detail。
//
// 参数:
// - err: 需要包装的原始错误；为 nil 时返回 nil。
// - op: 当前失败操作名。
// - kind: 可分支匹配的稳定语义键。
// - detailFormat/args: 便于排障的上下文信息。
func Wrapf(err error, op string, kind Kind, detailFormat string, args ...any) error {
	if err == nil {
		return nil
	}
	return &Error{
		Op:     op,
		Kind:   kind,
		Err:    err,
		Detail: strings.TrimSpace(fmt.Sprintf(detailFormat, args...)),
	}
}

// IsKind 判断错误链是否包含目标 kind。
//
// 典型调用场景:
// - 业务层按 kind 做稳定分支（重试、降级、提示语映射等）。
//
// 语义:
// - 同时遍历 Unwrap() error 单链与 errors.Join 的 Unwrap() []error 多分支；
// - 匹配结果不依赖 Join 分支顺序。
func IsKind(err error, kind Kind) bool {
	if err == nil {
		return false
	}

	var typed *Error
	if errors.As(err, &typed) && typed != nil && typed.Kind == kind {
		return true
	}

	// 多分支阶段：逐个遍历 errors.Join 暴露的兄弟分支，避免首个同类型错误遮蔽后续目标 Kind。
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		for _, branch := range multi.Unwrap() {
			if IsKind(branch, kind) {
				return true
			}
		}
		return false
	}

	// 单链阶段：沿标准 cause 继续匹配内层 Kind。
	if single, ok := err.(interface{ Unwrap() error }); ok {
		return IsKind(single.Unwrap(), kind)
	}
	return false
}

// KindOf 提取错误链中的 kind。
//
// 返回:
// - 第一个返回值: 提取到的 kind；
// - 第二个返回值: 是否成功命中 errorsx.Error。
func KindOf(err error) (Kind, bool) {
	var typed *Error
	if errors.As(err, &typed) && typed != nil {
		return typed.Kind, true
	}
	return "", false
}

// OpOf 提取错误链中的 op。
//
// 返回:
// - 第一个返回值: 提取到的 op；
// - 第二个返回值: 是否成功命中 errorsx.Error。
func OpOf(err error) (string, bool) {
	var typed *Error
	if errors.As(err, &typed) && typed != nil {
		return typed.Op, true
	}
	return "", false
}
