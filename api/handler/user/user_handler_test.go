package user

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	svcuser "github.com/anoixa/image-bed/internal/user"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupUserHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Device{}))
	require.NoError(t, db.Exec("DELETE FROM devices").Error)
	require.NoError(t, db.Exec("DELETE FROM users").Error)
	return db
}

func TestGetCurrentUser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db := setupUserHandlerTestDB(t)
	accountsRepo := accounts.NewRepository(db)
	service := svcuser.NewService(accountsRepo, nil)
	handler := NewHandler(service)

	user := &models.User{
		Username: "admin",
		Password: "hashed",
		Role:     models.RoleAdmin,
		Status:   models.UserStatusActive,
	}
	require.NoError(t, accountsRepo.CreateUser(user))

	router := gin.New()
	router.GET("/api/auth/me", func(c *gin.Context) {
		c.Set(middleware.ContextUserIDKey, user.ID)
		handler.GetCurrentUser(c)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Status string              `json:"status"`
		Data   CurrentUserResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "success", resp.Status)
	assert.Equal(t, user.ID, resp.Data.ID)
	assert.Equal(t, "admin", resp.Data.Username)
	assert.Equal(t, models.RoleAdmin, resp.Data.Role)
	assert.Equal(t, models.UserStatusActive, resp.Data.Status)
}

func TestGetCurrentUserRequiresAuthContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewHandler(svcuser.NewService(nil, nil))
	router := gin.New()
	router.GET("/api/auth/me", handler.GetCurrentUser)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
