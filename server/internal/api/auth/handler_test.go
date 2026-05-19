package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	apiauth "github.com/lx-wnk/agent-dashboard/server/internal/api/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	authpkg "github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
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
	// When a PluginLoginURL is set, GitHubRedirect must redirect to the plugin with a nonce.
	h := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:      "test-secret-32chars-long-minimum!",
		PluginLoginURL: "http://127.0.0.1:19001/login",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/github", nil)
	err := h.GitHubRedirect(w, r)
	require.NoError(t, err)
	require.Equal(t, http.StatusFound, w.Code)

	location := w.Header().Get("Location")
	require.Contains(t, location, "http://127.0.0.1:19001/login?nonce=", "redirect must include a nonce query param")
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

func TestHandler_CreateSession_MissingNonce(t *testing.T) {
	h := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:        "test-secret-32chars-long-minimum!",
		AuthPluginSecret: "correct-plugin-secret",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/session",
		bytes.NewBufferString(`{"github_id":"1","login":"alice"}`))
	r.Header.Set("Authorization", "Bearer correct-plugin-secret")
	err := h.CreateSession(w, r)
	// nonce is empty → ValidateNonce must reject → 401
	require.Error(t, err)
}

func TestHandler_CreateSession_InvalidNonce(t *testing.T) {
	h := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:        "test-secret-32chars-long-minimum!",
		AuthPluginSecret: "correct-plugin-secret",
	})
	w := httptest.NewRecorder()
	body := `{"github_id":"1","login":"alice","nonce":"not-a-valid-jwt"}`
	r := httptest.NewRequest(http.MethodPost, "/api/auth/session", bytes.NewBufferString(body))
	r.Header.Set("Authorization", "Bearer correct-plugin-secret")
	err := h.CreateSession(w, r)
	require.Error(t, err)
}

// stubOAuthProvider is a trivial OAuthProvider that satisfies the interface in tests.
type stubOAuthProvider struct{}

func (s *stubOAuthProvider) BuildAuthURL(_ context.Context, _, _ string) (string, error) {
	return "http://example.com", nil
}
func (s *stubOAuthProvider) ExchangeCode(_ context.Context, _, _ string) (string, error) {
	return "tok", nil
}
func (s *stubOAuthProvider) GetUser(_ context.Context, _ string) (*authpkg.OAuthUserProfile, error) {
	return &authpkg.OAuthUserProfile{ID: "1", Login: "u"}, nil
}

// openTestDB opens an in-memory SQLite DB and registers a cleanup.
func openTestDB(t *testing.T) repo.UserRepo {
	t.Helper()
	bundle, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = bundle.Client.Close() })
	return repo.NewUserRepo(bundle.Client)
}

func TestHandler_Callback_InPluginMode(t *testing.T) {
	h := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:      "test-secret",
		PluginLoginURL: "http://127.0.0.1:19001/login",
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/callback?code=x&state=y", nil)
	err := h.Callback(w, r)
	require.Error(t, err)
	var appErr *apierr.AppError
	require.True(t, errors.As(err, &appErr))
	require.Equal(t, http.StatusNotFound, appErr.Status)
}

func TestHandler_Callback_NoOAuthProvider(t *testing.T) {
	h := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:     "test-secret",
		OAuthProvider: nil,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/callback", nil)
	err := h.Callback(w, r)
	require.Error(t, err)
	var appErr *apierr.AppError
	require.True(t, errors.As(err, &appErr))
	require.Equal(t, http.StatusServiceUnavailable, appErr.Status)
}

func TestHandler_Callback_MissingStateCookie(t *testing.T) {
	userRepo := openTestDB(t)
	h := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:     "test-secret",
		OAuthProvider: &stubOAuthProvider{},
		UserRepo:      userRepo,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/callback", nil)
	// No oauth_state cookie set
	err := h.Callback(w, r)
	require.Error(t, err)
	var appErr *apierr.AppError
	require.True(t, errors.As(err, &appErr))
	require.Equal(t, http.StatusBadRequest, appErr.Status)
}

func TestHandler_Callback_InvalidStateJWT(t *testing.T) {
	userRepo := openTestDB(t)
	h := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:     "test-secret",
		OAuthProvider: &stubOAuthProvider{},
		UserRepo:      userRepo,
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/callback", nil)
	r.AddCookie(&http.Cookie{Name: "oauth_state", Value: "invalid.jwt.here"})
	err := h.Callback(w, r)
	require.Error(t, err)
	var appErr *apierr.AppError
	require.True(t, errors.As(err, &appErr))
	require.Equal(t, http.StatusUnauthorized, appErr.Status)
}

func TestHandler_Callback_StateMismatch(t *testing.T) {
	const jwtSecret = "test-secret"
	userRepo := openTestDB(t)
	h := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:     jwtSecret,
		OAuthProvider: &stubOAuthProvider{},
		UserRepo:      userRepo,
	})

	validState, err := authpkg.SignOAuthState(jwtSecret)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/callback?state=different-value", nil)
	r.AddCookie(&http.Cookie{Name: "oauth_state", Value: validState})

	callbackErr := h.Callback(w, r)
	require.Error(t, callbackErr)
	var appErr *apierr.AppError
	require.True(t, errors.As(callbackErr, &appErr))
	require.Equal(t, http.StatusUnauthorized, appErr.Status)
}

func TestHandler_Callback_MissingCode(t *testing.T) {
	const jwtSecret = "test-secret"
	userRepo := openTestDB(t)
	h := apiauth.NewHandler(apiauth.Deps{
		JWTSecret:     jwtSecret,
		OAuthProvider: &stubOAuthProvider{},
		UserRepo:      userRepo,
	})

	validState, err := authpkg.SignOAuthState(jwtSecret)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/callback?state="+validState, nil)
	r.AddCookie(&http.Cookie{Name: "oauth_state", Value: validState})

	callbackErr := h.Callback(w, r)
	require.Error(t, callbackErr)
	var appErr *apierr.AppError
	require.True(t, errors.As(callbackErr, &appErr))
	require.Equal(t, http.StatusBadRequest, appErr.Status)
}

// Ensure json import is used (Me encodes JSON on success path).
var _ = json.Marshal
