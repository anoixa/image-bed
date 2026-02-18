package images

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestParseThumbnailWidth(t *testing.T) {
	h := &Handler{}

	// Create a test context
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		widthParam string
		want       int
	}{
		{"default_empty", "", 300},
		{"valid_150", "150", 150},
		{"valid_300", "300", 300},
		{"valid_600", "600", 600},
		{"invalid_string", "abc", 300},
		{"invalid_zero", "0", 300},
		{"invalid_negative", "-100", 300},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			if tt.widthParam != "" {
				c.Request = &http.Request{
					URL: &url.URL{
						RawQuery: "width=" + tt.widthParam,
					},
				}
			}
			got := h.parseThumbnailWidth(c)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestGetThumbnailURL(t *testing.T) {
	service := &ThumbnailService{}

	tests := []struct {
		name       string
		identifier string
		width      int
		want       string
	}{
		{
			name:       "simple",
			identifier: "abc123.png",
			width:      300,
			want:       "thumbnails/abc123.png_300.jpg",
		},
		{
			name:       "with_path",
			identifier: "2024/01/image.jpg",
			width:      150,
			want:       "thumbnails/2024/01/image.jpg_150.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.GetThumbnailURL(tt.identifier, tt.width)
			assert.Equal(t, tt.want, got)
		})
	}
}
