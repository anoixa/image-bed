package admin

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/anoixa/image-bed/api/common"
	appconfig "github.com/anoixa/image-bed/config"
	configsvc "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/internal/auth"
	"github.com/gin-gonic/gin"
)

var supportedOAuthProviders = []string{"github", "google", "gitee"}

type authSettingsManager interface {
	GetAuthSettings(ctx context.Context, fallbackPasswordLoginEnabled bool) (*configsvc.AuthSettings, error)
	SetAuthSettings(ctx context.Context, settings *configsvc.AuthSettings) error
}

type AuthSettingsHandler struct {
	manager      authSettingsManager
	oauthService *auth.OAuthService
	cfg          *appconfig.Config
}

type AuthSettingsResponse struct {
	PasswordLoginEnabled bool                `json:"password_login_enabled"`
	OAuthLoginEnabled    bool                `json:"oauth_login_enabled"`
	Providers            []auth.ProviderInfo `json:"providers"`
	CallbackURLs         map[string]string   `json:"callback_urls"`
}

type UpdateAuthSettingsRequest struct {
	PasswordLoginEnabled *bool `json:"password_login_enabled" binding:"required"`
}

func NewAuthSettingsHandler(manager authSettingsManager, oauthService *auth.OAuthService, cfg *appconfig.Config) *AuthSettingsHandler {
	return &AuthSettingsHandler{
		manager:      manager,
		oauthService: oauthService,
		cfg:          cfg,
	}
}

// GetSettings returns runtime login settings for the admin login settings page.
// @Summary      Get login settings
// @Description  Get password login switch, enabled OAuth providers, and callback URLs.
// @Tags         admin
// @Produce      json
// @Success      200  {object}  common.Response{data=AuthSettingsResponse}  "Login settings"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      403  {object}  common.Response  "Forbidden"
// @Failure      500  {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/auth/settings [get]
func (h *AuthSettingsHandler) GetSettings(c *gin.Context) {
	response, err := h.buildResponse(c.Request.Context())
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get auth settings")
		return
	}
	common.RespondSuccess(c, response)
}

// UpdateSettings updates runtime login settings.
// @Summary      Update login settings
// @Description  Update password login availability. Disabling password login requires at least one enabled OAuth provider and a linked OAuth identity for the current admin.
// @Tags         admin
// @Accept       json
// @Produce      json
// @Param        request  body      UpdateAuthSettingsRequest  true  "Login settings"
// @Success      200      {object}  common.Response{data=AuthSettingsResponse}  "Login settings"
// @Failure      400      {object}  common.Response  "Invalid request or unsafe lockout risk"
// @Failure      401      {object}  common.Response  "Unauthorized"
// @Failure      403      {object}  common.Response  "Forbidden"
// @Failure      500      {object}  common.Response  "Internal server error"
// @Security     ApiKeyAuth
// @Router       /api/v1/admin/auth/settings [put]
func (h *AuthSettingsHandler) UpdateSettings(c *gin.Context) {
	var req UpdateAuthSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.RespondError(c, http.StatusBadRequest, "Invalid request body")
		return
	}

	passwordLoginEnabled := *req.PasswordLoginEnabled
	if !passwordLoginEnabled {
		if err := h.ensureSafeToDisablePasswordLogin(c.Request.Context(), c.GetUint("user_id")); err != nil {
			common.RespondError(c, http.StatusBadRequest, err.Error())
			return
		}
	}

	if err := h.manager.SetAuthSettings(c.Request.Context(), &configsvc.AuthSettings{
		PasswordLoginEnabled: passwordLoginEnabled,
	}); err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to update auth settings")
		return
	}
	if h.oauthService != nil {
		h.oauthService.SetPasswordLoginEnabled(passwordLoginEnabled)
	}

	response, err := h.buildResponse(c.Request.Context())
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get auth settings")
		return
	}
	common.RespondSuccess(c, response)
}

func (h *AuthSettingsHandler) ensureSafeToDisablePasswordLogin(ctx context.Context, userID uint) error {
	if h.oauthService == nil || len(h.oauthService.ListProviders()) == 0 {
		return fmt.Errorf("configure and enable at least one OAuth provider before disabling password login")
	}
	if userID == 0 {
		return fmt.Errorf("current admin is not authenticated")
	}

	identities, err := h.oauthService.GetUserIdentities(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to check current admin OAuth identities")
	}
	if len(identities) == 0 {
		return fmt.Errorf("bind an OAuth identity to the current admin before disabling password login")
	}
	return nil
}

func (h *AuthSettingsHandler) buildResponse(ctx context.Context) (*AuthSettingsResponse, error) {
	fallback := true
	if h.cfg != nil {
		fallback = h.cfg.AuthPasswordLoginEnabled
	}

	settings, err := h.manager.GetAuthSettings(ctx, fallback)
	if err != nil {
		return nil, err
	}

	providers := []auth.ProviderInfo{}
	if h.oauthService != nil {
		providers = h.oauthService.ListProviders()
	}

	return &AuthSettingsResponse{
		PasswordLoginEnabled: settings.PasswordLoginEnabled,
		OAuthLoginEnabled:    len(providers) > 0,
		Providers:            providers,
		CallbackURLs:         h.callbackURLs(),
	}, nil
}

func (h *AuthSettingsHandler) callbackURLs() map[string]string {
	result := make(map[string]string, len(supportedOAuthProviders))
	baseURL := ""
	if h.cfg != nil {
		baseURL = strings.TrimRight(h.cfg.BaseURL(), "/")
	}
	for _, provider := range supportedOAuthProviders {
		if baseURL == "" {
			result[provider] = fmt.Sprintf("/api/auth/oauth/%s/callback", provider)
			continue
		}
		result[provider] = fmt.Sprintf("%s/api/auth/oauth/%s/callback", baseURL, provider)
	}
	return result
}
