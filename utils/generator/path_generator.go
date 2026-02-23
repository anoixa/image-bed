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

// StorageIdentifiers 存储标识对
type StorageIdentifiers struct {
	Identifier  string // 业务标识符，如 a1b2c3d4e5f6（不含扩展名）
	StoragePath string // 存储路径，如 original/2024/01/15/a1b2c3d4e5f6.jpg
}

// GenerateOriginalIdentifiers 生成原图的 identifier 和 storage_path
func (pg *PathGenerator) GenerateOriginalIdentifiers(fileHash string, ext string, uploadTime time.Time) StorageIdentifiers {
	hash := fileHash[:12]
	datePath := uploadTime.Format("2006/01/02")

	return StorageIdentifiers{
		Identifier:  hash,
		StoragePath: fmt.Sprintf("original/%s/%s%s", datePath, hash, ext),
	}
}

// GenerateThumbnailIdentifiers 生成缩略图的 identifier 和 storage_path
func (pg *PathGenerator) GenerateThumbnailIdentifiers(originalStoragePath string, width int) StorageIdentifiers {
	hash := pg.extractHashFromPath(originalStoragePath)
	datePath := pg.extractDatePath(originalStoragePath)
	identifier := fmt.Sprintf("%s_%d", hash, width)

	return StorageIdentifiers{
		Identifier:  identifier,
		StoragePath: fmt.Sprintf("thumbnails/%s/%s.webp", datePath, identifier),
	}
}

// GenerateConvertedIdentifiers 生成格式转换的 identifier 和 storage_path
func (pg *PathGenerator) GenerateConvertedIdentifiers(originalStoragePath string, format string) StorageIdentifiers {
	hash := pg.extractHashFromPath(originalStoragePath)
	datePath := pg.extractDatePath(originalStoragePath)

	switch format {
	case "webp":
		return StorageIdentifiers{
			Identifier:  hash,
			StoragePath: fmt.Sprintf("converted/webp/%s/%s.webp", datePath, hash),
		}
	case "avif":
		return StorageIdentifiers{
			Identifier:  hash,
			StoragePath: fmt.Sprintf("converted/avif/%s/%s.avif", datePath, hash),
		}
	case "jpegxl", "jxl":
		return StorageIdentifiers{
			Identifier:  hash,
			StoragePath: fmt.Sprintf("converted/jpegxl/%s/%s.jxl", datePath, hash),
		}
	default:
		return StorageIdentifiers{
			Identifier:  hash,
			StoragePath: fmt.Sprintf("converted/%s/%s/%s.%s", format, datePath, hash, format),
		}
	}
}

// extractHashFromPath 从存储路径中提取文件哈希
// 支持: original/2024/01/15/hash.jpg, thumbnails/2024/01/15/hash_300.webp 等格式
func (pg *PathGenerator) extractHashFromPath(storagePath string) string {
	// 取最后的文件名
	base := filepath.Base(storagePath)

	// 去除扩展名
	ext := filepath.Ext(base)
	hash := strings.TrimSuffix(base, ext)

	// 针对缩略图去除 "_300" 等后缀
	if idx := strings.LastIndex(hash, "_"); idx > 0 {
		if _, err := strconv.Atoi(hash[idx+1:]); err == nil {
			hash = hash[:idx]
		}
	}
	return hash
}

// extractDatePath 从存储路径中提取日期路径
func (pg *PathGenerator) extractDatePath(storagePath string) string {
	parts := strings.Split(storagePath, "/")

	if len(parts) >= 5 {
		return strings.Join(parts[1:4], "/")
	}
	// 路径不包含日期，返回当前日期
	return time.Now().Format("2006/01/02")
}

// ParseFormatFromStoragePath 从存储路径解析格式类型
func (pg *PathGenerator) ParseFormatFromStoragePath(storagePath string) string {
	parts := strings.Split(storagePath, "/")
	if len(parts) == 0 {
		return ""
	}

	// 根据目录结构判断格式
	typeDir := parts[0]
	switch typeDir {
	case "original":
		return "original"
	case "thumbnails":
		return "thumbnail"
	case "converted":
		if len(parts) >= 2 {
			return parts[1] // webp, avif, etc.
		}
	}
	return typeDir
}
