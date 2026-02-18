package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetThumbnailFormat(t *testing.T) {
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
			got := GetThumbnailFormat(tt.width)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestParseThumbnailWidth(t *testing.T) {
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
			gotWidth, gotOk := ParseThumbnailWidth(tt.format)
			assert.Equal(t, tt.wantWidth, gotWidth)
			assert.Equal(t, tt.wantOk, gotOk)
		})
	}
}

func TestIsThumbnailFormat(t *testing.T) {
	tests := []struct {
		name   string
		format string
		want   bool
	}{
		{"valid_small", "thumbnail_150", true},
		{"valid_medium", "thumbnail_300", true},
		{"invalid_webp", "webp", false},
		{"invalid_avif", "avif", false},
		{"empty", "", false},
		{"just_prefix", "thumbnail", false},
		{"similar_name", "thumbnail_backup", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsThumbnailFormat(tt.format)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsValidThumbnailWidth(t *testing.T) {
	sizes := []ThumbnailSize{
		{Name: "small", Width: 150},
		{Name: "medium", Width: 300},
		{Name: "large", Width: 600},
	}

	tests := []struct {
		name  string
		width int
		want  bool
	}{
		{"valid_small", 150, true},
		{"valid_medium", 300, true},
		{"valid_large", 600, true},
		{"invalid_200", 200, false},
		{"invalid_0", 0, false},
		{"invalid_negative", -100, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidThumbnailWidth(tt.width, sizes)
			assert.Equal(t, tt.want, got)
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
