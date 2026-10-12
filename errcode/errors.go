package errcode

import (
	"errors"
	"fmt"
	"strings"
)

// StringCode 表示字符串类型错误码。
type StringCode string

// IntCode 表示整型错误码。
type IntCode int

// CodedError 是统一的泛型错误载体。
//
// 语义：
// 1. Code 是分类依据；
// 2. Msg 是对外可读信息；
// 3. Err 是内部原因链，可为 nil。
type CodedError[T comparable] struct {
	Code T
	Msg  string
	Err  error
}

// Error 返回可读错误文本。
func (this *CodedError[T]) Error() string {
	message := strings.TrimSpace(this.Msg)
	if this.Err == nil {
		if message == "" {
			return "unknown error"
		}
		return message
	}
	if message == "" {
		return this.Err.Error()
	}
	return fmt.Sprintf("%s: %v", message, this.Err)
}

// Unwrap 暴露内部原因链。
func (this *CodedError[T]) Unwrap() error {
	return this.Err
}

// New 创建一个无内部原因链的 coded error。
func New[T comparable](code T, msg string) error {
	return &CodedError[T]{
		Code: code,
		Msg:  strings.TrimSpace(msg),
	}
}

// Wrap 在现有错误上附加 code 与消息。
func Wrap[T comparable](err error, code T, msg string) error {
	if err == nil {
		return nil
	}
	return &CodedError[T]{
		Code: code,
		Msg:  strings.TrimSpace(msg),
		Err:  err,
	}
}

// Wrapf 在现有错误上附加格式化消息。
func Wrapf[T comparable](err error, code T, format string, args ...any) error {
	if err == nil {
		return nil
	}
	return &CodedError[T]{
		Code: code,
		Msg:  strings.TrimSpace(fmt.Sprintf(format, args...)),
		Err:  err,
	}
}

// CodeOf 从错误链中提取 code。
func CodeOf[T comparable](err error) (T, bool) {
	var typed *CodedError[T]
	if errors.As(err, &typed) && typed != nil {
		return typed.Code, true
	}
	var zero T
	return zero, false
}

// IsCode 判断单链或 errors.Join 多链中是否包含目标 code。
func IsCode[T comparable](err error, code T) bool {
	if err == nil {
		return false
	}
	var typed *CodedError[T]
	if errors.As(err, &typed) && typed != nil && typed.Code == code {
		return true
	}
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		for _, item := range multi.Unwrap() {
			if IsCode(item, code) {
				return true
			}
		}
		return false
	}
	if single, ok := err.(interface{ Unwrap() error }); ok {
		return IsCode(single.Unwrap(), code)
	}
	return false
}
