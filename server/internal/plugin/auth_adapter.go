package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
)

// PluginAuthProvider implements auth.OAuthProvider by proxying to an auth_provider plugin.
type PluginAuthProvider struct {
	entry Entry
}

// NewAuthProvider wraps an auth_provider Entry as an auth.OAuthProvider.
func NewAuthProvider(e Entry) auth.OAuthProvider {
	return &PluginAuthProvider{entry: e}
}

func (p *PluginAuthProvider) BuildAuthURL(state, redirectURI string) string {
	q := url.Values{"state": {state}, "redirect_uri": {redirectURI}}
	resp, err := http.Get(p.entry.BaseURL + "/capabilities/auth/authorize-url?" + q.Encode()) //nolint:noctx
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var out struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ""
	}
	return out.URL
}

func (p *PluginAuthProvider) ExchangeCode(ctx context.Context, code, redirectURI string) (string, error) {
	body := fmt.Sprintf(`{"code":%q,"redirect_uri":%q}`, code, redirectURI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.entry.BaseURL+"/capabilities/auth/exchange", strings.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Token, nil
}

func (p *PluginAuthProvider) GetUser(ctx context.Context, accessToken string) (*auth.OAuthUserProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		p.entry.BaseURL+"/capabilities/auth/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var profile auth.OAuthUserProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, err
	}
	return &profile, nil
}
