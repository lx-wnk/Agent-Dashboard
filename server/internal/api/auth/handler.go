// Package auth provides HTTP handlers for GitHub OAuth authentication.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/lx-wnk/agent-dashboard/server/internal/apierr"
	serverauth "github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/db/repo"
)

// Deps holds all dependencies for the auth handler.
type Deps struct {
	JWTSecret    string
	CallbackURL  string
	GitHubClient *serverauth.GitHubClient
	UserRepo     repo.UserRepo
}

// Handler handles GitHub OAuth routes.
type Handler struct {
	deps Deps
}

// NewHandler creates an auth Handler.
func NewHandler(deps Deps) *Handler {
	return &Handler{deps: deps}
}

// GitHubRedirect redirects the browser to the GitHub authorization URL.
// GET /api/auth/github
func (h *Handler) GitHubRedirect(w http.ResponseWriter, r *http.Request) error {
	if h.deps.GitHubClient == nil {
		return apierr.NewAppError(http.StatusServiceUnavailable, "GitHub OAuth not configured")
	}
	statePayload := serverauth.JWTPayload{Sub: "oauth-state"}
	state, err := serverauth.SignJWT(statePayload, h.deps.JWTSecret, 300)
	if err != nil {
		return fmt.Errorf("auth: build state: %w", err)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		MaxAge:   300,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
	http.Redirect(w, r, h.deps.GitHubClient.BuildAuthURL(state, h.deps.CallbackURL), http.StatusFound)
	return nil
}

// Callback handles the GitHub OAuth callback.
// GET /api/auth/callback?code=XXX&state=YYY
func (h *Handler) Callback(w http.ResponseWriter, r *http.Request) error {
	if h.deps.GitHubClient == nil {
		return apierr.NewAppError(http.StatusServiceUnavailable, "GitHub OAuth not configured")
	}
	if h.deps.UserRepo == nil {
		return apierr.NewAppError(http.StatusServiceUnavailable, "user store unavailable")
	}

	stateCookie, err := r.Cookie("oauth_state")
	if err != nil {
		return apierr.NewAppError(http.StatusBadRequest, "missing state cookie")
	}
	if _, err := serverauth.VerifyJWT(stateCookie.Value, h.deps.JWTSecret); err != nil {
		return apierr.NewAppError(http.StatusUnauthorized, "invalid state token")
	}
	if r.URL.Query().Get("state") != stateCookie.Value {
		return apierr.NewAppError(http.StatusUnauthorized, "state mismatch")
	}

	code := r.URL.Query().Get("code")
	accessToken, err := h.deps.GitHubClient.ExchangeCode(r.Context(), code, h.deps.CallbackURL)
	if err != nil {
		return fmt.Errorf("auth: exchange code: %w", err)
	}

	profile, err := h.deps.GitHubClient.GetUser(r.Context(), accessToken)
	if err != nil {
		return fmt.Errorf("auth: get user: %w", err)
	}

	user, err := h.deps.UserRepo.Upsert(r.Context(), repo.GitHubUserInfo{
		ID:          profile.ID,
		Login:       profile.Login,
		DisplayName: profile.DisplayName,
		AvatarURL:   profile.AvatarURL,
	})
	if err != nil {
		return fmt.Errorf("auth: upsert user: %w", err)
	}

	tokenPayload := serverauth.JWTPayload{
		Sub:     user.ID,
		Login:   user.GithubLogin,
		IsAdmin: user.IsAdmin,
	}
	token, err := serverauth.SignJWT(tokenPayload, h.deps.JWTSecret, 86400*7)
	if err != nil {
		return fmt.Errorf("auth: sign jwt: %w", err)
	}

	http.SetCookie(w, &http.Cookie{Name: "oauth_state", MaxAge: -1, Path: "/"})
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		MaxAge:   86400 * 7,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
	http.Redirect(w, r, "/", http.StatusFound)
	return nil
}

// Logout clears the auth cookie.
// POST /api/auth/logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) error {
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    "",
		MaxAge:   -1,
		HttpOnly: true,
		Path:     "/",
	})
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// Me returns the currently authenticated user.
// GET /api/auth/me
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) error {
	payload, ok := serverauth.PayloadFromContext(r.Context())
	if !ok {
		return apierr.NewAppError(http.StatusUnauthorized, "unauthorized")
	}
	if h.deps.UserRepo == nil {
		return fmt.Errorf("auth: user repo not configured")
	}
	user, err := h.deps.UserRepo.GetByID(r.Context(), payload.Sub)
	if err != nil {
		if errors.Is(err, serverauth.ErrTokenInvalid) {
			return apierr.NewAppError(http.StatusUnauthorized, "unauthorized")
		}
		return apierr.NewAppError(http.StatusUnauthorized, "user not found")
	}
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(map[string]any{
		"id":           user.ID,
		"github_login": user.GithubLogin,
		"display_name": user.DisplayName,
		"avatar_url":   user.AvatarURL,
		"is_admin":     user.IsAdmin,
	})
}
