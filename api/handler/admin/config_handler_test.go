package admin

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type stubConfigManager struct {
	getConfigMaskSensitive bool
	getConfigCalls         int
	enableCalls            int
	disableCalls           int
	config                 *models.ConfigResponse
	getConfigErr           error
	disableErr             error
}

func (m *stubConfigManager) ListConfigs(ctx context.Context, category models.ConfigCategory, enabledOnly, maskSensitive bool) ([]*models.ConfigResponse, error) {
	return nil, nil
}

func (m *stubConfigManager) GetConfig(ctx context.Context, id uint, maskSensitive bool) (*models.ConfigResponse, error) {
	m.getConfigCalls++
	m.getConfigMaskSensitive = maskSensitive
	return m.config, m.getConfigErr
}

func (m *stubConfigManager) CreateConfig(ctx context.Context, req *models.SystemConfigStoreRequest, userID uint) (*models.ConfigResponse, error) {
	return nil, nil
}

func (m *stubConfigManager) UpdateConfig(ctx context.Context, id uint, req *models.SystemConfigStoreRequest) (*models.ConfigResponse, error) {
	return nil, nil
}

func (m *stubConfigManager) DeleteConfig(ctx context.Context, id uint) error {
	return nil
}

func (m *stubConfigManager) SetDefault(ctx context.Context, id uint) error {
	return nil
}

func (m *stubConfigManager) Enable(ctx context.Context, id uint) error {
	m.enableCalls++
	return nil
}

func (m *stubConfigManager) Disable(ctx context.Context, id uint) error {
	m.disableCalls++
	return m.disableErr
}

func (m *stubConfigManager) GetGlobalTransferMode(ctx context.Context) storage.TransferMode {
	return storage.TransferModeAuto
}

func (m *stubConfigManager) SetGlobalTransferMode(ctx context.Context, mode storage.TransferMode) error {
	return nil
}

func (m *stubConfigManager) ClearCache() {}

func TestEnableConfigReloadsWithUnmaskedStorageSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	manager := &stubConfigManager{
		config: &models.ConfigResponse{
			ID:        42,
			Category:  models.ConfigCategoryStorage,
			IsDefault: true,
			Config: map[string]any{
				"type":              "s3",
				"endpoint":          "https://s3.example.com",
				"bucket_name":       "images",
				"access_key_id":     "ACCESS_KEY",
				"secret_access_key": "SECRET_VALUE",
			},
		},
	}

	var reloaded map[string]any
	handler := &ConfigHandler{
		manager:    manager,
		imagesRepo: nil,
	}
	handler.reloadStorageConfig = func(id uint, config map[string]any, isDefault bool) error {
		reloaded = config
		return nil
	}

	router := gin.New()
	router.POST("/configs/:id/enable", handler.EnableConfig)

	req := httptest.NewRequest(http.MethodPost, "/configs/42/enable", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, manager.getConfigCalls)
	assert.False(t, manager.getConfigMaskSensitive, "storage reload must use unmasked secrets")
	assert.Equal(t, 1, manager.enableCalls)
	require.NotNil(t, reloaded)
	assert.Equal(t, "SECRET_VALUE", reloaded["secret_access_key"])
	assert.Equal(t, "ACCESS_KEY", reloaded["access_key_id"])
}

func TestValidateRemoteStorageTestTarget(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		target    string
		wantError bool
	}{
		{name: "Public HTTPS URL", target: "https://s3.amazonaws.com", wantError: false},
		{name: "Public endpoint without scheme", target: "s3.amazonaws.com", wantError: false},
		{name: "Loopback IPv4", target: "http://127.0.0.1:9000", wantError: true},
		{name: "Localhost hostname", target: "http://localhost:9000", wantError: true},
		{name: "Localhost subdomain", target: "http://minio.localhost", wantError: true},
		{name: "Private IPv4", target: "https://10.0.0.5", wantError: true},
		{name: "Carrier grade NAT", target: "https://100.64.0.10", wantError: true},
		{name: "Benchmark subnet", target: "https://198.18.0.1", wantError: true},
		{name: "IPv6 loopback", target: "http://[::1]:9000", wantError: true},
		{name: "IPv6 ULA", target: "http://[fd00::1]:9000", wantError: true},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := validateRemoteStorageTestTarget(tc.target)
			if tc.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "refusing to test")
				return
			}

			if err != nil && strings.Contains(err.Error(), "operation not permitted") {
				t.Skip("skipping public DNS resolution in sandboxed environment")
			}
			require.NoError(t, err)
		})
	}
}

