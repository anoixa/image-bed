package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/anoixa/image-bed/config"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/accounts"
)

var (
	ErrProviderNotEnabled    = errors.New("oauth provider not enabled")
	ErrIdentityNotLinked     = errors.New("no linked identity found for this provider")
	ErrIdentityAlreadyBound  = errors.New("identity already bound to another user")
	ErrInvalidState          = errors.New("invalid or expired OAuth state")
	ErrInvalidReturnTo       = errors.New("invalid return_to path")
	ErrLastLoginMethod       = errors.New("cannot unlink the last login method")
	ErrPasswordLoginDisabled = errors.New("password login is disabled")
)

// OAuthService orchestrates OAuth login, link, and unlink flows.
type OAuthService struct {
	stateManager         *StateManager
	loginService         *LoginService
	identityRepo         *accounts.IdentityRepository
	accountsRepo         *accounts.Repository
	passwordLoginEnabled bool
	providers            map[string]OAuthProvider
	providersMu          sync.RWMutex
}

// NewOAuthService creates a new OAuthService.
func NewOAuthService(
	jwtSecret []byte,
	loginService *LoginService,
	identityRepo *accounts.IdentityRepository,
	accountsRepo *accounts.Repository,
	passwordLoginEnabled bool,
) *OAuthService {
	return &OAuthService{
		stateManager:         NewStateManager(jwtSecret),
		loginService:         loginService,
		identityRepo:         identityRepo,
		accountsRepo:         accountsRepo,
		passwordLoginEnabled: passwordLoginEnabled,
		providers:            make(map[string]OAuthProvider),
	}
}

// RegisterProvider registers an OAuth provider adapter.
func (s *OAuthService) RegisterProvider(provider OAuthProvider) {
	s.providersMu.Lock()
	defer s.providersMu.Unlock()
	s.providers[provider.Name()] = provider
}

// ReplaceProviders atomically replaces the enabled OAuth provider set.
func (s *OAuthService) ReplaceProviders(providers []OAuthProvider) {
	next := make(map[string]OAuthProvider, len(providers))
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		next[provider.Name()] = provider
	}

	s.providersMu.Lock()
	defer s.providersMu.Unlock()
	s.providers = next
}

// GetProvider returns a registered provider by name.
func (s *OAuthService) GetProvider(name string) (OAuthProvider, bool) {
	s.providersMu.RLock()
	defer s.providersMu.RUnlock()
	p, ok := s.providers[name]
	return p, ok
}

// ListProviders returns info about all registered providers.
func (s *OAuthService) ListProviders() []ProviderInfo {
	s.providersMu.RLock()
	defer s.providersMu.RUnlock()

	result := make([]ProviderInfo, 0, len(s.providers))
	for _, p := range s.providers {
		result = append(result, ProviderInfo{
			Provider:    p.Name(),
			DisplayName: p.DisplayName(),
			Icon:        p.Icon(),
			Enabled:     true,
		})
	}
	return result
}

// StartLogin builds the authorization redirect URL for a login flow.
// Returns auth URL, state cookie, PKCE cookie, nonce cookie.
func (s *OAuthService) StartLogin(providerName, returnTo string) (string, []*http.Cookie, error) {
	provider, ok := s.GetProvider(providerName)
	if !ok {
		return "", nil, ErrProviderNotEnabled
	}

	if !ValidateReturnTo(returnTo) {
		return "", nil, ErrInvalidReturnTo
	}

	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate PKCE: %w", err)
	}

	nonce, err := GenerateNonce()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	state := NewOAuthState(StateModeLogin, providerName, returnTo)

	signedState, err := s.stateManager.SignState(state)
	if err != nil {
		return "", nil, fmt.Errorf("failed to sign state: %w", err)
	}

	authURL := provider.AuthCodeURL(signedState, challenge)

	cookies := stateCookies(signedState, verifier, nonce)
	return authURL, cookies, nil
}

