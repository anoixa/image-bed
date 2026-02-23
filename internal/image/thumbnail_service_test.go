package image

import (
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
)

func TestGenerateThumbnailIdentifiers(t *testing.T) {
	service := &ThumbnailService{}

	tests := []struct {
		name            string
		originalPath    string
		width           int
		wantIdentifier  string
		wantStoragePath string
	}{
		{
			name:            "new_format_path",
			originalPath:    "original/2024/01/15/a1b2c3d4e5f6.jpg",
			width:           300,
			wantIdentifier:  "a1b2c3d4e5f6_300",
			wantStoragePath: "thumbnails/2024/01/15/a1b2c3d4e5f6_300.webp",
		},
		{
			name:            "large_width",
			originalPath:    "original/2024/01/15/xyz789.jpg",
			width:           600,
			wantIdentifier:  "xyz789_600",
			wantStoragePath: "thumbnails/2024/01/15/xyz789_600.webp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.GenerateThumbnailIdentifiers(tt.originalPath, tt.width)
			assert.Equal(t, tt.wantIdentifier, got.Identifier)
			assert.Equal(t, tt.wantStoragePath, got.StoragePath)
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
		Format:      "thumbnail_300",
		Identifier:  "abc_300",
		StoragePath: "thumbnails/2024/01/15/abc_300.webp",
		Width:       300,
		Height:      200,
		MIMEType:    "image/jpeg",
	}

	assert.Equal(t, "thumbnail_300", result.Format)
	assert.Equal(t, 300, result.Width)
	assert.Equal(t, 200, result.Height)
}

func TestFormatThumbnailSize(t *testing.T) {
	tests := []struct {
		width int
		want  string
	}{
		{150, "thumbnail_150"},
		{300, "thumbnail_300"},
		{600, "thumbnail_600"},
		{0, "thumbnail_0"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := formatThumbnailSize(tt.width)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsValidThumbnailWidth(t *testing.T) {
	sizes := []models.ThumbnailSize{
		{Width: 150, Height: 0},
		{Width: 300, Height: 0},
		{Width: 600, Height: 0},
	}

	tests := []struct {
		name  string
		width int
		want  bool
	}{
		{"valid 150", 150, true},
		{"valid 300", 300, true},
		{"valid 600", 600, true},
		{"invalid 100", 100, false},
		{"invalid 500", 500, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isValidThumbnailWidth(tt.width, sizes)
			assert.Equal(t, tt.want, got)
		})
	}
}
