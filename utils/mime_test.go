package utils

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 各种图片类型的 Magic Bytes
var (
	// JPEG: FF D8 FF
	jpegMagic = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}
	// PNG: 89 50 4E 47
	pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	// GIF: GIF87a 或 GIF89a
	gifMagic = []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61}
	// WebP: RIFF....WEBP
	webpMagic = []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50}
	// BMP: BM
	bmpMagic = []byte{0x42, 0x4D, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
)

// TestSniffContentType_JPEG 测试JPEG检测
func TestSniffContentType_JPEG(t *testing.T) {
	reader := bytes.NewReader(jpegMagic)
	contentType, err := SniffContentType(reader)
	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", contentType)

	// 验证流被重置
	pos, _ := reader.Seek(0, 1)
	assert.Equal(t, int64(0), pos, "Stream should be reset to beginning")
}

// TestSniffContentType_PNG 测试PNG检测
func TestSniffContentType_PNG(t *testing.T) {
	reader := bytes.NewReader(pngMagic)
	contentType, err := SniffContentType(reader)
	require.NoError(t, err)
	assert.Equal(t, "image/png", contentType)
}

// TestSniffContentType_GIF 测试GIF检测
func TestSniffContentType_GIF(t *testing.T) {
	reader := bytes.NewReader(gifMagic)
	contentType, err := SniffContentType(reader)
	require.NoError(t, err)
	assert.Equal(t, "image/gif", contentType)
}

// TestSniffContentType_WebP 测试WebP检测
func TestSniffContentType_WebP(t *testing.T) {
	reader := bytes.NewReader(webpMagic)
	contentType, err := SniffContentType(reader)
	require.NoError(t, err)
	// WebP 可能不被所有系统支持
	assert.True(t, contentType == "image/webp" || contentType == "application/octet-stream",
		"Content type should be image/webp or application/octet-stream, got %s", contentType)
}

// TestSniffContentType_BMP 测试BMP检测
func TestSniffContentType_BMP(t *testing.T) {
	reader := bytes.NewReader(bmpMagic)
	contentType, err := SniffContentType(reader)
	require.NoError(t, err)
	assert.Equal(t, "image/bmp", contentType) // 或 image/x-ms-bmp，取决于系统
}

// TestSniffContentType_Text 测试文本类型检测
func TestSniffContentType_Text(t *testing.T) {
	reader := strings.NewReader("Hello, World!")
	contentType, err := SniffContentType(reader)
	require.NoError(t, err)
	assert.Equal(t, "text/plain; charset=utf-8", contentType)
}

// TestSniffContentType_Empty 测试空内容
func TestSniffContentType_Empty(t *testing.T) {
	reader := strings.NewReader("")
	contentType, err := SniffContentType(reader)
	require.NoError(t, err)
	// 空内容可能返回不同的默认类型
	assert.True(t, contentType == "application/octet-stream" || contentType == "text/plain; charset=utf-8",
		"Empty content type should be application/octet-stream or text/plain, got %s", contentType)
}

// TestSniffContentType_LargeData 测试大数据
func TestSniffContentType_LargeData(t *testing.T) {
	// 创建一个大于512字节的数据
	largeData := make([]byte, 1024)
	copy(largeData, jpegMagic) // 以JPEG开头

	reader := bytes.NewReader(largeData)
	contentType, err := SniffContentType(reader)
	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", contentType)
}

// BenchmarkSniffContentType 基准测试
func BenchmarkSniffContentType(b *testing.B) {
	data := make([]byte, 512)
	copy(data, jpegMagic)

	for i := 0; i < b.N; i++ {
		reader := bytes.NewReader(data)
		_, err := SniffContentType(reader)
		if err != nil {
			b.Fatal(err)
		}
	}
}
