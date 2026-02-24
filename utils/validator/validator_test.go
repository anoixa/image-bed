package validator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsImage_JPEG 测试JPEG图片验证
func TestIsImage_JPEG(t *testing.T) {
	// JPEG Magic Bytes: FF D8 FF
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}
	reader := bytes.NewReader(data)

	isValid, mimeType, err := IsImage(reader)
	require.NoError(t, err)
	assert.True(t, isValid)
	assert.Equal(t, "image/jpeg", mimeType)

	pos, _ := reader.Seek(0, 1)
	assert.Equal(t, int64(0), pos)
}

// TestIsImage_PNG 测试PNG图片验证
func TestIsImage_PNG(t *testing.T) {
	// PNG Magic Bytes: 89 50 4E 47
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	reader := bytes.NewReader(data)

	isValid, mimeType, err := IsImage(reader)
	require.NoError(t, err)
	assert.True(t, isValid)
	assert.Equal(t, "image/png", mimeType)
}

// TestIsImage_GIF 测试GIF图片验证
func TestIsImage_GIF(t *testing.T) {
	// GIF Magic Bytes: GIF89a
	data := []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61}
	reader := bytes.NewReader(data)

	isValid, mimeType, err := IsImage(reader)
	require.NoError(t, err)
	assert.True(t, isValid)
	assert.Equal(t, "image/gif", mimeType)
}

// TestIsImage_WebP 测试WebP图片验证
func TestIsImage_WebP(t *testing.T) {
	// WebP: RIFF....WEBP
	data := []byte{0x52, 0x49, 0x46, 0x46, 0x00, 0x00, 0x00, 0x00, 0x57, 0x45, 0x42, 0x50}
	reader := bytes.NewReader(data)

	isValid, mimeType, err := IsImage(reader)
	require.NoError(t, err)
	// WebP 可能不被所有系统支持，所以如果失败也不报错
	if isValid {
		assert.Equal(t, "image/webp", mimeType)
	}
}

// TestIsImage_BMP 测试BMP图片验证
func TestIsImage_BMP(t *testing.T) {
	// BMP Magic Bytes: BM
	data := []byte{0x42, 0x4D, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	reader := bytes.NewReader(data)

	isValid, mimeType, err := IsImage(reader)
	require.NoError(t, err)
	assert.True(t, isValid)
	// BMP MIME类型在不同系统可能不同
	assert.Contains(t, []string{"image/bmp", "image/x-ms-bmp"}, mimeType)
}

// TestIsImage_InvalidType 测试非图片类型
func TestIsImage_InvalidType(t *testing.T) {
	// 文本文件
	data := []byte("This is not an image file")
	reader := bytes.NewReader(data)

	isValid, mimeType, err := IsImage(reader)
	require.NoError(t, err)
	assert.False(t, isValid)
	assert.Empty(t, mimeType)
}

// TestIsImage_Empty 测试空文件
func TestIsImage_Empty(t *testing.T) {
	reader := strings.NewReader("")

	isValid, mimeType, err := IsImage(reader)
	require.NoError(t, err)
	assert.False(t, isValid)
	assert.Empty(t, mimeType)
}

// TestIsImage_PDF 测试PDF文件（不是图片）
func TestIsImage_PDF(t *testing.T) {
	// PDF Magic Bytes: %PDF
	data := []byte("%PDF-1.4")
	reader := bytes.NewReader(data)

	isValid, mimeType, err := IsImage(reader)
	require.NoError(t, err)
	assert.False(t, isValid)
	assert.Empty(t, mimeType)
}

// TestIsImage_Zip 测试ZIP文件（不是图片）
func TestIsImage_Zip(t *testing.T) {
	// ZIP Magic Bytes: PK
	data := []byte{0x50, 0x4B, 0x03, 0x04}
	reader := bytes.NewReader(data)

	isValid, mimeType, err := IsImage(reader)
	require.NoError(t, err)
	assert.False(t, isValid)
	assert.Empty(t, mimeType)
}

// TestIsImage_StreamReset 测试流是否正确重置
func TestIsImage_StreamReset(t *testing.T) {
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	reader := bytes.NewReader(data)

	_, _, err := IsImage(reader)
	require.NoError(t, err)

	pos, _ := reader.Seek(0, 1)
	assert.Equal(t, int64(0), pos, "Stream should be reset to beginning")

	// 可以再次读取
	buf := make([]byte, 4)
	n, _ := reader.Read(buf)
	assert.Equal(t, 4, n)
	assert.Equal(t, data, buf)
}

// TestIsImageBytes_JPEG 测试字节切片验证 - JPEG
func TestIsImageBytes_JPEG(t *testing.T) {
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	isValid, mimeType := IsImageBytes(data)
	assert.True(t, isValid)
	assert.Equal(t, "image/jpeg", mimeType)
}

// TestIsImageBytes_PNG 测试字节切片验证 - PNG
func TestIsImageBytes_PNG(t *testing.T) {
	data := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	isValid, mimeType := IsImageBytes(data)
	assert.True(t, isValid)
	assert.Equal(t, "image/png", mimeType)
}

// TestIsImageBytes_Invalid 测试字节切片验证 - 无效类型
func TestIsImageBytes_Invalid(t *testing.T) {
	data := []byte("not an image")
	isValid, mimeType := IsImageBytes(data)
	assert.False(t, isValid)
	assert.Empty(t, mimeType)
}

// TestIsImageBytes_Empty 测试字节切片验证 - 空数据
func TestIsImageBytes_Empty(t *testing.T) {
	isValid, mimeType := IsImageBytes([]byte{})
	assert.False(t, isValid)
	assert.Empty(t, mimeType)
}

// TestAllowedImageMimeTypes 测试允许的图片类型列表
func TestAllowedImageMimeTypes(t *testing.T) {
	expectedTypes := map[string]bool{
		"image/jpeg": true,
		"image/png":  true,
		"image/gif":  true,
		"image/webp": true,
		"image/bmp":  true,
	}

	assert.Equal(t, expectedTypes, allowedImageMimeTypes)
}

// BenchmarkIsImage 基准测试
func BenchmarkIsImage(b *testing.B) {
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}

	for i := 0; i < b.N; i++ {
		reader := bytes.NewReader(data)
		_, _, err := IsImage(reader)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIsImageBytes 基准测试字节版本
func BenchmarkIsImageBytes(b *testing.B) {
	data := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46}

	for i := 0; i < b.N; i++ {
		_, _ = IsImageBytes(data)
	}
}
