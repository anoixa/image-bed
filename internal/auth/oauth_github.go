package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"golang.org/x/oauth2"
)

const (
	githubAuthURL  = "https://github.com/login/oauth/authorize"
	githubTokenURL = "https://github.com/login/oauth/access_token"
	githubUserURL  = "https://api.github.com/user"
	githubEmailURL = "https://api.github.com/user/emails"
)

// GitHubProvider implements OAuthProvider for GitHub.
type GitHubProvider struct {
	config *oauth2.Config
}

// NewGitHubProvider creates a GitHub OAuth provider.
func NewGitHubProvider(clientID, clientSecret, redirectURL string) *GitHubProvider {
	return &GitHubProvider{
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"read:user", "user:email"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  githubAuthURL,
				TokenURL: githubTokenURL,
			},
		},
	}
}

func (p *GitHubProvider) Name() string        { return "github" }
func (p *GitHubProvider) DisplayName() string { return "GitHub" }
func (p *GitHubProvider) Icon() string        { return "github" }

func (p *GitHubProvider) AuthCodeURL(state, challenge string) string {
	return p.config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
}

func (p *GitHubProvider) Exchange(ctx context.Context, code, verifier string) (*OAuthToken, error) {
	token, err := p.config.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", verifier),
	)
	if err != nil {
		return nil, fmt.Errorf("github token exchange failed: %w", err)
	}
	return &OAuthToken{
		AccessToken: token.AccessToken,
		TokenType:   token.TokenType,
	}, nil
}

func (p *GitHubProvider) FetchIdentity(ctx context.Context, token *OAuthToken, _ string) (*ExternalIdentity, error) {
	client := oauth2.NewClient(ctx, oauth2.StaticTokenSource(&oauth2.Token{
		AccessToken: token.AccessToken,
		TokenType:   token.TokenType,
	}))

	// Fetch user profile
	user, err := fetchGitHubUser(client)
	if err != nil {
		return nil, err
	}

	identity := &ExternalIdentity{
		Subject:  strconv.FormatInt(user.ID, 10),
		Username: user.Login,
	}

	// Fetch avatar
	if user.AvatarURL != "" {
		identity.AvatarURL = user.AvatarURL
	}

	// Fetch emails to find primary verified email
	emails, err := fetchGitHubEmails(client)
	if err != nil {
		// Non-fatal: continue without email
		return identity, nil
	}

	for _, e := range emails {
		if e.Primary && e.Verified {
			identity.Email = e.Email
			identity.EmailVerified = true
			break
		}
	}

	return identity, nil
}

type githubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
	Name      string `json:"name"`
	Email     string `json:"email"`
}

type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

func fetchGitHubUser(client *http.Client) (*githubUser, error) {
	resp, err := client.Get(githubUserURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch github user: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github user API returned %d: %s", resp.StatusCode, string(body))
	}

	var user githubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("failed to decode github user: %w", err)
	}
	return &user, nil
}

func fetchGitHubEmails(client *http.Client) ([]githubEmail, error) {
	resp, err := client.Get(githubEmailURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch github emails: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github emails API returned %d", resp.StatusCode)
	}

	var emails []githubEmail
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return nil, fmt.Errorf("failed to decode github emails: %w", err)
	}
	return emails, nil
}
