package githubauth_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	githubauth "github.com/lx-wnk/agent-dashboard/server/internal/auth/github"
)

func TestClient_GetUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":         12345,
			"login":      "octocat",
			"name":       "The Octocat",
			"avatar_url": "https://example.com/avatar.png",
		})
	}))
	defer srv.Close()

	client := githubauth.NewClient("id", "secret", githubauth.WithUserAPIURL(srv.URL))
	user, err := client.GetUser(t.Context(), "test-token")
	require.NoError(t, err)
	require.Equal(t, "12345", user.ID) // numeric GitHub ID converted to string
	require.Equal(t, "octocat", user.Login)
	require.Equal(t, "The Octocat", user.DisplayName)
}

func TestClient_BuildAuthURL(t *testing.T) {
	client := githubauth.NewClient("my-client-id", "secret")
	url := client.BuildAuthURL("my-state", "http://callback")
	require.Contains(t, url, "client_id=my-client-id")
	require.Contains(t, url, "state=my-state")
	require.Contains(t, url, "redirect_uri=")
}

func TestClient_ExchangeCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "gho_test123",
		})
	}))
	defer srv.Close()

	client := githubauth.NewClient("id", "secret", githubauth.WithTokenURL(srv.URL))
	token, err := client.ExchangeCode(t.Context(), "code123", "http://callback")
	require.NoError(t, err)
	require.Equal(t, "gho_test123", token)
}
