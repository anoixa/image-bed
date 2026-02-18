package config

import (
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
)

func TestDefaultThumbnailSettings(t *testing.T) {
	settings := DefaultThumbnailSettings()

	assert.True(t, settings.Enabled)
	assert.Equal(t, 85, settings.Quality)
	assert.Equal(t, 3, settings.MaxRetries)
	assert.Len(t, settings.Sizes, 3)
	assert.Equal(t, 150, settings.Sizes[0].Width)
	assert.Equal(t, 300, settings.Sizes[1].Width)
	assert.Equal(t, 600, settings.Sizes[2].Width)
}

func TestThumbnailSettings_IsValidWidth(t *testing.T) {
	settings := &ThumbnailSettings{
		Sizes: []models.ThumbnailSize{
			{Name: "small", Width: 150},
			{Name: "medium", Width: 300},
			{Name: "large", Width: 600},
		},
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
			got := settings.IsValidWidth(tt.width)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestThumbnailSettings_GetSizeByWidth(t *testing.T) {
	settings := &ThumbnailSettings{
		Sizes: []models.ThumbnailSize{
			{Name: "small", Width: 150},
			{Name: "medium", Width: 300},
			{Name: "large", Width: 600},
		},
	}

	tests := []struct {
		name      string
		width     int
		wantName  string
		wantFound bool
	}{
		{"found_small", 150, "small", true},
		{"found_medium", 300, "medium", true},
		{"found_large", 600, "large", true},
		{"not_found", 200, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := settings.GetSizeByWidth(tt.width)
			if tt.wantFound {
				assert.NotNil(t, got)
				assert.Equal(t, tt.wantName, got.Name)
			} else {
				assert.Nil(t, got)
			}
		})
	}
}
