package images

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/database/models"
	repoimages "github.com/anoixa/image-bed/database/repo/images"
	imageSvc "github.com/anoixa/image-bed/internal/image"
	randomsvc "github.com/anoixa/image-bed/internal/random"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupRandomHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Album{}, &models.Image{}, &models.ImageVariant{}))

	return db
}

func TestRandomImageReturnsNoContentWhenNoImageMatches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupRandomHandlerTestDB(t)
	repo := repoimages.NewRepository(db)
	handler := &Handler{
		readService: imageSvc.NewReadService(repo, nil, nil, nil, "http://localhost:8080", nil),
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/images/random", nil)

	handler.RandomImage(c)

	assert.Equal(t, http.StatusNoContent, c.Writer.Status())
}

func TestRandomImageReturnsServerErrorOnRepositoryFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupRandomHandlerTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	repo := repoimages.NewRepository(db)
	handler := &Handler{
		readService: imageSvc.NewReadService(repo, nil, nil, nil, "http://localhost:8080", nil),
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/images/random", nil)

	handler.RandomImage(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRandomImageAlbumIDZeroOverridesConfiguredSourceAlbum(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupRandomHandlerTestDB(t)
	repo := repoimages.NewRepository(db)

	publicImage := &models.Image{
		Identifier:   "random-public",
		OriginalName: "random.jpg",
		FileHash:     "random-public-hash",
		StoragePath:  "uploads/random.jpg",
		FileSize:     1024,
		MimeType:     "image/jpeg",
		UserID:       1,
		IsPublic:     true,
	}
	require.NoError(t, repo.SaveImage(publicImage))

	randomService := randomsvc.NewService(nil)
	require.NoError(t, randomService.SetSourceAlbum(999, false))

	handler := &Handler{
		baseURL:       "http://localhost:8080",
		readService:   imageSvc.NewReadService(repo, nil, nil, nil, "http://localhost:8080", nil),
		randomService: randomService,
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/images/random?album_id=0&format=json", nil)

	handler.RandomImage(c)

	require.Equal(t, http.StatusOK, w.Code)

	var response common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	dataBytes, err := json.Marshal(response.Data)
	require.NoError(t, err)

	var payload struct {
		Identifier string `json:"identifier"`
	}
	require.NoError(t, json.Unmarshal(dataBytes, &payload))
	assert.Equal(t, publicImage.Identifier, payload.Identifier)
}

func TestRandomImageConfiguredAlbumTakesPrecedenceOverIncludeAllPublic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupRandomHandlerTestDB(t)
	repo := repoimages.NewRepository(db)

	outsideImage := &models.Image{
		Identifier:   "outside-album",
		OriginalName: "outside.jpg",
		FileHash:     "outside-hash",
		StoragePath:  "uploads/outside.jpg",
		FileSize:     1024,
		MimeType:     "image/jpeg",
		UserID:       1,
		IsPublic:     true,
	}
	require.NoError(t, repo.SaveImage(outsideImage))

	albumImage := &models.Image{
		Identifier:   "inside-album",
		OriginalName: "inside.jpg",
		FileHash:     "inside-hash",
		StoragePath:  "uploads/inside.jpg",
		FileSize:     1024,
		MimeType:     "image/jpeg",
		UserID:       1,
		IsPublic:     true,
	}
	require.NoError(t, repo.SaveImage(albumImage))

	album := &models.Album{
		UserID: 1,
		Name:   "Random Source",
	}
	require.NoError(t, db.Create(album).Error)
	require.NoError(t, db.Exec("INSERT INTO album_images (album_id, image_id) VALUES (?, ?)", album.ID, albumImage.ID).Error)

	randomService := randomsvc.NewService(nil)
	require.NoError(t, randomService.SetSourceAlbum(album.ID, true))

	handler := &Handler{
		baseURL:       "http://localhost:8080",
		readService:   imageSvc.NewReadService(repo, nil, nil, nil, "http://localhost:8080", nil),
		randomService: randomService,
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/images/random?format=json", nil)

	handler.RandomImage(c)

	require.Equal(t, http.StatusOK, w.Code)

	var response common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	dataBytes, err := json.Marshal(response.Data)
	require.NoError(t, err)

	var payload struct {
		Identifier string `json:"identifier"`
	}
	require.NoError(t, json.Unmarshal(dataBytes, &payload))
	assert.Equal(t, albumImage.Identifier, payload.Identifier)

	_, includeAllPublic := randomService.GetSourceAlbum()
	assert.False(t, includeAllPublic)
}

func TestSetRandomSourceAlbumReturnsNormalizedSpecificAlbumConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	randomService := randomsvc.NewService(nil)
	handler := &Handler{
		randomService: randomService,
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/random-source-album",
		strings.NewReader(`{"album_id":123,"include_all_public":true}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")

	handler.SetRandomSourceAlbum(c)

	require.Equal(t, http.StatusOK, w.Code)

	var response common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	dataBytes, err := json.Marshal(response.Data)
	require.NoError(t, err)

	var payload struct {
		AlbumID          uint `json:"album_id"`
		IncludeAllPublic bool `json:"include_all_public"`
		Enabled          bool `json:"enabled"`
	}
	require.NoError(t, json.Unmarshal(dataBytes, &payload))

	assert.Equal(t, uint(123), payload.AlbumID)
	assert.False(t, payload.IncludeAllPublic)
	assert.True(t, payload.Enabled)
}

func TestSetRandomSourceAlbumCanDisableRandomAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	randomService := randomsvc.NewService(nil)
	handler := &Handler{
		randomService: randomService,
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/v1/admin/random-source-album",
		strings.NewReader(`{"album_id":0,"include_all_public":true,"enabled":false}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")

	handler.SetRandomSourceAlbum(c)

	require.Equal(t, http.StatusOK, w.Code)

	config := randomService.GetSourceConfig()
	assert.False(t, config.Enabled)
	assert.True(t, config.IncludeAllPublic)
}

func TestRandomImageReturnsForbiddenWhenRandomAPIDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupRandomHandlerTestDB(t)
	repo := repoimages.NewRepository(db)
	randomService := randomsvc.NewService(nil)
	require.NoError(t, randomService.SetSourceConfig(randomsvc.SourceConfig{
		Enabled: false,
	}))

	handler := &Handler{
		readService:   imageSvc.NewReadService(repo, nil, nil, nil, "http://localhost:8080", nil),
		randomService: randomService,
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/images/random?format=json", nil)

	handler.RandomImage(c)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRandomImageRejectsInvalidFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupRandomHandlerTestDB(t)
	repo := repoimages.NewRepository(db)
	handler := &Handler{
		readService: imageSvc.NewReadService(repo, nil, nil, nil, "http://localhost:8080", nil),
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/images/random?format=xml", bytes.NewReader(nil))

	handler.RandomImage(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestRespondRandomJSONUsesSelectedURLForVariantResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := &Handler{
		baseURL: "http://localhost:8080",
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	selectedURL := "https://cdn.example.com/selected.webp"
	result := &imageSvc.ImageResultDTO{
		Image: &models.Image{
			ID:         1,
			Identifier: "original-id",
			FileSize:   2048,
			MimeType:   "image/jpeg",
			IsPublic:   true,
		},
		IsOriginal: false,
		MIMEType:   "image/webp",
		URL:        selectedURL,
		Variant: &models.ImageVariant{
			Identifier: "variant-id",
			Format:     models.FormatWebP,
		},
	}

	handler.respondRandomJSON(c, result)

	require.Equal(t, http.StatusOK, w.Code)

	var response common.Response
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	dataBytes, err := json.Marshal(response.Data)
	require.NoError(t, err)

	var payload struct {
		URL         string `json:"url"`
		OriginalURL string `json:"original_url"`
		Variant     struct {
			URL string `json:"url"`
		} `json:"variant"`
	}
	require.NoError(t, json.Unmarshal(dataBytes, &payload))

	assert.Equal(t, selectedURL, payload.URL)
	assert.Equal(t, "http://localhost:8080/images/original-id", payload.OriginalURL)
	assert.Equal(t, selectedURL, payload.Variant.URL)
}
