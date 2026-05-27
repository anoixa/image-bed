package auth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redirectTransport routes all requests to the test server regardless of URL.
type redirectTransport struct {
	server *httptest.Server
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Preserve the path but redirect to the test server
	req.URL.Scheme = "http"
	req.URL.Host = t.server.Listener.Addr().String()
	return http.DefaultTransport.RoundTrip(req)
}

func testClient(server *httptest.Server) *http.Client {
	return &http.Client{Transport: &redirectTransport{server: server}}
}

func TestGitHubProvider(t *testing.T) {
	provider := NewGitHubProvider("test-client-id", "test-secret", "http://localhost/callback")
	assert.Equal(t, "github", provider.Name())
	assert.Equal(t, "GitHub", provider.DisplayName())
	assert.Equal(t, "github", provider.Icon())

	url := provider.AuthCodeURL("test-state", "test-challenge")
	assert.Contains(t, url, "github.com/login/oauth/authorize")
	assert.Contains(t, url, "test-state")
	assert.Contains(t, url, "code_challenge=test-challenge")
}

func TestGitHubProviderFetchUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			if err := json.NewEncoder(w).Encode(map[string]any{
				"id":         12345,
				"login":      "testuser",
				"avatar_url": "https://avatars.githubusercontent.com/u/12345",
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	user, err := fetchGitHubUser(testClient(server))
	require.NoError(t, err)
	assert.Equal(t, int64(12345), user.ID)
	assert.Equal(t, "testuser", user.Login)
	assert.Equal(t, "https://avatars.githubusercontent.com/u/12345", user.AvatarURL)
}

func TestGitHubProviderFetchEmails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode([]map[string]any{
			{"email": "primary@example.com", "primary": true, "verified": true},
			{"email": "other@example.com", "primary": false, "verified": false},
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	emails, err := fetchGitHubEmails(testClient(server))
	require.NoError(t, err)
	require.Len(t, emails, 2)
	assert.Equal(t, "primary@example.com", emails[0].Email)
	assert.True(t, emails[0].Primary)
	assert.True(t, emails[0].Verified)
}

func TestGoogleProvider(t *testing.T) {
	provider := NewGoogleProvider("test-client-id", "test-secret", "http://localhost/callback")
	assert.Equal(t, "google", provider.Name())
	assert.Equal(t, "Google", provider.DisplayName())
	assert.Equal(t, "google", provider.Icon())

	url := provider.AuthCodeURL("test-state", "test-challenge")
	assert.Contains(t, url, "accounts.google.com")
	assert.Contains(t, url, "code_challenge=test-challenge")
}

func TestGoogleProviderFetchUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]any{
			"id":             "google-sub-123",
			"email":          "user@gmail.com",
			"verified_email": true,
			"name":           "Test User",
			"picture":        "https://lh3.googleusercontent.com/test",
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	user, err := fetchGoogleUser(testClient(server))
	require.NoError(t, err)
	assert.Equal(t, "google-sub-123", user.Sub)
	assert.Equal(t, "user@gmail.com", user.Email)
	assert.True(t, user.VerifiedEmail)
	assert.Equal(t, "Test User", user.Name)
	assert.Equal(t, "https://lh3.googleusercontent.com/test", user.Picture)
}

func TestGiteeProvider(t *testing.T) {
	provider := NewGiteeProvider("test-client-id", "test-secret", "http://localhost/callback")
	assert.Equal(t, "gitee", provider.Name())
	assert.Equal(t, "Gitee", provider.DisplayName())
	assert.Equal(t, "gitee", provider.Icon())

	url := provider.AuthCodeURL("test-state", "test-challenge")
	assert.Contains(t, url, "gitee.com/oauth/authorize")
	assert.Contains(t, url, "code_challenge=test-challenge")
}

func TestGiteeProviderFetchUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]any{
			"id":         67890,
			"login":      "giteeuser",
			"avatar_url": "https://gitee.com/assets/avatar.png",
			"email":      "user@gitee.com",
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	user, err := fetchGiteeUser(testClient(server))
	require.NoError(t, err)
	assert.Equal(t, int64(67890), user.ID)
	assert.Equal(t, "giteeuser", user.Login)
	assert.Equal(t, "user@gitee.com", user.Email)
	assert.Equal(t, "https://gitee.com/assets/avatar.png", user.AvatarURL)
}

func TestValidateGoogleIDTokenNonce(t *testing.T) {
	t.Run("valid nonce", func(t *testing.T) {
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
		payload := base64.RawURLEncoding.EncodeToString([]byte(`{"nonce":"test-nonce"}`))
		sig := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
		token := header + "." + payload + "." + sig

		err := validateGoogleIDTokenNonce(token, "test-nonce")
		assert.NoError(t, err)
	})

	t.Run("invalid nonce", func(t *testing.T) {
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`))
		payload := base64.RawURLEncoding.EncodeToString([]byte(`{"nonce":"wrong-nonce"}`))
		sig := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
		token := header + "." + payload + "." + sig

		err := validateGoogleIDTokenNonce(token, "expected-nonce")
		assert.Error(t, err)
	})
}
