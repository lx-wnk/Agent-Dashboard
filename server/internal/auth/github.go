package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const (
	defaultGitHubTokenURL = "https://github.com/login/oauth/access_token"
	defaultGitHubUserURL  = "https://api.github.com/user"
	defaultGitHubAuthURL  = "https://github.com/login/oauth/authorize"
)

// GitHubUserProfile holds the fields we need from the GitHub user API.
type GitHubUserProfile struct {
	ID          string // numeric GitHub user ID, as string
	Login       string
	DisplayName string
	AvatarURL   string
}

// GitHubClient exchanges OAuth codes and fetches GitHub user profiles.
type GitHubClient struct {
	clientID     string
	clientSecret string
	tokenURL     string
	userURL      string
	authURL      string
	httpClient   *http.Client
}

type githubOption func(*GitHubClient)

// WithUserAPIURL overrides the GitHub user API URL (for testing).
func WithUserAPIURL(u string) githubOption {
	return func(c *GitHubClient) { c.userURL = u }
}

// WithTokenURL overrides the GitHub token exchange URL (for testing).
func WithTokenURL(u string) githubOption {
	return func(c *GitHubClient) { c.tokenURL = u }
}

// NewGitHubClient creates a GitHubClient for the given OAuth app credentials.
func NewGitHubClient(clientID, clientSecret string, opts ...githubOption) *GitHubClient {
	c := &GitHubClient{
		clientID:     clientID,
		clientSecret: clientSecret,
		tokenURL:     defaultGitHubTokenURL,
		userURL:      defaultGitHubUserURL,
		authURL:      defaultGitHubAuthURL,
		httpClient:   &http.Client{},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// BuildAuthURL returns the GitHub authorization URL for the OAuth flow.
func (c *GitHubClient) BuildAuthURL(state, redirectURI string) string {
	v := url.Values{}
	v.Set("client_id", c.clientID)
	v.Set("state", state)
	v.Set("redirect_uri", redirectURI)
	v.Set("scope", "read:user")
	return c.authURL + "?" + v.Encode()
}

// ExchangeCode exchanges an OAuth authorization code for an access token.
func (c *GitHubClient) ExchangeCode(ctx context.Context, code, redirectURI string) (string, error) {
	v := url.Values{}
	v.Set("client_id", c.clientID)
	v.Set("client_secret", c.clientSecret)
	v.Set("code", code)
	v.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL, strings.NewReader(v.Encode()))
	if err != nil {
		return "", fmt.Errorf("github.ExchangeCode: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("github.ExchangeCode: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("github.ExchangeCode: decode: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("github.ExchangeCode: %s", result.Error)
	}
	return result.AccessToken, nil
}

// GetUser fetches the GitHub user profile for the given access token.
func (c *GitHubClient) GetUser(ctx context.Context, accessToken string) (*GitHubUserProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.userURL, nil)
	if err != nil {
		return nil, fmt.Errorf("github.GetUser: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github.GetUser: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github.GetUser: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github.GetUser: HTTP %d: %s", resp.StatusCode, body)
	}

	var raw struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("github.GetUser: decode: %w", err)
	}
	return &GitHubUserProfile{
		ID:          strconv.FormatInt(raw.ID, 10),
		Login:       raw.Login,
		DisplayName: raw.Name,
		AvatarURL:   raw.AvatarURL,
	}, nil
}
