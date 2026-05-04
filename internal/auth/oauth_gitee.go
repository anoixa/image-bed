package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
)

const (
	giteeAuthURL  = "https://gitee.com/oauth/authorize"
	giteeTokenURL = "https://gitee.com/oauth/token"
	giteeUserURL  = "https://gitee.com/api/v5/user"
)

// GiteeProvider implements OAuthProvider for Gitee.
type GiteeProvider struct {
	config *oauth2.Config
}

// NewGiteeProvider creates a Gitee OAuth provider.
func NewGiteeProvider(clientID, clientSecret, redirectURL string) *GiteeProvider {
	return &GiteeProvider{
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"user_info", "emails"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  giteeAuthURL,
				TokenURL: giteeTokenURL,
			},
		},
	}
}

func (p *GiteeProvider) Name() string        { return "gitee" }
func (p *GiteeProvider) DisplayName() string { return "Gitee" }
func (p *GiteeProvider) Icon() string        { return "gitee" }

func (p *GiteeProvider) AuthCodeURL(state, challenge string) string {
	return p.config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

func (p *GiteeProvider) Exchange(ctx context.Context, code, verifier string) (*OAuthToken, error) {
	token, err := p.config.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", verifier),
	)
	if err != nil {
		return nil, fmt.Errorf("gitee token exchange failed: %w", err)
	}
	return &OAuthToken{
		AccessToken: token.AccessToken,
		TokenType:   token.TokenType,
	}, nil
}

func (p *GiteeProvider) FetchIdentity(ctx context.Context, token *OAuthToken, _ string) (*ExternalIdentity, error) {
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: token.AccessToken,
		TokenType:   token.TokenType,
	}))

	user, err := fetchGiteeUser(client)
	if err != nil {
		return nil, err
	}

	identity := &ExternalIdentity{
		Subject:   fmt.Sprintf("%d", user.ID),
		Username:  user.Login,
		Email:     user.Email,
		AvatarURL: user.AvatarURL,
	}

	// Gitee does not reliably indicate email verification status via the user API.
	// Set EmailVerified to false; email-based invites will require manual admin verification.
	identity.EmailVerified = false

	return identity, nil
}

type giteeUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
	Email     string `json:"email"`
}

func fetchGiteeUser(client *http.Client) (*giteeUser, error) {
	resp, err := client.Get(giteeUserURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch gitee user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gitee user API returned %d: %s", resp.StatusCode, string(body))
	}

	var user giteeUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode gitee user: %w", err)
	}
	return &user, nil
}
