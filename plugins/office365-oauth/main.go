// Package main is the office365-oauth standalone auth plugin for agent-dashboard.
//
// The plugin owns the complete Microsoft OAuth2/OIDC dance and creates dashboard
// sessions by calling core's POST /api/auth/session endpoint. Core has no
// Microsoft-specific knowledge.
//
// # Required environment variables
//
//	AZURE_CLIENT_ID              — OAuth app (application) client ID
//	AZURE_CLIENT_SECRET          — OAuth app client secret
//	AZURE_TENANT_ID              — Azure AD tenant ID (directory ID)
//	DASHBOARD_URL                — base URL of the dashboard (e.g. http://127.0.0.1:13120)
//	DASHBOARD_AUTH_PLUGIN_SECRET — shared secret for POST /api/auth/session (≥32 chars)
//
// # Optional environment variables
//
//	OFFICE365_ALLOWED_GROUP_ID   — Azure AD group object ID; if set, non-members get 403
//
// # Routes
//
//	GET  /health    → {"ok":true}
//	GET  /login     → redirect to Microsoft OAuth (entry point; core forwards here)
//	GET  /callback  → exchange code, fetch user, check group, create session, redirect to /
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
	"strings"
	"time"
)

const (
	listenAddr        = "127.0.0.1:19002"
	graphMeURL        = "https://graph.microsoft.com/v1.0/me"
	graphMemberOfURL  = "https://graph.microsoft.com/v1.0/me/memberOf?$select=id&$top=999"
	stateCookieName   = "ms_oauth_state"
	stateCookieMaxAge = 300
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	clientID     := os.Getenv("AZURE_CLIENT_ID")
	clientSecret := os.Getenv("AZURE_CLIENT_SECRET")
	tenantID     := os.Getenv("AZURE_TENANT_ID")
	dashboardURL := os.Getenv("DASHBOARD_URL")
	pluginSecret := os.Getenv("DASHBOARD_AUTH_PLUGIN_SECRET")
	allowedGroup := os.Getenv("OFFICE365_ALLOWED_GROUP_ID")

	if clientID == "" || clientSecret == "" {
		slog.Error("AZURE_CLIENT_ID and AZURE_CLIENT_SECRET must be set")
		os.Exit(1)
	}
	if tenantID == "" {
		slog.Error("AZURE_TENANT_ID must be set")
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
	if len(pluginSecret) < 32 {
		slog.Error("DASHBOARD_AUTH_PLUGIN_SECRET must be at least 32 characters")
		os.Exit(1)
	}

	h := &handler{
		clientID:        clientID,
		clientSecret:    clientSecret,
		tenantID:        tenantID,
		dashboardURL:    strings.TrimRight(dashboardURL, "/"),
		pluginSecret:    pluginSecret,
		allowedGroup:    allowedGroup,
		httpClient:      &http.Client{Timeout: 10 * time.Second},
		callbackURL:     "http://" + listenAddr + "/callback",
		tokenURL:        fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", url.PathEscape(tenantID)),
		msGraphMeURL:    graphMeURL,
		msGraphMemberURL: graphMemberOfURL,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("GET /login", h.login)
	mux.HandleFunc("GET /callback", h.callback)

	srv := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	slog.Info("office365-oauth plugin listening", "addr", listenAddr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

type handler struct {
	clientID     string
	clientSecret string
	tenantID     string
	dashboardURL string
	pluginSecret string
	allowedGroup string
	callbackURL  string
	httpClient   *http.Client
	// injectable for testing; set from constants/tenant in main.
	tokenURL        string
	msGraphMeURL    string
	msGraphMemberURL string
}

func (h *handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// login redirects to Microsoft OAuth2. Embeds core's nonce in state cookie as "<csrf>.<nonce>".
func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	nonce := r.URL.Query().Get("nonce")
	if nonce == "" {
		writeError(w, http.StatusBadRequest, "missing nonce")
		return
	}

	csrfState, err := randomState()
	if err != nil {
		slog.Error("login: generate state", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to generate state")
		return
	}

	stateValue := csrfState + "." + nonce

	http.SetCookie(w, &http.Cookie{ //nolint:gosec
		Name:     stateCookieName,
		Value:    stateValue,
		MaxAge:   stateCookieMaxAge,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})

	scopes := "openid profile email User.Read"
	if h.allowedGroup != "" {
		scopes += " GroupMember.Read.All"
	}

	v := url.Values{}
	v.Set("client_id", h.clientID)
	v.Set("response_type", "code")
	v.Set("redirect_uri", h.callbackURL)
	v.Set("scope", scopes)
	v.Set("state", stateValue)
	v.Set("response_mode", "query")

	authURL := fmt.Sprintf(
		"https://login.microsoftonline.com/%s/oauth2/v2.0/authorize?%s",
		url.PathEscape(h.tenantID), v.Encode(),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// callback handles the Microsoft OAuth2 callback.
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

	stateValue := stateCookie.Value
	dotIdx := strings.Index(stateValue, ".")
	if dotIdx < 0 {
		writeError(w, http.StatusBadRequest, "malformed state")
		return
	}
	nonce := stateValue[dotIdx+1:]
	if nonce == "" {
		writeError(w, http.StatusBadRequest, "empty nonce in state")
		return
	}

	http.SetCookie(w, &http.Cookie{ //nolint:gosec
		Name:   stateCookieName,
		MaxAge: -1,
		Path:   "/",
	})

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		slog.Warn("callback: oauth error from provider", "error", errParam, "description", r.URL.Query().Get("error_description"))
		writeError(w, http.StatusForbidden, "authentication denied by provider")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing code")
		return
	}

	accessToken, err := h.exchangeCode(r.Context(), code)
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

	if h.allowedGroup != "" {
		member, err := h.isMember(r.Context(), accessToken, h.allowedGroup)
		if err != nil {
			slog.Error("callback: group check", "err", err)
			writeError(w, http.StatusBadGateway, "group membership check failed")
			return
		}
		if !member {
			writeError(w, http.StatusForbidden, "access denied: not a member of the required group")
			return
		}
	}

	sessionCookie, err := h.createCoreSession(r.Context(), profile, nonce)
	if err != nil {
		slog.Error("callback: create core session", "err", err)
		writeError(w, http.StatusBadGateway, "failed to create session")
		return
	}

	http.SetCookie(w, sessionCookie)
	http.Redirect(w, r, h.dashboardURL+"/", http.StatusFound)
}

func (h *handler) exchangeCode(ctx context.Context, code string) (string, error) {
	v := url.Values{}
	v.Set("client_id", h.clientID)
	v.Set("client_secret", h.clientSecret)
	v.Set("code", code)
	v.Set("redirect_uri", h.callbackURL)
	v.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.tokenURL, strings.NewReader(v.Encode()))
	if err != nil {
		return "", fmt.Errorf("exchangeCode: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("exchangeCode: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("exchangeCode: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("exchangeCode: HTTP %d: %s", resp.StatusCode, truncateBody(body))
	}

	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("exchangeCode: decode: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("exchangeCode: %s: %s", result.Error, result.ErrorDesc)
	}
	return result.AccessToken, nil
}

type msUserProfile struct {
	ID                string `json:"id"`
	DisplayName       string `json:"displayName"`
	UserPrincipalName string `json:"userPrincipalName"`
}

func (h *handler) getUser(ctx context.Context, accessToken string) (*msUserProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.msGraphMeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("getUser: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

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
		return nil, fmt.Errorf("getUser: HTTP %d: %s", resp.StatusCode, truncateBody(body))
	}

	var profile msUserProfile
	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, fmt.Errorf("getUser: decode: %w", err)
	}
	if profile.ID == "" {
		return nil, fmt.Errorf("getUser: empty id in response")
	}
	return &profile, nil
}

func (h *handler) isMember(ctx context.Context, accessToken, groupID string) (bool, error) {
	nextURL := h.msGraphMemberURL
	for nextURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return false, fmt.Errorf("isMember: build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)

		resp, err := h.httpClient.Do(req)
		if err != nil {
			return false, fmt.Errorf("isMember: %w", err)
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return false, fmt.Errorf("isMember: read body: %w", readErr)
		}
		if resp.StatusCode != http.StatusOK {
			return false, fmt.Errorf("isMember: HTTP %d: %s", resp.StatusCode, body)
		}

		var page struct {
			Value    []struct{ ID string `json:"id"` } `json:"value"`
			NextLink string                             `json:"@odata.nextLink"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return false, fmt.Errorf("isMember: decode: %w", err)
		}
		for _, g := range page.Value {
			if g.ID == groupID {
				return true, nil
			}
		}
		// Trailing slash anchors the prefix — "https://graph.microsoft.com/" prevents
		// spoofs like "https://graph.microsoft.com.evil.example.com/".
		if page.NextLink != "" && !strings.HasPrefix(page.NextLink, "https://graph.microsoft.com/") {
			return false, fmt.Errorf("isMember: unexpected nextLink host: %s", page.NextLink)
		}
		nextURL = page.NextLink
	}
	return false, nil
}

func (h *handler) createCoreSession(ctx context.Context, profile *msUserProfile, nonce string) (*http.Cookie, error) {
	body, err := json.Marshal(map[string]string{
		"provider_id":    profile.ID,
		"login":        profile.UserPrincipalName,
		"display_name": profile.DisplayName,
		"avatar_url":   "",
		"nonce":        nonce,
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

	for _, c := range resp.Cookies() {
		if c.Name == "auth_token" {
			return c, nil
		}
	}
	return nil, fmt.Errorf("createCoreSession: auth_token cookie missing from core response")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func truncateBody(b []byte) string {
	const max = 200
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "…"
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("randomState: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
