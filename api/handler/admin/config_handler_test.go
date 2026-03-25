package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/storage"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubConfigManager struct {
	getConfigMaskSensitive bool
	getConfigCalls         int
	enableCalls            int
	config                 *models.ConfigResponse
}

func (m *stubConfigManager) ListConfigs(ctx context.Context, category models.ConfigCategory, enabledOnly, maskSensitive bool) ([]*models.ConfigResponse, error) {
	return nil, nil
}

func (m *stubConfigManager) GetConfig(ctx context.Context, id uint, maskSensitive bool) (*models.ConfigResponse, error) {
	m.getConfigCalls++
	m.getConfigMaskSensitive = maskSensitive
	return m.config, nil
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
	return nil
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
