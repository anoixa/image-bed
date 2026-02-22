package generator

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PathGenerator 分层路径生成器
type PathGenerator struct{}

// NewPathGenerator 创建路径生成器
func NewPathGenerator() *PathGenerator {
	return &PathGenerator{}
}

// GenerateOriginalIdentifier 生成原图 identifier
// 格式: original/2024/01/15/a1b2c3d4e5f6.jpg
func (pg *PathGenerator) GenerateOriginalIdentifier(fileHash string, ext string, uploadTime time.Time) string {
	datePath := uploadTime.Format("2006/01/02")
	return fmt.Sprintf("original/%s/%s%s", datePath, fileHash[:12], ext)
}

// GenerateThumbnailIdentifier 生成缩略图 identifier
// 格式: thumbnails/2024/01/15/a1b2c3d4e5f6_300.webp
func (pg *PathGenerator) GenerateThumbnailIdentifier(originalIdentifier string, width int) string {
	hash := pg.extractHash(originalIdentifier)
	datePath := pg.extractDatePath(originalIdentifier)
	if datePath != "" {
		return fmt.Sprintf("thumbnails/%s/%s_%d.webp", datePath, hash, width)
	}
	return fmt.Sprintf("thumbnails/%s_%d.webp", hash, width)
}

// GenerateConvertedIdentifier 生成格式转换 identifier
// 格式: converted/webp/2024/01/15/a1b2c3d4e5f6.webp
func (pg *PathGenerator) GenerateConvertedIdentifier(originalIdentifier string, format string) string {
	hash := pg.extractHash(originalIdentifier)
	datePath := pg.extractDatePath(originalIdentifier)

	switch format {
	case "webp":
		if datePath != "" {
			return fmt.Sprintf("converted/webp/%s/%s.webp", datePath, hash)
		}
		return fmt.Sprintf("converted/webp/%s.webp", hash)
	case "avif":
		if datePath != "" {
			return fmt.Sprintf("converted/avif/%s/%s.avif", datePath, hash)
		}
		return fmt.Sprintf("converted/avif/%s.avif", hash)
	case "jpegxl", "jxl":
		if datePath != "" {
			return fmt.Sprintf("converted/jpegxl/%s/%s.jxl", datePath, hash)
		}
		return fmt.Sprintf("converted/jpegxl/%s.jxl", hash)
	default:
		if datePath != "" {
			return fmt.Sprintf("converted/%s/%s/%s.%s", format, datePath, hash, format)
		}
		return fmt.Sprintf("converted/%s/%s.%s", format, hash, format)
	}
}

// extractHash 从 identifier 中提取纯净的文件哈希
func (pg *PathGenerator) extractHash(identifier string) string {
	base := filepath.Base(identifier)

	ext := filepath.Ext(base)
	hash := strings.TrimSuffix(base, ext)

	if idx := strings.LastIndex(hash, "_"); idx > 0 {
		if _, err := strconv.Atoi(hash[idx+1:]); err == nil {
			hash = hash[:idx]
		}
	}
	return hash
}

// extractDatePath 从 identifier 中提取日期路径
func (pg *PathGenerator) extractDatePath(identifier string) string {
	parts := strings.Split(identifier, "/")
	if len(parts) >= 4 {
		return strings.Join(parts[1:4], "/")
	}
	return ""
}

// IsHierarchicalPath 判断是否为分层路径格式
func (pg *PathGenerator) IsHierarchicalPath(identifier string) bool {
	return strings.Contains(identifier, "/")
}
