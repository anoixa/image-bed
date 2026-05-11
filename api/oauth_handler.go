package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/internal/auth"

	"github.com/gin-gonic/gin"
)

// OAuthHandler handles OAuth login, link, and unlink flows.
type OAuthHandler struct {
	oauthService *auth.OAuthService
	loginService *auth.LoginService
	cfg          *config.Config
}

type oauthProvidersResponse struct {
	Providers []auth.ProviderInfo `json:"providers"`
}

type authCapabilitiesResponse struct {
	PasswordLoginEnabled bool                `json:"password_login_enabled" example:"true"`
	OAuthLoginEnabled    bool                `json:"oauth_login_enabled" example:"true"`
	Providers            []auth.ProviderInfo `json:"providers"`
}

type oauthIdentityResponse struct {
	ID            uint      `json:"id"`
	Provider      string    `json:"provider" example:"github"`
	Username      string    `json:"username,omitempty"`
	Email         string    `json:"email,omitempty"`
	EmailVerified bool      `json:"email_verified"`
	AvatarURL     string    `json:"avatar_url,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type oauthIdentitiesResponse struct {
	Identities []oauthIdentityResponse `json:"identities"`
}

// NewOAuthHandler creates a new OAuthHandler.
func NewOAuthHandler(oauthService *auth.OAuthService, loginService *auth.LoginService, cfg *config.Config) *OAuthHandler {
	return &OAuthHandler{
		oauthService: oauthService,
		loginService: loginService,
		cfg:          cfg,
	}
}

// ListProviders returns enabled OAuth providers.
// @Summary      List OAuth providers
// @Description  Get enabled OAuth providers for login-page rendering.
// @Tags         auth
// @Produce      json
// @Success      200  {object}  common.Response{data=oauthProvidersResponse}  "OAuth provider list"
// @Router       /api/auth/oauth/providers [get]
func (h *OAuthHandler) ListProviders(c *gin.Context) {
	providers := h.oauthService.ListProviders()
	common.RespondSuccess(c, oauthProvidersResponse{Providers: providers})
}

// StartLogin initiates an OAuth login flow.
// @Summary      Start OAuth login
// @Description  Start an OAuth login flow. Redirects to the provider authorization page.
// @Tags         auth
// @Param        provider   path   string  true   "OAuth provider"  Enums(github, google, gitee)
// @Param        return_to  query  string  false  "Same-site relative return path"
// @Success      302       "Redirect to OAuth provider"
// @Failure      400       {object}  common.Response  "Provider not enabled or invalid return_to"
// @Failure      500       {object}  common.Response  "Failed to start OAuth flow"
// @Router       /api/auth/oauth/{provider}/start [get]
func (h *OAuthHandler) StartLogin(c *gin.Context) {
	providerName := c.Param("provider")
	returnTo := c.Query("return_to")
	if returnTo == "" {
		returnTo = "/"
	}

	authURL, cookies, err := h.oauthService.StartLogin(providerName, returnTo)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrProviderNotEnabled):
			common.RespondError(c, http.StatusBadRequest, "Provider not enabled")
		case errors.Is(err, auth.ErrInvalidReturnTo):
			common.RespondError(c, http.StatusBadRequest, "Invalid return_to path")
		default:
			common.RespondError(c, http.StatusInternalServerError, "Failed to start OAuth flow")
		}
		return
	}

	for _, cookie := range cookies {
		http.SetCookie(c.Writer, cookie)
	}
	c.Redirect(http.StatusFound, authURL)
}

// Callback handles the OAuth provider callback for both login and link flows.
// The state mode (login vs link) determines the redirect target on error/success.
// @Summary      OAuth callback
// @Description  Complete an OAuth login or account-link flow. On login success it sets auth cookies and redirects to return_to; on failure it redirects with oauth_error.
// @Tags         auth
// @Param        provider  path   string  true  "OAuth provider"  Enums(github, google, gitee)
// @Param        code      query  string  true  "Authorization code"
// @Param        state     query  string  true  "Signed OAuth state"
// @Success      302       "Redirect to return_to, /login, or /settings/account"
// @Router       /api/auth/oauth/{provider}/callback [get]
func (h *OAuthHandler) Callback(c *gin.Context) {
	providerName := c.Param("provider")
	code := c.Query("code")
	rawState := c.Query("state")

	if code == "" {
		c.Redirect(http.StatusFound, "/login?oauth_error=missing_code")
		return
	}

	// Read cookies
	cookieState, _ := c.Cookie(auth.StateCookieName())
	pkceVerifier, _ := c.Cookie(auth.PKCECookieName())
	nonce, _ := c.Cookie(auth.NonceCookieName())

	// Clear cookies
	clearOAuthCookies(c)

	result, returnTo, mode, err := h.oauthService.HandleCallback(c.Request.Context(), providerName, code, rawState, cookieState, pkceVerifier, nonce)
	if err != nil {
		h.redirectError(c, mode, err)
		return
	}

	// For login mode, result has session data
	if result != nil {
		refreshTokenMaxAge := int(time.Until(result.RefreshTokenExpiry).Seconds())
		setAuthCookies(c, result.RefreshToken, result.DeviceID, refreshTokenMaxAge)
	}

	// For link mode, append success indicator
	if mode == auth.StateModeLink {
		c.Redirect(http.StatusFound, appendQueryParam(returnTo, "linked", providerName))
		return
	}

	c.Redirect(http.StatusFound, returnTo)
}

// redirectError redirects to the appropriate page with an error parameter
// based on whether this was a login or link flow.
func (h *OAuthHandler) redirectError(c *gin.Context, mode string, err error) {
	base := "/login"
	if mode == auth.StateModeLink {
		base = "/settings/account"
	}

	errorCode := "internal"
	switch {
	case errors.Is(err, auth.ErrIdentityNotLinked):
		errorCode = "not_linked"
	case errors.Is(err, auth.ErrUserDisabled):
		errorCode = "disabled"
	case errors.Is(err, auth.ErrInvalidState):
		errorCode = "invalid_state"
	case errors.Is(err, auth.ErrIdentityAlreadyBound):
		errorCode = "already_bound"
	}

	c.Redirect(http.StatusFound, base+"?oauth_error="+errorCode)
}

func appendQueryParam(rawURL, key, value string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	query := parsed.Query()
	query.Set(key, value)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// GetIdentities returns the current user's linked identities.
// @Summary      List linked OAuth identities
// @Description  Get OAuth identities linked to the current user.
// @Tags         auth
// @Produce      json
// @Success      200  {object}  common.Response{data=oauthIdentitiesResponse}  "Linked identities"
// @Failure      401  {object}  common.Response  "Unauthorized"
// @Failure      500  {object}  common.Response  "Failed to get identities"
// @Security     ApiKeyAuth
// @Router       /api/auth/oauth/identities [get]
func (h *OAuthHandler) GetIdentities(c *gin.Context) {
	userID := c.GetUint("user_id")
	identities, err := h.oauthService.GetUserIdentities(c.Request.Context(), userID)
	if err != nil {
		common.RespondError(c, http.StatusInternalServerError, "Failed to get identities")
		return
	}
	common.RespondSuccess(c, oauthIdentitiesResponse{Identities: buildOAuthIdentityResponses(identities)})
}

// StartLink initiates an OAuth link flow for an authenticated user.
// @Summary      Start OAuth account link
// @Description  Start linking an OAuth provider to the current authenticated user. Redirects to the provider authorization page.
// @Tags         auth
// @Param        provider   path   string  true   "OAuth provider"  Enums(github, google, gitee)
// @Param        return_to  query  string  false  "Same-site relative return path"
// @Success      302       "Redirect to OAuth provider"
// @Failure      400       {object}  common.Response  "Provider not enabled or invalid return_to"
// @Failure      401       {object}  common.Response  "Unauthorized"
// @Failure      500       {object}  common.Response  "Failed to start link flow"
// @Security     ApiKeyAuth
// @Router       /api/auth/oauth/{provider}/link/start [post]
func (h *OAuthHandler) StartLink(c *gin.Context) {
	providerName := c.Param("provider")
	returnTo := c.Query("return_to")
	if returnTo == "" {
		returnTo = "/settings/account"
	}
	userID := c.GetUint("user_id")

	authURL, cookies, err := h.oauthService.StartLink(providerName, returnTo, userID)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrProviderNotEnabled):
			common.RespondError(c, http.StatusBadRequest, "Provider not enabled")
		case errors.Is(err, auth.ErrInvalidReturnTo):
			common.RespondError(c, http.StatusBadRequest, "Invalid return_to path")
		default:
			common.RespondError(c, http.StatusInternalServerError, "Failed to start link flow")
		}
		return
	}

	for _, cookie := range cookies {
		http.SetCookie(c.Writer, cookie)
	}
	c.Redirect(http.StatusFound, authURL)
}

// UnlinkIdentity removes an OAuth provider identity from the current user.
// @Summary      Unlink OAuth identity
// @Description  Remove a linked OAuth identity from the current user. Refuses to remove the last usable login method.
// @Tags         auth
// @Produce      json
// @Param        provider  path      string  true  "OAuth provider"  Enums(github, google, gitee)
// @Success      200       {object}  common.Response  "Identity unlinked"
// @Failure      401       {object}  common.Response  "Unauthorized"
// @Failure      404       {object}  common.Response  "Identity not found"
// @Failure      409       {object}  common.Response  "Cannot unlink the last login method"
// @Failure      500       {object}  common.Response  "Failed to unlink identity"
// @Security     ApiKeyAuth
// @Router       /api/auth/oauth/identities/{provider} [delete]
func (h *OAuthHandler) UnlinkIdentity(c *gin.Context) {
	providerName := c.Param("provider")
	userID := c.GetUint("user_id")

	err := h.oauthService.UnlinkIdentity(c.Request.Context(), userID, providerName)
	if err != nil {
		switch {
		case strings.Contains(err.Error(), "identity not found"):
			common.RespondError(c, http.StatusNotFound, "Identity not found")
		case errors.Is(err, auth.ErrLastLoginMethod):
			common.RespondError(c, http.StatusConflict, "Cannot unlink the last login method")
		default:
			common.RespondError(c, http.StatusInternalServerError, "Failed to unlink identity")
		}
		return
	}

	common.RespondSuccessMessage(c, "Identity unlinked", nil)
}

// Capabilities returns enabled authentication capabilities.
// @Summary      Get auth capabilities
// @Description  Get password-login and OAuth-login availability for frontend login-page rendering.
// @Tags         auth
// @Produce      json
// @Success      200  {object}  common.Response{data=authCapabilitiesResponse}  "Authentication capabilities"
// @Router       /api/auth/capabilities [get]
func (h *OAuthHandler) Capabilities(c *gin.Context) {
	providers := h.oauthService.ListProviders()
	oauthEnabled := len(providers) > 0

	passwordEnabled := true
	if h.cfg != nil {
		passwordEnabled = h.cfg.AuthPasswordLoginEnabled
	}

	common.RespondSuccess(c, authCapabilitiesResponse{
		PasswordLoginEnabled: passwordEnabled,
		OAuthLoginEnabled:    oauthEnabled,
		Providers:            providers,
	})
}

// clearOAuthCookies removes the OAuth state, PKCE, and nonce cookies.
func clearOAuthCookies(c *gin.Context) {
	for _, name := range []string{auth.StateCookieName(), auth.PKCECookieName(), auth.NonceCookieName()} {
		c.SetCookie(name, "", -1, "/api/auth/oauth/", "", false, true)
	}
}

func buildOAuthIdentityResponses(identities []*models.UserIdentity) []oauthIdentityResponse {
	result := make([]oauthIdentityResponse, 0, len(identities))
	for _, identity := range identities {
		if identity == nil {
			continue
		}
		result = append(result, oauthIdentityResponse{
			ID:            identity.ID,
			Provider:      identity.Provider,
			Username:      identity.Username,
			Email:         identity.Email,
			EmailVerified: identity.EmailVerified,
			AvatarURL:     identity.AvatarURL,
			CreatedAt:     identity.CreatedAt,
		})
	}
	return result
}
