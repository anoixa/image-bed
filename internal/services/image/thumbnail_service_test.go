package image

import (
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
)

func TestGenerateThumbnailIdentifier(t *testing.T) {
	service := &ThumbnailService{}

	tests := []struct {
		name     string
		original string
		width    int
		want     string
	}{
		{
			name:     "simple_identifier",
			original: "abc123.png",
			width:    300,
			want:     "thumbnails/abc123.png_300.webp",
		},
		{
			name:     "path_with_slash",
			original: "images/photo.jpg",
			width:    150,
			want:     "thumbnails/images/photo.jpg_150.webp",
		},
		{
			name:     "large_width",
			original: "image.png",
			width:    600,
			want:     "thumbnails/image.png_600.webp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.GenerateThumbnailIdentifier(tt.original, tt.width)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetMIMETypeFromFormat(t *testing.T) {
	service := &ThumbnailService{}

	tests := []struct {
		format string
		want   string
	}{
		{"thumbnail_150", "image/webp"},
		{"thumbnail_300", "image/webp"},
		{"webp", "image/webp"},
		{"avif", "image/webp"},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got := service.getMIMETypeFromFormat(tt.format)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestThumbnailResult(t *testing.T) {
	result := &ThumbnailResult{
		Format:     "thumbnail_300",
		Identifier: "thumbnails/abc_300.jpg",
		Width:      300,
		Height:     200,
		MIMEType:   "image/jpeg",
	}

	assert.Equal(t, "thumbnail_300", result.Format)
	assert.Equal(t, "thumbnails/abc_300.jpg", result.Identifier)
	assert.Equal(t, 300, result.Width)
	assert.Equal(t, 200, result.Height)
	assert.Equal(t, "image/jpeg", result.MIMEType)
}

func TestNewThumbnailService(t *testing.T) {
	// This is a basic structure test
	// Full integration tests would require mocking the dependencies
	service := &ThumbnailService{}
	assert.NotNil(t, service)
}

func TestIsValidThumbnailWidthWithDefaultSizes(t *testing.T) {
	tests := []struct {
		width int
		valid bool
	}{
		{150, true},
		{300, true},
		{600, true},
		{200, false},
		{0, false},
		{-1, false},
		{1000, false},
	}

	for _, tt := range tests {
		t.Run("width_"+string(rune(tt.width)), func(t *testing.T) {
			got := models.IsValidThumbnailWidth(tt.width, models.DefaultThumbnailSizes)
			assert.Equal(t, tt.valid, got)
		})
	}
}
