package service

import "errors"

// 通用业务错误
var (
	// ErrNotFound 资源不存在
	ErrNotFound = errors.New("资源不存在")
	// ErrForbidden 无权限访问
	ErrForbidden = errors.New("无权限访问")
	// ErrInvalidParam 参数错误
	ErrInvalidParam = errors.New("参数错误")
	// ErrConflict 状态冲突
	ErrConflict = errors.New("状态冲突")
)
