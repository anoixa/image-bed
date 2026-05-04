package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

const (
	googleAuthURL    = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL   = "https://oauth2.googleapis.com/token"
	googleUserInfoEP = "https://www.googleapis.com/oauth2/v2/userinfo"
)

// GoogleProvider implements OAuthProvider for Google using OIDC.
type GoogleProvider struct {
	config *oauth2.Config
}

// NewGoogleProvider creates a Google OIDC provider.
func NewGoogleProvider(clientID, clientSecret, redirectURL string) *GoogleProvider {
	return &GoogleProvider{
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  googleAuthURL,
				TokenURL: googleTokenURL,
			},
		},
	}
}

func (p *GoogleProvider) Name() string        { return "google" }
func (p *GoogleProvider) DisplayName() string { return "Google" }
func (p *GoogleProvider) Icon() string        { return "google" }

func (p *GoogleProvider) AuthCodeURL(state, challenge string) string {
	return p.config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

func (p *GoogleProvider) Exchange(ctx context.Context, code, verifier string) (*OAuthToken, error) {
	token, err := p.config.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", verifier),
	)
	if err != nil {
		return nil, fmt.Errorf("google token exchange failed: %w", err)
	}

	idToken, _ := token.Extra("id_token").(string)
	return &OAuthToken{
		AccessToken: token.AccessToken,
		TokenType:   token.TokenType,
		IDToken:     idToken,
	}, nil
}

func (p *GoogleProvider) FetchIdentity(ctx context.Context, token *OAuthToken, nonce string) (*ExternalIdentity, error) {
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: token.AccessToken,
		TokenType:   token.TokenType,
	}))

	user, err := fetchGoogleUser(client)
	if err != nil {
		return nil, err
	}

	identity := &ExternalIdentity{
		Subject:       user.Sub,
		Username:      user.Name,
		Email:         user.Email,
		EmailVerified: user.VerifiedEmail,
		AvatarURL:     user.Picture,
	}

	// Validate nonce from id_token if available
	if token.IDToken != "" && nonce != "" {
		if err := validateGoogleIDTokenNonce(token.IDToken, nonce); err != nil {
			return nil, fmt.Errorf("id_token nonce validation failed: %w", err)
		}
	}

	return identity, nil
}

type googleUser struct {
	Sub           string `json:"id"`
	Email         string `json:"email"`
	VerifiedEmail bool   `json:"verified_email"`
	Name          string `json:"name"`
	Picture       string `json:"picture"`
}

func fetchGoogleUser(client *http.Client) (*googleUser, error) {
	resp, err := client.Get(googleUserInfoEP)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch google user info: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google userinfo API returned %d", resp.StatusCode)
	}

	var user googleUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode google user info: %w", err)
	}
	return &user, nil
}

// validateGoogleIDTokenNonce extracts the nonce claim from the id_token JWT
// payload without verifying the signature.
//
// TODO: This is intentionally minimal for a self-hosted, single-user image bed.
// A production multi-tenant system MUST use a full OIDC verifier that validates:
//   - JWT signature via Google's JWKS endpoints
//   - issuer ("accounts.google.com" or "https://accounts.google.com")
//   - audience (must match our client_id)
//   - expiry
//   - hosted-domain constraint (if configured)
//
// For this project, nonce-checking prevents replay while the HMAC-signed state
// cookie binds the flow to the originating browser session.
func validateGoogleIDTokenNonce(idToken, expectedNonce string) error {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid id_token format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("failed to decode id_token payload: %w", err)
	}

	var claims struct {
		Nonce string `json:"nonce"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return fmt.Errorf("failed to parse id_token claims: %w", err)
	}

	if claims.Nonce != expectedNonce {
		return fmt.Errorf("nonce mismatch: expected %q, got %q", expectedNonce, claims.Nonce)
	}

	return nil
}
