package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// StateModeLogin is the default mode for OAuth login flows.
	StateModeLogin = "login"
	// StateModeLink is for linking an OAuth identity to an already-authenticated user.
	StateModeLink = "link"

	stateMaxAge     = 10 * time.Minute
	stateCookieName = "oauth_state"
	pkceCookieName  = "oauth_pkce"
	nonceCookieName = "oauth_nonce"
)

// OAuthState holds the data embedded in the OAuth state parameter.
// PKCE verifier and nonce are stored in separate cookies, not in this
// front-channel token, to prevent exposure via the authorization URL.
type OAuthState struct {
	Mode     string `json:"m"` // "login" or "link"
	Provider string `json:"p"`
	ReturnTo string `json:"r"`
	UserID   uint   `json:"u,omitempty"` // Set in link mode
	ExpireAt int64  `json:"e"`
}

// StateCookieName returns the cookie name used for the OAuth state.
func StateCookieName() string {
	return stateCookieName
}

// PKCECookieName returns the cookie name used for the PKCE verifier.
func PKCECookieName() string {
	return pkceCookieName
}

// NonceCookieName returns the cookie name used for the OIDC nonce.
func NonceCookieName() string {
	return nonceCookieName
}

// NewOAuthState creates a new OAuthState with default values.
func NewOAuthState(mode, provider, returnTo string) *OAuthState {
	return &OAuthState{
		Mode:     mode,
		Provider: provider,
		ReturnTo: returnTo,
		ExpireAt: time.Now().Add(stateMaxAge).Unix(),
	}
}

// GeneratePKCE creates a PKCE code verifier and challenge (S256).
func GeneratePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", fmt.Errorf("failed to generate PKCE verifier: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge, nil
}

// GenerateNonce creates a random nonce string.
func GenerateNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// StateManager handles signing and verification of OAuth state tokens.
type StateManager struct {
	signingKey []byte
}

// NewStateManager creates a StateManager using the given secret for HMAC signing.
func NewStateManager(secret []byte) *StateManager {
	// Derive a distinct key from the secret using HKDF-like approach
	h := hmac.New(sha256.New, secret)
	h.Write([]byte("oauth-state-key"))
	key := h.Sum(nil)
	return &StateManager{signingKey: key}
}

// SignState encodes the state as JSON, computes an HMAC signature, and returns
// a single base64 string suitable for use as a cookie value.
func (sm *StateManager) SignState(state *OAuthState) (string, error) {
	payload, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("failed to marshal state: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, sm.signingKey)
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, nil
}

// VerifyState parses and verifies a signed state string. It checks the HMAC
// signature and expiry.
func (sm *StateManager) VerifyState(signed string) (*OAuthState, error) {
	parts := strings.SplitN(signed, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid state format")
	}

	encoded, sig := parts[0], parts[1]

	// Verify HMAC
	mac := hmac.New(sha256.New, sm.signingKey)
	mac.Write([]byte(encoded))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expectedSig)) {
		return nil, errors.New("invalid state signature")
	}

	// Decode payload
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode state: %w", err)
	}

	var state OAuthState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Check expiry
	if time.Now().Unix() > state.ExpireAt {
		return nil, errors.New("state expired")
	}

	return &state, nil
}

// ValidateReturnTo checks that the return_to path is a same-site relative path.
func ValidateReturnTo(returnTo string) bool {
	if returnTo == "" {
		return true
	}
	// Must start with / and not be a full URL or // path
	if !strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//") {
		return false
	}
	// Must not contain URL scheme indicators
	if strings.Contains(returnTo, "://") {
		return false
	}
	return true
}