func TestTestStorageConfigRejectsBlockedRemoteAddresses(t *testing.T) {
	t.Parallel()

	handler := &ConfigHandler{}

	t.Run("Rejects blocked S3 endpoint", func(t *testing.T) {
		t.Parallel()

		result := handler.testStorageConfig(context.Background(), map[string]any{
			"type":              "s3",
			"endpoint":          "127.0.0.1:9000",
			"bucket_name":       "images",
			"access_key_id":     "key",
			"secret_access_key": "secret",
		})

		require.NotNil(t, result)
		assert.False(t, result.Success)
		assert.True(t, strings.Contains(result.Message, "refusing to test"))
	})

	t.Run("Rejects blocked WebDAV URL", func(t *testing.T) {
		t.Parallel()

		result := handler.testStorageConfig(context.Background(), map[string]any{
			"type":        "webdav",
			"webdav_url":  "http://localhost:8080/dav",
			"webdav_root": "/",
		})

		require.NotNil(t, result)
		assert.False(t, result.Success)
		assert.True(t, strings.Contains(result.Message, "refusing to test"))
	})
}

func TestListConfigsRejectsSecurityCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewConfigHandler(&stubConfigManager{}, nil)
	router := gin.New()
	router.GET("/configs", handler.ListConfigs)

	req := httptest.NewRequest(http.MethodGet, "/configs?category=security", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateConfigRejectsSecurityCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewConfigHandler(&stubConfigManager{}, nil)
	router := gin.New()
	router.POST("/configs", handler.CreateConfig)

	body := bytes.NewBufferString(`{"category":"security","name":"x","config":{"enabled":true}}`)
	req := httptest.NewRequest(http.MethodPost, "/configs", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListConfigsRejectsJWTCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewConfigHandler(&stubConfigManager{}, nil)
	router := gin.New()
	router.GET("/configs", handler.ListConfigs)

	req := httptest.NewRequest(http.MethodGet, "/configs?category=jwt", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateConfigRejectsJWTCategory(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewConfigHandler(&stubConfigManager{}, nil)
	router := gin.New()
	router.POST("/configs", handler.CreateConfig)

	body := bytes.NewBufferString(`{"category":"jwt","name":"x","config":{"secret":"test-secret-key-at-least-32-characters-long"}}`)
	req := httptest.NewRequest(http.MethodPost, "/configs", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDisableConfigReturnsNotFoundForMissingConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewConfigHandler(&stubConfigManager{getConfigErr: gorm.ErrRecordNotFound}, nil)
	router := gin.New()
	router.POST("/configs/:id/disable", handler.DisableConfig)

	req := httptest.NewRequest(http.MethodPost, "/configs/42/disable", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestDisableConfigReturnsInternalErrorWhenGetConfigFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewConfigHandler(&stubConfigManager{getConfigErr: errors.New("decrypt failed")}, nil)
	router := gin.New()
	router.POST("/configs/:id/disable", handler.DisableConfig)

	req := httptest.NewRequest(http.MethodPost, "/configs/42/disable", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestGetConfigReturnsNotFoundForMissingConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewConfigHandler(&stubConfigManager{getConfigErr: gorm.ErrRecordNotFound}, nil)
	router := gin.New()
	router.GET("/configs/:id", handler.GetConfig)

	req := httptest.NewRequest(http.MethodGet, "/configs/42", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestGetConfigReturnsInternalErrorWhenGetConfigFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewConfigHandler(&stubConfigManager{getConfigErr: errors.New("decrypt failed")}, nil)
	router := gin.New()
	router.GET("/configs/:id", handler.GetConfig)

	req := httptest.NewRequest(http.MethodGet, "/configs/42", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSetDefaultConfigReturnsNotFoundForMissingConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewConfigHandler(&stubConfigManager{getConfigErr: gorm.ErrRecordNotFound}, nil)
	router := gin.New()
	router.POST("/configs/:id/default", handler.SetDefaultConfig)

	req := httptest.NewRequest(http.MethodPost, "/configs/42/default", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSetDefaultConfigReturnsInternalErrorWhenGetConfigFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewConfigHandler(&stubConfigManager{getConfigErr: errors.New("decrypt failed")}, nil)
	router := gin.New()
	router.POST("/configs/:id/default", handler.SetDefaultConfig)

	req := httptest.NewRequest(http.MethodPost, "/configs/42/default", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}
