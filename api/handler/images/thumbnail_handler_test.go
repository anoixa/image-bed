package images

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anoixa/image-bed/database/models"
	imageSvc "github.com/anoixa/image-bed/internal/image"
)

type thumbnailStreamProvider struct {
	streamCalls int
}

func (p *thumbnailStreamProvider) SaveWithContext(ctx context.Context, storagePath string, file io.Reader) error {
	return nil
}

func (p *thumbnailStreamProvider) GetWithContext(ctx context.Context, storagePath string) (io.ReadSeeker, error) {
	return nil, os.ErrNotExist
}

func (p *thumbnailStreamProvider) DeleteWithContext(ctx context.Context, storagePath string) error {
	return nil
}

func (p *thumbnailStreamProvider) Exists(ctx context.Context, storagePath string) (bool, error) {
	return true, nil
}

func (p *thumbnailStreamProvider) Health(ctx context.Context) error {
	return nil
}

func (p *thumbnailStreamProvider) Name() string {
	return "thumbnail-stream"
}

func (p *thumbnailStreamProvider) StreamTo(ctx context.Context, storagePath string, w http.ResponseWriter) (int64, error) {
	p.streamCalls++
	n, err := io.WriteString(w, "thumb-body")
	return int64(n), err
}

var _ storage.StreamProvider = (*thumbnailStreamProvider)(nil)

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

func TestServeThumbnailByStreaming(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{}
	provider := &thumbnailStreamProvider{}
	result := &imageSvc.ThumbnailResult{
		Identifier:  "thumb.webp",
		StoragePath: "thumbs/thumb.webp",
		FileHash:    "thumb-hash",
		MIMEType:    "image/webp",
	}
	image := &models.Image{IsPublic: true}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/thumbnails/test", nil)

	ok := h.serveThumbnailByStreaming(c, image, result, provider)

	require.True(t, ok)
	assert.Equal(t, 1, provider.streamCalls)
	assert.Equal(t, "thumb-body", w.Body.String())
	assert.Equal(t, "image/webp", w.Header().Get("Content-Type"))
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, `"thumb-hash"`, w.Header().Get("ETag"))
}

func TestServeThumbnailByStreamingReturnsNotModifiedOnETagMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{}
	provider := &thumbnailStreamProvider{}
	result := &imageSvc.ThumbnailResult{
		Identifier:  "thumb.webp",
		StoragePath: "thumbs/thumb.webp",
		FileHash:    "thumb-hash",
		MIMEType:    "image/webp",
	}
	image := &models.Image{IsPublic: true}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodGet, "/thumbnails/test", nil)
	req.Header.Set("If-None-Match", `W/"thumb-hash", "other"`)
	c.Request = req

	ok := h.serveThumbnailByStreaming(c, image, result, provider)

	require.True(t, ok)
	assert.Equal(t, 0, provider.streamCalls)
	assert.Equal(t, http.StatusNotModified, c.Writer.Status())
	assert.Equal(t, `"thumb-hash"`, w.Header().Get("ETag"))
}

func TestServeThumbnailByStreamingUsesPrivateCacheControlForPrivateImages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{}
	provider := &thumbnailStreamProvider{}
	result := &imageSvc.ThumbnailResult{
		Identifier:  "thumb.webp",
		StoragePath: "thumbs/thumb.webp",
		FileHash:    "thumb-hash",
		MIMEType:    "image/webp",
	}
	image := &models.Image{IsPublic: false}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/thumbnails/test", nil)

	ok := h.serveThumbnailByStreaming(c, image, result, provider)

	require.True(t, ok)
	assert.Equal(t, privateImageCacheControl, w.Header().Get("Cache-Control"))
}
