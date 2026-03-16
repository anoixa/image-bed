package validator

import (
	"bytes"
	"io"
	"net/http"
	"strings"
)

// allowedImageMimeTypes Allowed image types
var allowedImageMimeTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
	"image/bmp":  true,
}

// 文件魔数验证
var imageMagicNumbers = map[string][][]byte{
	"image/jpeg": {{0xFF, 0xD8, 0xFF}},
	"image/png":  {{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}},
	"image/gif":  {{0x47, 0x49, 0x46, 0x38, 0x37, 0x61}, {0x47, 0x49, 0x46, 0x38, 0x39, 0x61}},
	"image/webp": {{0x52, 0x49, 0x46, 0x46}}, // RIFF header, need more check
	"image/bmp":  {{0x42, 0x4D}},             // BM
}

// extensionToMimeType 扩展名到 MIME 类型映射
var extensionToMimeType = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
}

// IsImage Verify if the file content is an allowed image type.
func IsImage(file io.ReadSeeker, filename string) (bool, string, error) {
	// 1. 验证文件扩展名
	ext := strings.ToLower(strings.TrimSpace(getExtension(filename)))
	expectedMime, extValid := extensionToMimeType[ext]
	if !extValid {
		return false, "", nil
	}

	// 2. 读取文件头进行魔数验证
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return false, "", err
	}
	buffer = buffer[:n]

	// 3. 魔数验证
	if !validateMagicNumber(buffer, expectedMime) {
		return false, "", nil
	}

	// 4. 使用 http.DetectContentType 进行二次验证
	_, err = file.Seek(0, io.SeekStart)
	if err != nil {
		return false, "", err
	}

	detectedMime := http.DetectContentType(buffer)
	if detectedMime != expectedMime {
		// 允许一些变体，如 image/jpeg 检测为 image/pjpeg
		// WebP 可能不被系统支持，返回 application/octet-stream，但通过魔数验证即可
		if !isCompatibleMime(detectedMime, expectedMime) && expectedMime != "image/webp" {
			return false, "", nil
		}
	}

	return true, expectedMime, nil
}

// getExtension 获取文件扩展名
func getExtension(filename string) string {
	if idx := strings.LastIndex(filename, "."); idx != -1 {
		return filename[idx:]
	}
	return ""
}

// validateMagicNumber 验证文件魔数
func validateMagicNumber(data []byte, mimeType string) bool {
	// WebP 需要特殊验证：RIFF....WEBP
	if mimeType == "image/webp" {
		return len(data) >= 12 &&
			bytes.Equal(data[:4], []byte{0x52, 0x49, 0x46, 0x46}) && // RIFF
			bytes.Equal(data[8:12], []byte{0x57, 0x45, 0x42, 0x50}) // WEBP
	}

	magics, ok := imageMagicNumbers[mimeType]
	if !ok {
		return false
	}

	for _, magic := range magics {
		if len(data) >= len(magic) && bytes.Equal(data[:len(magic)], magic) {
			return true
		}
	}
	return false
}

// isCompatibleMime 检查检测到的 MIME 是否与期望的兼容
func isCompatibleMime(detected, expected string) bool {
	// 某些系统可能报告为 image/pjpeg 而不是 image/jpeg
	compat := map[string][]string{
		"image/jpeg": {"image/jpeg", "image/pjpeg", "image/jpg"},
		"image/png":  {"image/png", "image/x-png"},
		"image/gif":  {"image/gif"},
		"image/webp": {"image/webp"},
		"image/bmp":  {"image/bmp", "image/x-bmp", "image/x-ms-bmp"},
	}

	allowed, ok := compat[expected]
	if !ok {
		return detected == expected
	}

	for _, m := range allowed {
		if detected == m {
			return true
		}
	}
	return false
}

func IsImageBytes(data []byte) (bool, string) {
	mimeType := http.DetectContentType(data)

	if _, ok := allowedImageMimeTypes[mimeType]; ok {
		return true, mimeType
	}

	return false, ""
}
