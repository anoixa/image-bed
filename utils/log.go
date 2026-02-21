package utils

import (
	"log"
	"strings"
	"unicode"

	"github.com/anoixa/image-bed/config"
)

// maxLogMessageLen 日志消息最大长度
const maxLogMessageLen = 500

// SanitizeLogMessage 清理日志消息
func SanitizeLogMessage(msg string) string {
	var sb strings.Builder
	for _, r := range msg {
		if unicode.IsPrint(r) {
			sb.WriteRune(r)
		}
	}
	result := sb.String()
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

// LogIfDev 仅在开发版本时输出日志
func LogIfDev(msg string) {
	if config.CommitHash == "n/a" {
		log.Println(msg)
	}
}

// LogIfDevf 仅在开发版本时格式化输出日志
func LogIfDevf(format string, v ...interface{}) {
	if config.CommitHash == "n/a" {
		log.Printf(format, v...)
	}
}
