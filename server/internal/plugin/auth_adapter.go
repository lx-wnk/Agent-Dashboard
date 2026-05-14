package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
)

// PluginAuthProvider implements auth.OAuthProvider by proxying to an auth_provider plugin.
type PluginAuthProvider struct {
	entry  Entry
	client *http.Client
}

// NewAuthProvider wraps an auth_provider Entry as an auth.OAuthProvider.
func NewAuthProvider(e Entry) auth.OAuthProvider {
	return &PluginAuthProvider{
		entry:  e,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (p *PluginAuthProvider) BuildAuthURL(ctx context.Context, state, redirectURI string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	u := p.entry.BaseURL + "/capabilities/auth/authorize-url" +
		"?state=" + url.QueryEscape(state) + "&redirect_uri=" + url.QueryEscape(redirectURI)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		slog.Error("plugin BuildAuthURL: create request", "err", err)
		return ""
	}
	resp, err := p.client.Do(req)
	if err != nil {
		slog.Error("plugin BuildAuthURL: request failed", "err", err)
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Error("plugin BuildAuthURL: non-200", "status", resp.StatusCode)
		return ""
	}
	var result struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("plugin BuildAuthURL: decode failed", "err", err)
		return ""
	}
	return result.URL
}

func (p *PluginAuthProvider) ExchangeCode(ctx context.Context, code, redirectURI string) (string, error) {
	type exchangeReq struct {
		Code        string `json:"code"`
		RedirectURI string `json:"redirect_uri"`
	}
	body, err := json.Marshal(exchangeReq{Code: code, RedirectURI: redirectURI})
	if err != nil {
		return "", fmt.Errorf("plugin ExchangeCode: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.entry.BaseURL+"/capabilities/auth/exchange", strings.NewReader(string(body)))
	if err != nil {
		return "", fmt.Errorf("plugin ExchangeCode: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("plugin ExchangeCode: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("plugin ExchangeCode: non-200 status %d", resp.StatusCode)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("plugin ExchangeCode: decode: %w", err)
	}
	return out.Token, nil
}

func (p *PluginAuthProvider) GetUser(ctx context.Context, accessToken string) (*auth.OAuthUserProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		p.entry.BaseURL+"/capabilities/auth/user", nil)
	if err != nil {
		return nil, fmt.Errorf("plugin GetUser: create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("plugin GetUser: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("plugin GetUser: non-200 status %d", resp.StatusCode)
	}
	var profile auth.OAuthUserProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, fmt.Errorf("plugin GetUser: decode: %w", err)
	}
	return &profile, nil
}
