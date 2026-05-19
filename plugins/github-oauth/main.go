// Package main is the github-oauth standalone auth plugin for agent-dashboard.
//
// The plugin owns the complete GitHub OAuth dance and creates dashboard sessions
// by calling core's POST /api/auth/session endpoint. Core has no GitHub-specific
// knowledge — it only issues JWT session cookies when a trusted plugin presents
// a verified user profile.
//
// # Architecture
//
//	Browser                 Core                      Plugin                  GitHub
//	  │                       │                          │                       │
//	  │  GET /api/auth/github  │                          │                       │
//	  │──────────────────────►│                          │                       │
//	  │  302 → plugin /login  │                          │                       │
//	  │◄──────────────────────│                          │                       │
//	  │                       │                          │                       │
//	  │  GET /login           │                          │                       │
//	  │─────────────────────────────────────────────────►│                       │
//	  │  302 → GitHub OAuth   │                          │                       │
//	  │◄─────────────────────────────────────────────────│                       │
//	  │                       │                          │                       │
//	  │  GitHub OAuth dance   │                          │                       │
//	  │──────────────────────────────────────────────────────────────────────────►│
//	  │  code + state         │                          │                       │
//	  │◄──────────────────────────────────────────────────────────────────────────│
//	  │                       │                          │                       │
//	  │  GET /callback?code=… │                          │                       │
//	  │─────────────────────────────────────────────────►│                       │
//	  │                       │  POST /api/auth/session  │                       │
//	  │                       │◄─────────────────────────│                       │
//	  │                       │  Set-Cookie: auth_token  │                       │
//	  │                       │─────────────────────────►│                       │
//	  │  302 → /              │                          │                       │
//	  │◄─────────────────────────────────────────────────│                       │
//
// # Required environment variables
//
//	GITHUB_CLIENT_ID             — GitHub OAuth app client ID
//	GITHUB_CLIENT_SECRET         — GitHub OAuth app client secret
//	DASHBOARD_URL                — base URL of the dashboard (e.g. http://127.0.0.1:13120)
//	DASHBOARD_AUTH_PLUGIN_SECRET — shared secret for POST /api/auth/session
//
// # Routes
//
//	GET  /health      → {"ok":true}
//	GET  /login       → redirect to GitHub OAuth (entry point; core forwards here)
//	GET  /callback    → exchange code, fetch user, create core session, redirect to /
//
// # Legacy capability routes (retained for backwards compatibility)
//
//	GET  /capabilities/auth/authorize-url → {"url":"<github-oauth-url>"}
//	POST /capabilities/auth/exchange      → {"token":"<access-token>"}
//	GET  /capabilities/auth/user          → OAuthUserProfile JSON
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	githubAuthURL  = "https://github.com/login/oauth/authorize"
	githubTokenURL = "https://github.com/login/oauth/access_token" //nolint:gosec
	githubUserURL  = "https://api.github.com/user"

	listenAddr = "127.0.0.1:19001"

	// stateCookieName is the CSRF state cookie set during login and validated on callback.
	stateCookieName = "github_oauth_state"
	// stateCookieMaxAge is the lifetime of the CSRF state cookie in seconds (5 minutes).
	stateCookieMaxAge = 300
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	clientID := os.Getenv("GITHUB_CLIENT_ID")
	clientSecret := os.Getenv("GITHUB_CLIENT_SECRET")
	dashboardURL := os.Getenv("DASHBOARD_URL")
	pluginSecret := os.Getenv("DASHBOARD_AUTH_PLUGIN_SECRET")

	if clientID == "" || clientSecret == "" {
		slog.Error("GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET must be set")
		os.Exit(1)
	}
	if dashboardURL == "" {
		slog.Error("DASHBOARD_URL must be set (e.g. http://127.0.0.1:13120)")
		os.Exit(1)
	}
	if pluginSecret == "" {
		slog.Error("DASHBOARD_AUTH_PLUGIN_SECRET must be set")
		os.Exit(1)
	}

	h := &handler{
		clientID:     clientID,
		clientSecret: clientSecret,
		dashboardURL: strings.TrimRight(dashboardURL, "/"),
		pluginSecret: pluginSecret,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		// The GitHub OAuth callback URL points back to this plugin.
		callbackURL: "http://" + listenAddr + "/callback",
	}

	mux := http.NewServeMux()

	// Health check — required by plugin registry.
	mux.HandleFunc("GET /health", h.health)

	// Primary flow: standalone OAuth dance.
	mux.HandleFunc("GET /login", h.login)
	mux.HandleFunc("GET /callback", h.callback)

	// Legacy capability routes — kept for backwards compatibility with deployments
	// using the old in-core OAuthProvider proxy flow.
	mux.HandleFunc("GET /capabilities/auth/authorize-url", h.authorizeURL)
	mux.HandleFunc("POST /capabilities/auth/exchange", h.exchange)
	mux.HandleFunc("GET /capabilities/auth/user", h.user)

	srv := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	slog.Info("github-oauth plugin listening", "addr", listenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

type handler struct {
	clientID     string
	clientSecret string
	dashboardURL string // e.g. "http://127.0.0.1:13120"
	pluginSecret string
	callbackURL  string // GitHub redirects here after OAuth
	httpClient   *http.Client
}

// health responds with {"ok":true}. Required by the registry health-check.
func (h *handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// login kicks off the GitHub OAuth flow.
// It generates a random CSRF state, stores it in a short-lived cookie, and redirects to GitHub.
// GET /login
func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	state, err := randomState()
	if err != nil {
		slog.Error("login: generate state", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}

	http.SetCookie(w, &http.Cookie{ //nolint:gosec
		Name:     stateCookieName,
		Value:    state,
		MaxAge:   stateCookieMaxAge,
		HttpOnly: true,
		Secure:   false, // plugin is always loopback
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})

	v := url.Values{}
	v.Set("client_id", h.clientID)
	v.Set("state", state)
	v.Set("redirect_uri", h.callbackURL)
	v.Set("scope", "read:user")

	http.Redirect(w, r, githubAuthURL+"?"+v.Encode(), http.StatusFound)
}

// callback handles the GitHub OAuth callback.
// It validates the CSRF state, exchanges the code for a token, fetches the user profile,
// and calls core's POST /api/auth/session to create a session cookie.
// GET /callback?code=XXX&state=YYY
func (h *handler) callback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing state cookie")
		return
	}
	if r.URL.Query().Get("state") != stateCookie.Value {
		writeError(w, http.StatusUnauthorized, "state mismatch")
		return
	}
	// Clear the state cookie immediately.
	http.SetCookie(w, &http.Cookie{ //nolint:gosec
		Name:   stateCookieName,
		MaxAge: -1,
		Path:   "/",
	})

	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing code")
		return
	}

	accessToken, err := h.exchangeCode(r.Context(), code, h.callbackURL)
	if err != nil {
		slog.Error("callback: exchange code", "err", err)
		writeError(w, http.StatusBadGateway, "code exchange failed")
		return
	}

	profile, err := h.getUser(r.Context(), accessToken)
	if err != nil {
		slog.Error("callback: get user", "err", err)
		writeError(w, http.StatusBadGateway, "failed to fetch user profile")
		return
	}

	sessionCookie, err := h.createCoreSession(r.Context(), profile)
	if err != nil {
		slog.Error("callback: create core session", "err", err)
		writeError(w, http.StatusBadGateway, "failed to create session")
		return
	}

	// Forward the session cookie from core to the browser.
	http.SetCookie(w, sessionCookie)

	// Redirect to the dashboard root.
	http.Redirect(w, r, h.dashboardURL+"/", http.StatusFound)
}

