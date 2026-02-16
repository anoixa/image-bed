package utils

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

// mimeToExtMap MIME类型到安全扩展名的映射
var mimeToExtMap = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
	"image/bmp":  ".bmp",
}

// GetSafeExtension 根据MIME类型返回安全的文件扩展名
// 如果MIME类型不被允许，返回空字符串
func GetSafeExtension(mimeType string) string {
	// 标准化MIME类型（去除可能的参数）
	mimeType = strings.Split(mimeType, ";")[0]
	mimeType = strings.TrimSpace(mimeType)

	if ext, ok := mimeToExtMap[mimeType]; ok {
		return ext
	}
	return ""
}

// GetExtensionFromFilename 从文件名获取扩展名（小写）
func GetExtensionFromFilename(filename string) string {
	return strings.ToLower(filepath.Ext(filename))
}

func SniffContentType(stream io.ReadSeeker) (string, error) {
	buffer := make([]byte, 512)

	n, err := stream.Read(buffer)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("failed to read stream for mime sniffing: %w", err)
	}

	contentType := http.DetectContentType(buffer[:n])

	_, err = stream.Seek(0, io.SeekStart)
	if err != nil {
		return "", fmt.Errorf("failed to seek stream back to start after sniffing: %w", err)
	}

	return contentType, nil
}
