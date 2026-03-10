package utils

import (
	"context"
	"errors"
	"strings"
)

// IsContextCanceled 检查错误是否是由于上下文取消导致的
func IsContextCanceled(err error) bool {
	if err == nil {
		return false
	}
	// 检查直接的上下文错误
	if errors.Is(err, context.Canceled) {
		return true
	}

	return strings.Contains(err.Error(), "context canceled")
}

// IsClientDisconnect 检查错误是否是客户端断开连接
func IsClientDisconnect(err error) bool {
	return IsContextCanceled(err)
}