// StartLink builds the authorization redirect URL for linking an identity.
// Returns auth URL, state cookie, PKCE cookie, nonce cookie.
func (s *OAuthService) StartLink(providerName, returnTo string, userID uint) (string, []*http.Cookie, error) {
	provider, ok := s.GetProvider(providerName)
	if !ok {
		return "", nil, ErrProviderNotEnabled
	}

	if !ValidateReturnTo(returnTo) {
		return "", nil, ErrInvalidReturnTo
	}

	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate PKCE: %w", err)
	}

	nonce, err := GenerateNonce()
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	state := NewOAuthState(StateModeLink, providerName, returnTo)
	state.UserID = userID

	signedState, err := s.stateManager.SignState(state)
	if err != nil {
		return "", nil, fmt.Errorf("failed to sign state: %w", err)
	}

	authURL := provider.AuthCodeURL(signedState, challenge)

	cookies := stateCookies(signedState, verifier, nonce)
	return authURL, cookies, nil
}

// stateCookies builds the state, PKCE, and nonce cookies for an OAuth flow.
func stateCookies(signedState, verifier, nonce string) []*http.Cookie {
	cookieDefaults := http.Cookie{
		MaxAge:   int(stateMaxAge.Seconds()),
		Path:     "/api/auth/oauth/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   config.IsProduction(),
	}
	return []*http.Cookie{
		{
			Name:     StateCookieName(),
			Value:    signedState,
			MaxAge:   cookieDefaults.MaxAge,
			Path:     cookieDefaults.Path,
			HttpOnly: cookieDefaults.HttpOnly,
			SameSite: cookieDefaults.SameSite,
			Secure:   cookieDefaults.Secure,
		},
		{
			Name:     PKCECookieName(),
			Value:    verifier,
			MaxAge:   cookieDefaults.MaxAge,
			Path:     cookieDefaults.Path,
			HttpOnly: cookieDefaults.HttpOnly,
			SameSite: cookieDefaults.SameSite,
			Secure:   cookieDefaults.Secure,
		},
		{
			Name:     NonceCookieName(),
			Value:    nonce,
			MaxAge:   cookieDefaults.MaxAge,
			Path:     cookieDefaults.Path,
			HttpOnly: cookieDefaults.HttpOnly,
			SameSite: cookieDefaults.SameSite,
			Secure:   cookieDefaults.Secure,
		},
	}
}

// HandleCallback processes the OAuth callback for both login and link flows.
// It verifies the query state matches the browser's state cookie, then
// uses the PKCE verifier and nonce from their respective cookies.
// Returns: LoginResult (nil for link mode), returnTo path, state mode, error.
func (s *OAuthService) HandleCallback(ctx context.Context, providerName, code, rawState, cookieState, verifier, nonce string) (*LoginResult, string, string, error) {
	provider, ok := s.GetProvider(providerName)
	if !ok {
		return nil, "", "", ErrProviderNotEnabled
	}

	// Verify the query state matches the cookie state (CSRF protection)
	if cookieState == "" || rawState != cookieState {
		return nil, "", "", ErrInvalidState
	}

	state, err := s.stateManager.VerifyState(rawState)
	if err != nil {
		return nil, "", "", ErrInvalidState
	}

	if state.Provider != providerName {
		return nil, "", state.Mode, ErrInvalidState
	}

	if state.Mode != StateModeLogin && state.Mode != StateModeLink {
		return nil, "", state.Mode, ErrInvalidState
	}

	if verifier == "" {
		return nil, "", state.Mode, ErrInvalidState
	}

	token, err := provider.Exchange(ctx, code, verifier)
	if err != nil {
		return nil, "", state.Mode, fmt.Errorf("failed to exchange code: %w", err)
	}

	identity, err := provider.FetchIdentity(ctx, token, nonce)
	if err != nil {
		return nil, "", state.Mode, fmt.Errorf("failed to fetch identity: %w", err)
	}

	switch state.Mode {
	case StateModeLogin:
		result, returnTo, err := s.handleLogin(ctx, providerName, identity, state.ReturnTo)
		return result, returnTo, state.Mode, err
	case StateModeLink:
		_, returnTo, err := s.handleLink(ctx, providerName, identity, state.UserID, state.ReturnTo)
		return nil, returnTo, state.Mode, err
	default:
		return nil, "", state.Mode, ErrInvalidState
	}
}

