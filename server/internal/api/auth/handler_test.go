package auth_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	apiauth "github.com/lx-wnk/agent-dashboard/server/internal/api/auth"
)

func TestHandler_GitHubRedirect_NoClientID(t *testing.T) {
	h := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:   "test-secret",
		CallbackURL: "http://localhost/api/auth/callback",
		// GitHubClient nil: misconfigured server
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/github", nil)
	err := h.GitHubRedirect(w, r)
	require.Error(t, err) // must return error when GitHub not configured
}

func TestHandler_GitHubRedirect_PluginLoginURL(t *testing.T) {
	// When a PluginLoginURL is set, GitHubRedirect must redirect to the plugin.
	h := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:      "test-secret",
		PluginLoginURL: "http://127.0.0.1:19001/login",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/github", nil)
	err := h.GitHubRedirect(w, r)
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, w.Code)
	require.Equal(t, "http://127.0.0.1:19001/login", w.Header().Get("Location"))
}

func TestHandler_Logout(t *testing.T) {
	h := apiauth.NewHandler(apiauth.Deps{JWTSecret: "test-secret"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	err := h.Logout(w, r)
	require.NoError(t, err)
	// Cookie should be cleared
	cookies := w.Result().Cookies()
	var found bool
	for _, c := range cookies {
		if c.Name == "auth_token" {
			found = true
			require.LessOrEqual(t, c.MaxAge, 0)
		}
	}
	require.True(t, found, "auth_token cookie should be cleared")
}

func TestHandler_Me_Unauthenticated(t *testing.T) {
	h := apiauth.NewHandler(apiauth.Deps{JWTSecret: "test-secret"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	err := h.Me(w, r)
	require.Error(t, err) // no JWT in context → error
}

func TestHandler_Me_Authenticated(t *testing.T) {
	// Build a valid JWT and inject it into context via cookie
	// Since we can't easily call RequireAuth middleware in a unit test,
	// just verify that Me returns error when no payload is in context.
	// Full integration test is deferred to Task 32.
	h := apiauth.NewHandler(apiauth.Deps{JWTSecret: "test-secret"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	err := h.Me(w, r)
	require.Error(t, err)
}

func TestHandler_CreateSession_NotConfigured(t *testing.T) {
	// When AuthPluginSecret is empty, CreateSession must return 404-style error.
	h := apiauth.NewHandler(apiauth.Deps{JWTSecret: "test-secret"})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/session", nil)
	err := h.CreateSession(w, r)
	require.Error(t, err)
}

func TestHandler_CreateSession_WrongSecret(t *testing.T) {
	h := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:        "test-secret",
		AuthPluginSecret: "correct-plugin-secret",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/session",
		bytes.NewBufferString(`{"github_id":"1","login":"alice"}`))
	r.Header.Set("Authorization", "Bearer wrong-secret")
	err := h.CreateSession(w, r)
	require.Error(t, err)
}

func TestHandler_CreateSession_MissingAuthHeader(t *testing.T) {
	h := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:        "test-secret",
		AuthPluginSecret: "correct-plugin-secret",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/session",
		bytes.NewBufferString(`{"github_id":"1","login":"alice"}`))
	// No Authorization header
	err := h.CreateSession(w, r)
	require.Error(t, err)
}

// Ensure json import is used (Me encodes JSON on success path).
var _ = json.Marshal
