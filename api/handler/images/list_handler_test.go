package images

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	repoimages "github.com/anoixa/image-bed/database/repo/images"
	imagesvc "github.com/anoixa/image-bed/internal/image"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupListHandler(t *testing.T) (*Handler, *repoimages.Repository) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Album{}, &models.Image{}, &models.ImageVariant{}))

	provider, err := cache.NewMemoryCache(cache.MemoryConfig{
		NumCounters: 1_000,
		MaxCost:     1 << 20,
		BufferItems: 64,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = provider.Close()
	})

	repo := repoimages.NewRepository(db)
	variantRepo := repoimages.NewVariantRepository(db)
	helper := cache.NewHelper(provider)
	service := imagesvc.NewService(repo, variantRepo, nil, nil, nil, nil, helper, nil, "http://localhost:8080")

	return &Handler{
		cacheHelper:  helper,
		repo:         repo,
		imageService: service,
		baseURL:      "http://localhost:8080",
	}, repo
}

func TestListImagesNormalizesNegativePaginationValues(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler, repo := setupListHandler(t)
	for i := 1; i <= 5; i++ {
		err := repo.SaveImage(&models.Image{
			Identifier:   "image-list-" + string(rune('a'+i)),
			OriginalName: "image.jpg",
			FileHash:     "list-hash-" + string(rune('a'+i)),
			StoragePath:  "uploads/image.jpg",
			FileSize:     1024,
			MimeType:     "image/jpeg",
			UserID:       1,
			IsPublic:     true,
		})
		require.NoError(t, err)
	}

	router := gin.New()
	router.POST("/images", func(c *gin.Context) {
		c.Set(middleware.ContextUserIDKey, uint(1))
		handler.ListImages(c)
	})

	body := []byte(`{"page":-1,"limit":-1}`)
	req := httptest.NewRequest(http.MethodPost, "/images", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var response common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

	dataBytes, err := json.Marshal(response.Data)
	require.NoError(t, err)

	var payload ImageListResponse
	require.NoError(t, json.Unmarshal(dataBytes, &payload))
	assert.Equal(t, 1, payload.Page)
	assert.Equal(t, config.DefaultPerPage, payload.Limit)
	assert.Equal(t, 1, payload.TotalPages)
	assert.Equal(t, int64(5), payload.Total)
}
