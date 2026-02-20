package utils

import (
	"fmt"
	"image"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
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
func GetSafeExtension(mimeType string) string {
	mimeType = strings.Split(mimeType, ";")[0]
	mimeType = strings.TrimSpace(mimeType)

	if ext, ok := mimeToExtMap[mimeType]; ok {
		return ext
	}
	return ""
}

// GetExtensionFromFilename 从文件名获取扩展名
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

// GetImageDimensions 从图片流中获取图片尺寸
func GetImageDimensions(stream io.ReadSeeker) (int, int) {
	currentPos, err := stream.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, 0
	}

	img, _, err := image.Decode(stream)
	if err != nil {
		stream.Seek(currentPos, io.SeekStart)
		return 0, 0
	}

	stream.Seek(currentPos, io.SeekStart)

	bounds := img.Bounds()
	return bounds.Dx(), bounds.Dy()
}
