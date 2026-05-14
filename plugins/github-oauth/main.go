// Package main is the github-oauth reference plugin — a standalone HTTP sidecar that
// implements the auth_provider capability expected by the agent-dashboard plugin system.
//
// Required env vars:
//
//	GITHUB_CLIENT_ID     — GitHub OAuth app client ID
//	GITHUB_CLIENT_SECRET — GitHub OAuth app client secret
//
// The plugin binds to 127.0.0.1:19001 and serves the following routes:
//
//	GET  /health                          → {"ok":true}
//	GET  /capabilities/auth/authorize-url → {"url":"<github-oauth-url>"}
//	POST /capabilities/auth/exchange      → {"token":"<access-token>"}
//	GET  /capabilities/auth/user          → OAuthUserProfile JSON
package main

import (
	"context"
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
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	clientID := os.Getenv("GITHUB_CLIENT_ID")
	clientSecret := os.Getenv("GITHUB_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		slog.Error("GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET must be set")
		os.Exit(1)
	}

	h := &handler{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
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
	httpClient   *http.Client
}

// health responds with {"ok":true}. Required by the registry health-check.
func (h *handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// authorizeURL builds the GitHub OAuth authorization URL.
// Query params: state, redirect_uri.
func (h *handler) authorizeURL(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	redirectURI := q.Get("redirect_uri")

	v := url.Values{}
	v.Set("client_id", h.clientID)
	v.Set("state", state)
	v.Set("redirect_uri", redirectURI)
	v.Set("scope", "read:user")

	authURL := githubAuthURL + "?" + v.Encode()
	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

// exchange accepts {"code":"...","redirect_uri":"..."} and returns {"token":"..."}.
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

// oauthUserProfile mirrors the dashboard's auth.OAuthUserProfile for JSON serialization.
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
