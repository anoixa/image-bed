package images

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
