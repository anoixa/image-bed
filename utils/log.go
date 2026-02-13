package utils

import (
	"strings"
	"unicode"
)

// maxLogMessageLen 日志消息最大长度，防止日志过大
const maxLogMessageLen = 500

// SanitizeLogMessage 清理日志消息，防止日志注入攻击
// 移除所有控制字符，限制长度
func SanitizeLogMessage(msg string) string {
	var sb strings.Builder
	for _, r := range msg {
		// 只保留可打印字符，移除换行、制表符等控制字符
		if unicode.IsPrint(r) {
			sb.WriteRune(r)
		}
	}
	result := sb.String()
	// 限制长度
	if len(result) > maxLogMessageLen {
		return result[:maxLogMessageLen] + "..."
	}
	return result
}

// SanitizeLogUsername 清理用户名日志，限制长度
func SanitizeLogUsername(username string) string {
	if len(username) > 50 {
		username = username[:50] + "..."
	}
	return SanitizeLogMessage(username)
}
