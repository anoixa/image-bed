package utils

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"unicode"
)

// maxLogMessageLen 日志消息最大长度
const maxLogMessageLen = 500

// InitLogger 初始化全局 slog logger
// 开发模式: DEBUG 级别 + 文本格式
// 生产模式: INFO 级别 + JSON 格式
func InitLogger(isDev bool) {
	var level slog.Level
	if isDev {
		level = slog.LevelDebug
	} else {
		level = slog.LevelInfo
	}

	var handler slog.Handler
	opts := &slog.HandlerOptions{Level: level}
	if isDev {
		handler = slog.NewTextHandler(os.Stderr, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
}

// Debugf 输出 DEBUG 级别日志（生产模式不输出）
func Debugf(format string, args ...any) {
	slog.Debug(fmt.Sprintf(format, args...))
}

// Infof 输出 INFO 级别日志
func Infof(format string, args ...any) {
	slog.Info(fmt.Sprintf(format, args...))
}

// Warnf 输出 WARN 级别日志
func Warnf(format string, args ...any) {
	slog.Warn(fmt.Sprintf(format, args...))
}

// Errorf 输出 ERROR 级别日志
func Errorf(format string, args ...any) {
	slog.Error(fmt.Sprintf(format, args...))
}

// LogIfDev 兼容旧接口，映射到 Debugf
func LogIfDev(msg string) {
	slog.Debug(msg)
}

// LogIfDevf 兼容旧接口，映射到 Debugf
func LogIfDevf(format string, v ...any) {
	Debugf(format, v...)
}

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
