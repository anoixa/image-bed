package format

import (
	"strconv"
	"strings"
)

// FormatType 格式类型
type FormatType string

const (
	FormatAVIF     FormatType = "avif"
	FormatWebP     FormatType = "webp"
	FormatOriginal FormatType = "original"
)

// FormatInfo 格式信息
type FormatInfo struct {
	Type      FormatType
	MIMEType  string
	Extension string
	Priority  int // 服务质量优先级
}

// FormatRegistry 格式注册表
var FormatRegistry = map[FormatType]FormatInfo{
	FormatAVIF: {
		Type:      FormatAVIF,
		MIMEType:  "image/avif",
		Extension: ".avif",
		Priority:  30,
	},
	FormatWebP: {
		Type:      FormatWebP,
		MIMEType:  "image/webp",
		Extension: ".webp",
		Priority:  20,
	},
	FormatOriginal: {
		Type:      FormatOriginal,
		MIMEType:  "",
		Extension: "",
		Priority:  0,
	},
}

// ClientPreference 客户端偏好
type ClientPreference struct {
	FormatType FormatType
	QValue     float64
}

// Negotiator 格式协商器
type Negotiator struct {
	enabledFormats map[FormatType]bool
}

// NewNegotiator 创建协商器
func NewNegotiator(enabled []string) *Negotiator {
	enabledMap := make(map[FormatType]bool)
	for _, f := range enabled {
		enabledMap[FormatType(f)] = true
	}
	return &Negotiator{enabledFormats: enabledMap}
}

// Negotiate 执行格式协商
func (n *Negotiator) Negotiate(acceptHeader string, available map[FormatType]bool) FormatType {
	clientPrefs := parseAcceptHeader(acceptHeader)

	// 按优先级检查：AVIF > WebP > Original
	candidates := []FormatType{FormatAVIF, FormatWebP}

	for _, format := range candidates {
		// 服务器启用且客户端支持且变体可用
		if n.enabledFormats[format] && available[format] && clientSupports(clientPrefs, format) {
			return format
		}
	}

	return FormatOriginal
}

// clientSupports 检查客户端是否支持某格式
func clientSupports(prefs []ClientPreference, format FormatType) bool {
	for _, pref := range prefs {
		if pref.FormatType == format {
			return pref.QValue > 0
		}
	}

	for _, pref := range prefs {
		if pref.FormatType == "" && pref.QValue > 0 {
			return true
		}
	}

	return false
}

// parseAcceptHeader 解析 Accept 头
func parseAcceptHeader(header string) []ClientPreference {
	if header == "" {
		return nil
	}

	var prefs []ClientPreference
	parts := strings.Split(header, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		mimeType, qValue := parseMediaRange(part)

		format := mimeToFormat(mimeType)
		if format != "" || mimeType == "*/*" || strings.HasPrefix(mimeType, "image/*") {
			prefs = append(prefs, ClientPreference{
				FormatType: format,
				QValue:     qValue,
			})
		}
	}

	return prefs
}

// parseMediaRange 解析单个 media range
func parseMediaRange(part string) (string, float64) {
	qValue := 1.0

	if idx := strings.Index(part, ";"); idx != -1 {
		params := part[idx+1:]
		part = part[:idx]

		if qIdx := strings.Index(params, "q="); qIdx != -1 {
			qStr := params[qIdx+2:]
			if endIdx := strings.IndexAny(qStr, ";,"); endIdx != -1 {
				qStr = qStr[:endIdx]
			}
			if q, err := strconv.ParseFloat(strings.TrimSpace(qStr), 64); err == nil {
				qValue = q
			}
		}
	}

	return strings.TrimSpace(part), qValue
}

// mimeToFormat MIME 类型映射到 FormatType
func mimeToFormat(mime string) FormatType {
	switch mime {
	case "image/avif":
		return FormatAVIF
	case "image/webp":
		return FormatWebP
	default:
		return ""
	}
}
