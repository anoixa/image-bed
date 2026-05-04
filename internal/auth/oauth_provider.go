package auth

import "context"

// ExternalIdentity represents a normalized identity from an OAuth provider.
type ExternalIdentity struct {
	Subject       string // Provider's stable user ID
	Username      string // Display username (may change)
	Email         string // Email from provider
	EmailVerified bool   // Whether the provider confirmed the email
	AvatarURL     string // Profile picture URL
}

// OAuthToken holds the tokens returned by the OAuth exchange.
type OAuthToken struct {
	AccessToken string
	TokenType   string
	IDToken     string // For OIDC providers
}

// OAuthProvider is the interface each provider adapter must implement.
type OAuthProvider interface {
	// Name returns the provider identifier (e.g. "github", "google").
	Name() string
	// DisplayName returns a human-readable name for UI rendering.
	DisplayName() string
	// Icon returns the icon identifier for frontend rendering.
	Icon() string
	// AuthCodeURL builds the authorization redirect URL with the given state and PKCE challenge.
	AuthCodeURL(state, challenge string) string
	// Exchange trades an authorization code for tokens using the PKCE verifier.
	Exchange(ctx context.Context, code, verifier string) (*OAuthToken, error)
	// FetchIdentity calls the provider's user-info endpoint and returns a normalized identity.
	// The nonce parameter is used by OIDC providers for id_token validation.
	FetchIdentity(ctx context.Context, token *OAuthToken, nonce string) (*ExternalIdentity, error)
}

// ProviderInfo is a lightweight provider description for API responses.
type ProviderInfo struct {
	Provider    string `json:"provider"`
	DisplayName string `json:"display_name"`
	Icon        string `json:"icon"`
	Enabled     bool   `json:"enabled"`
}
