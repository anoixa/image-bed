package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anoixa/image-bed/api"
	"github.com/anoixa/image-bed/api/middleware"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/internal/admin"
	authpkg "github.com/anoixa/image-bed/internal/auth"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupUserHandlerTest(t *testing.T) (*gin.Engine, *admin.UserService, *authpkg.JWTService, uint) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}))
	db.Exec("DELETE FROM users")

	repo := accounts.NewRepository(db)
	devicesRepo := accounts.NewDeviceRepository(db)
	svc := admin.NewUserService(repo, devicesRepo, nil, nil, nil)
	handler := NewUserHandler(svc)

	jwtSvc, err := api.NewTestJWTService("0123456789abcdef0123456789abcdef", "15m", "24h")
	require.NoError(t, err)

	// Create an admin user for auth — this ensures the JWT userID matches a real admin
	adminUser := &models.User{
		Username: "test-admin",
		Password: "hash",
		Role:     models.RoleAdmin,
		Status:   models.UserStatusActive,
	}
	require.NoError(t, repo.CreateUser(adminUser))

	router := gin.New()
	adminGroup := router.Group("/api/v1/admin")
	adminGroup.Use(middleware.CombinedAuth(jwtSvc))
	adminGroup.Use(middleware.Authorize(middleware.AllowJWTOnly...))
	adminGroup.Use(middleware.RequireRole(middleware.RoleAdmin))
	{
		adminGroup.GET("/users", handler.ListUsers)
		adminGroup.POST("/users", handler.CreateUser)
		adminGroup.PUT("/users/:id/role", handler.UpdateRole)
		adminGroup.PUT("/users/:id/status", handler.UpdateStatus)
		adminGroup.POST("/users/:id/reset-password", handler.ResetPassword)
		adminGroup.DELETE("/users/:id", handler.DeleteUser)
	}

	return router, svc, jwtSvc, adminUser.ID
}

func adminAuthHeader(jwtSvc *authpkg.JWTService, adminUserID uint) string {
	tokenPair, err := jwtSvc.GenerateTokens("test-admin", adminUserID, middleware.RoleAdmin)
	if err != nil {
		panic(err)
	}
	return "Bearer " + tokenPair.AccessToken
}

func TestListUsersHandler(t *testing.T) {
	router, svc, jwtSvc, adminID := setupUserHandlerTest(t)

	// Create test users via service
	_, _, err := svc.CreateUser("user1", "password123", models.RoleUser)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	req.Header.Set("Authorization", adminAuthHeader(jwtSvc, adminID))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "success", resp["status"])
}

func TestCreateUserHandler(t *testing.T) {
	router, _, jwtSvc, adminID := setupUserHandlerTest(t)

	body, _ := json.Marshal(map[string]string{
		"username": "newuser",
		"password": "password123",
		"role":     "user",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader(body))
	req.Header.Set("Authorization", adminAuthHeader(jwtSvc, adminID))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCreateUserHandlerEmptyBody(t *testing.T) {
	router, _, jwtSvc, adminID := setupUserHandlerTest(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", nil)
	req.Header.Set("Authorization", adminAuthHeader(jwtSvc, adminID))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteUserHandler(t *testing.T) {
	router, svc, jwtSvc, adminID := setupUserHandlerTest(t)

	user, _, err := svc.CreateUser("to-delete", "password123", models.RoleUser)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/admin/users/%d", user.ID), nil)
	req.Header.Set("Authorization", adminAuthHeader(jwtSvc, adminID))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