// createCoreSession calls core's POST /api/auth/session and returns the session cookie.
func (h *handler) createCoreSession(ctx context.Context, profile *oauthUserProfile) (*http.Cookie, error) {
	body, err := json.Marshal(map[string]string{
		"github_id":    profile.ID,
		"login":        profile.Login,
		"display_name": profile.DisplayName,
		"avatar_url":   profile.AvatarURL,
	})
	if err != nil {
		return nil, fmt.Errorf("createCoreSession: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		h.dashboardURL+"/api/auth/session", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("createCoreSession: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.pluginSecret)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("createCoreSession: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("createCoreSession: HTTP %d: %s", resp.StatusCode, b)
	}

	// Find the auth_token cookie in core's response.
	for _, c := range resp.Cookies() {
		if c.Name == "auth_token" {
			return c, nil
		}
	}
	return nil, fmt.Errorf("createCoreSession: auth_token cookie missing from core response")
}

// --- Legacy capability routes (in-core OAuthProvider proxy flow) ---

// authorizeURL builds the GitHub OAuth authorization URL.
// GET /capabilities/auth/authorize-url?state=XXX&redirect_uri=YYY
func (h *handler) authorizeURL(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	redirectURI := q.Get("redirect_uri")

	v := url.Values{}
	v.Set("client_id", h.clientID)
	v.Set("state", state)
	v.Set("redirect_uri", redirectURI)
	v.Set("scope", "read:user")

	writeJSON(w, http.StatusOK, map[string]string{"url": githubAuthURL + "?" + v.Encode()})
}

// exchange accepts {"code":"...","redirect_uri":"..."} and returns {"token":"..."}.
// POST /capabilities/auth/exchange
func (h *handler) exchange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code        string `json:"code"`
		RedirectURI string `json:"redirect_uri"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	token, err := h.exchangeCode(r.Context(), req.Code, req.RedirectURI)
	if err != nil {
		slog.Error("exchange: failed", "err", err)
		writeError(w, http.StatusBadGateway, "code exchange failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

// user fetches the GitHub user profile for the token in the Authorization header.
// GET /capabilities/auth/user
func (h *handler) user(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "missing Bearer token")
		return
	}
	token := strings.TrimPrefix(authHeader, "Bearer ")

	profile, err := h.getUser(r.Context(), token)
	if err != nil {
		slog.Error("user: failed", "err", err)
		writeError(w, http.StatusBadGateway, "failed to fetch user profile")
		return
	}
	writeJSON(w, http.StatusOK, profile)
}

// --- GitHub API helpers ---

// exchangeCode exchanges an OAuth authorization code for an access token via GitHub.
func (h *handler) exchangeCode(ctx context.Context, code, redirectURI string) (string, error) {
	v := url.Values{}
	v.Set("client_id", h.clientID)
	v.Set("client_secret", h.clientSecret)
	v.Set("code", code)
	v.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, githubTokenURL, strings.NewReader(v.Encode()))
	if err != nil {
		return "", fmt.Errorf("exchange: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchange: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("exchange: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("exchange: HTTP %d: %s", resp.StatusCode, body)
	}

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("exchange: decode: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("exchange: %s", result.Error)
	}
	return result.AccessToken, nil
}

// oauthUserProfile holds the user profile returned by GitHub and sent to core.
type oauthUserProfile struct {
	ID          string `json:"ID"`
	Login       string `json:"Login"`
	DisplayName string `json:"DisplayName"`
	AvatarURL   string `json:"AvatarURL"`
}

// getUser fetches the GitHub user profile for the given access token.
func (h *handler) getUser(ctx context.Context, accessToken string) (*oauthUserProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubUserURL, nil)
	if err != nil {
		return nil, fmt.Errorf("getUser: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getUser: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("getUser: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getUser: HTTP %d: %s", resp.StatusCode, body)
	}

	var raw struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("getUser: decode: %w", err)
	}
	return &oauthUserProfile{
		ID:          strconv.FormatInt(raw.ID, 10),
		Login:       raw.Login,
		DisplayName: raw.Name,
		AvatarURL:   raw.AvatarURL,
	}, nil
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// randomState generates a cryptographically random 32-byte URL-safe string for OAuth CSRF protection.
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("randomState: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