// handleLogin resolves an external identity to an internal user and issues a session.
func (s *OAuthService) handleLogin(ctx context.Context, providerName string, extID *ExternalIdentity, returnTo string) (*LoginResult, string, error) {
	// 1. Check existing identity binding
	existing, err := s.identityRepo.FindByProviderSubject(ctx, providerName, extID.Subject)
	if err != nil && !errors.Is(err, accounts.ErrIdentityNotFound) {
		return nil, "", fmt.Errorf("failed to lookup identity: %w", err)
	}

	var user *models.User

	if existing != nil {
		// Identity already linked to a user
		user, err = s.accountsRepo.GetUserByID(existing.UserID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to get user: %w", err)
		}
	} else {
		return nil, "", ErrIdentityNotLinked
	}

	if !user.IsActive() {
		return nil, "", ErrUserDisabled
	}

	// Update identity profile fields
	if existing != nil {
		changed := false
		if extID.Username != existing.Username {
			existing.Username = extID.Username
			changed = true
		}
		if extID.AvatarURL != existing.AvatarURL {
			existing.AvatarURL = extID.AvatarURL
			changed = true
		}
		if extID.Email != existing.Email || extID.EmailVerified != existing.EmailVerified {
			existing.Email = extID.Email
			existing.EmailVerified = extID.EmailVerified
			changed = true
		}
		if changed {
			s.identityRepo.DB().WithContext(ctx).Save(existing)
		}
	}

	result, err := s.loginService.IssueSessionForUser(user)
	if err != nil {
		return nil, "", fmt.Errorf("failed to issue session: %w", err)
	}

	return result, returnTo, nil
}

// handleLink links an external identity to an authenticated user.
func (s *OAuthService) handleLink(ctx context.Context, providerName string, extID *ExternalIdentity, userID uint, returnTo string) (*LoginResult, string, error) {
	// Check if identity is already bound to another user
	existing, err := s.identityRepo.FindByProviderSubject(ctx, providerName, extID.Subject)
	if err != nil && !errors.Is(err, accounts.ErrIdentityNotFound) {
		return nil, "", fmt.Errorf("failed to lookup identity: %w", err)
	}
	if existing != nil {
		return nil, "", ErrIdentityAlreadyBound
	}

	// Verify user exists and is active
	user, err := s.accountsRepo.GetUserByID(userID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get user: %w", err)
	}
	if !user.IsActive() {
		return nil, "", ErrUserDisabled
	}

	// Create identity binding
	newIdentity := &models.UserIdentity{
		UserID:        userID,
		Provider:      providerName,
		Subject:       extID.Subject,
		Username:      extID.Username,
		Email:         extID.Email,
		EmailVerified: extID.EmailVerified,
		AvatarURL:     extID.AvatarURL,
	}
	if err := s.identityRepo.Create(ctx, newIdentity); err != nil {
		return nil, "", fmt.Errorf("failed to create identity: %w", err)
	}

	return nil, returnTo, nil
}

// GetUserIdentities returns all identities linked to a user.
func (s *OAuthService) GetUserIdentities(ctx context.Context, userID uint) ([]*models.UserIdentity, error) {
	return s.identityRepo.FindByUser(ctx, userID)
}

// UnlinkIdentity removes a provider identity from a user.
// It refuses to unlink the last login method.
func (s *OAuthService) UnlinkIdentity(ctx context.Context, userID uint, providerName string) error {
	identities, err := s.identityRepo.FindByUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("failed to get user identities: %w", err)
	}

	// Check if user has a password set
	user, err := s.accountsRepo.GetUserByID(userID)
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}
	hasPassword := s.passwordLoginEnabled && user.Password != ""

	// If only one identity and no password, refuse
	if len(identities) <= 1 && !hasPassword {
		return ErrLastLoginMethod
	}

	return s.identityRepo.Delete(ctx, userID, providerName)
}

// StateManager returns the state manager for cookie validation.
func (s *OAuthService) StateManager() *StateManager {
	return s.stateManager
}
