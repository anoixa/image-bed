package images

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	imageSvc "github.com/anoixa/image-bed/internal/image"
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
	service := &imageSvc.ThumbnailService{}

	tests := []struct {
		name       string
		identifier string
		width      int
		want       string
	}{
		{
			name:       "new_format",
			identifier: "original/2024/01/15/image.jpg",
			width:      150,
			want:       "thumbnails/2024/01/15/image_150.webp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := service.GetThumbnailURL(tt.identifier, tt.width)
			assert.Equal(t, tt.want, got)
		})
	}
}
