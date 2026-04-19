package utils

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

const (
	// maxLogMessageLen 日志消息最大长度
	maxLogMessageLen = 500
	defaultLogModule = "App"
	logModuleAttrKey = "module"
)

type Logger struct {
	module string
}

type textLogHandler struct {
	writer   io.Writer
	minLevel slog.Level
	attrs    []slog.Attr
	groups   []string
	mu       *sync.Mutex
}

// InitLogger 初始化全局 slog logger。
// 输出格式统一为: 2006-01-02 15:04:05 [Module][LEVEL] message
func InitLogger(isDev bool) {
	level := slog.LevelInfo
	if isDev {
		level = slog.LevelDebug
	}

	slog.SetDefault(slog.New(newTextLogHandler(os.Stderr, level)))
}

func newTextLogHandler(writer io.Writer, minLevel slog.Level) slog.Handler {
	return &textLogHandler{
		writer:   writer,
		minLevel: minLevel,
		mu:       &sync.Mutex{},
	}
}

func (h *textLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.minLevel
}

func (h *textLogHandler) Handle(_ context.Context, record slog.Record) error {
	module, message := parseModulePrefix(strings.TrimSpace(record.Message))
	fields := make([]string, 0, len(h.attrs)+record.NumAttrs())

	appendAttr := func(attr slog.Attr) {
		attr.Value = attr.Value.Resolve()
		if attr.Equal(slog.Attr{}) {
			return
		}

		key := attr.Key
		if len(h.groups) > 0 {
			key = strings.Join(append(append([]string(nil), h.groups...), key), ".")
		}

		if key == logModuleAttrKey || strings.HasSuffix(key, "."+logModuleAttrKey) {
			module = attr.Value.String()
			return
		}

		fields = append(fields, fmt.Sprintf("%s=%s", key, formatLogValue(attr.Value)))
	}

	for _, attr := range h.attrs {
		appendAttr(attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		appendAttr(attr)
		return true
	})

	if module == "" {
		module = defaultLogModule
	}

	var line strings.Builder
	if !record.Time.IsZero() {
		line.WriteString(record.Time.Format("2006-01-02 15:04:05"))
		line.WriteByte(' ')
	}
	line.WriteString("[")
	line.WriteString(module)
	line.WriteString("][")
	line.WriteString(strings.ToUpper(record.Level.String()))
	line.WriteString("] ")
	line.WriteString(message)

	if len(fields) > 0 {
		line.WriteByte(' ')
		line.WriteString(strings.Join(fields, " "))
	}
	line.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.writer, line.String())
	return err
}

func (h *textLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := h.clone()
	clone.attrs = append(clone.attrs, attrs...)
	return clone
}

func (h *textLogHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := h.clone()
	clone.groups = append(clone.groups, name)
	return clone
}

func (h *textLogHandler) clone() *textLogHandler {
	return &textLogHandler{
		writer:   h.writer,
		minLevel: h.minLevel,
		attrs:    append([]slog.Attr(nil), h.attrs...),
		groups:   append([]string(nil), h.groups...),
		mu:       h.mu,
	}
}

// ForModule 返回带固定模块名的 logger，适合新代码直接复用。
func ForModule(module string) *Logger {
	return &Logger{module: strings.TrimSpace(module)}
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

// Debugf 输出 DEBUG 级别日志（生产模式不输出）
func (l *Logger) Debugf(format string, args ...any) {
	logWithModule(slog.LevelDebug, l.module, fmt.Sprintf(format, args...))
}

// Infof 输出 INFO 级别日志
func (l *Logger) Infof(format string, args ...any) {
	logWithModule(slog.LevelInfo, l.module, fmt.Sprintf(format, args...))
}

// Warnf 输出 WARN 级别日志
func (l *Logger) Warnf(format string, args ...any) {
	logWithModule(slog.LevelWarn, l.module, fmt.Sprintf(format, args...))
}

// Errorf 输出 ERROR 级别日志
func (l *Logger) Errorf(format string, args ...any) {
	logWithModule(slog.LevelError, l.module, fmt.Sprintf(format, args...))
}

func logWithModule(level slog.Level, module string, message string) {
	module = strings.TrimSpace(module)
	if module == "" {
		slog.Log(context.Background(), level, message)
		return
	}

	slog.LogAttrs(context.Background(), level, message, slog.String(logModuleAttrKey, module))
}

// LogIfDev 兼容旧接口，映射到 Debugf
func LogIfDev(msg string) {
	slog.Debug(msg)
}

// LogIfDevf 兼容旧接口，映射到 Debugf
func LogIfDevf(format string, v ...any) {
	Debugf(format, v...)
}

func parseModulePrefix(message string) (string, string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", ""
	}

	tokens := make([]string, 0, 2)
	rest := message
	for strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]")
		if end <= 1 {
			break
		}

		tokens = append(tokens, strings.TrimSpace(rest[1:end]))
		rest = strings.TrimSpace(rest[end+1:])
	}

	if len(tokens) == 0 {
		return "", message
	}

	if isLogLevelToken(tokens[0]) {
		if len(tokens) == 1 {
			return "", message
		}
		tokens = tokens[1:]
	}

	module := tokens[0]
	if len(tokens) > 1 {
		rest = "[" + strings.Join(tokens[1:], "][") + "] " + rest
	}

	if rest == "" {
		rest = message
	}

	return module, strings.TrimSpace(rest)
}

func isLogLevelToken(token string) bool {
	switch strings.ToUpper(strings.TrimSpace(token)) {
	case "DEBUG", "INFO", "WARN", "WARNING", "ERROR":
		return true
	default:
		return false
	}
}

func formatLogValue(value slog.Value) string {
	switch value.Kind() {
	case slog.KindString:
		return fmt.Sprintf("%q", value.String())
	default:
		return fmt.Sprintf("%v", value.Any())
	}
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
		return truncateAtRuneBoundary(result, maxLogMessageLen)
	}
	return result
}

// SanitizeLogUsername 清理用户名日志，限制长度
func SanitizeLogUsername(username string) string {
	if len(username) > 50 {
		username = truncateAtRuneBoundary(username, 50)
	}
	return SanitizeLogMessage(username)
}

// truncateAtRuneBoundary 截断字符串，确保不在多字节 UTF-8 字符中间截断
func truncateAtRuneBoundary(s string, maxBytes int) string {
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	if maxBytes <= 0 {
		return "..."
	}
	return s[:maxBytes] + "..."
}
