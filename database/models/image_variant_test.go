package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatThumbnailSize(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		expect string
	}{
		{"small", 150, "thumbnail_150"},
		{"medium", 300, "thumbnail_300"},
		{"large", 600, "thumbnail_600"},
		{"custom", 800, "thumbnail_800"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatThumbnailSize(tt.width)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestParseThumbnailSize(t *testing.T) {
	tests := []struct {
		name      string
		format    string
		wantWidth int
		wantOk    bool
	}{
		{"valid_small", "thumbnail_150", 150, true},
		{"valid_medium", "thumbnail_300", 300, true},
		{"valid_large", "thumbnail_600", 600, true},
		{"invalid_format", "webp", 0, false},
		{"invalid_prefix", "thumb_300", 0, false},
		{"empty_string", "", 0, false},
		{"just_prefix", "thumbnail_", 0, false},
		{"non_numeric", "thumbnail_abc", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotWidth, gotOk := ParseThumbnailSize(tt.format)
			assert.Equal(t, tt.wantWidth, gotWidth)
			assert.Equal(t, tt.wantOk, gotOk)
		})
	}
}

func TestDefaultThumbnailSizes(t *testing.T) {
	assert.Len(t, DefaultThumbnailSizes, 3)
	assert.Equal(t, 150, DefaultThumbnailSizes[0].Width)
	assert.Equal(t, 300, DefaultThumbnailSizes[1].Width)
	assert.Equal(t, 600, DefaultThumbnailSizes[2].Width)
}

func TestFormatConstants(t *testing.T) {
	assert.Equal(t, "webp", FormatWebP)
	assert.Equal(t, "avif", FormatAVIF)
	assert.Equal(t, "thumbnail", FormatThumbnail)
}
