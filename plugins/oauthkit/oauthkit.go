// Package oauthkit is the shared, provider-agnostic core of the agent-dashboard
// OAuth auth plugins. It owns the security-critical plumbing that must behave
// identically across every provider: CSRF state generation, the state cookie
// carrying "<csrf>.<nonce>", callback state validation, authorization-code
// extraction, and dashboard session creation via core's POST /api/auth/session.
//
// A provider plugin (github-oauth, office365-oauth, …) keeps only its
// provider-specific pieces — authorize-URL construction, token exchange, user
// profile fetch, and any authorization checks — and delegates the shared flow
// here so there is exactly one audited copy of the CSRF/session code.
package oauthkit

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// StateCookieMaxAge is the lifetime of the CSRF state cookie in seconds.
const StateCookieMaxAge = 300

// RandomState returns a cryptographically random 32-byte URL-safe string used as
// the OAuth CSRF state.
func RandomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("randomState: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// WriteJSON writes v as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes {"error": msg} with the given status.
func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, map[string]string{"error": msg})
}

// Health writes {"ok":true}. Required by the plugin registry health-check.
func Health(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// StartLogin reads core's nonce from the request, generates a fresh CSRF state,
// and sets the state cookie to "<csrf>.<nonce>" so both values survive the OAuth
// round-trip without server-side storage. It returns the full state value to
// embed as the provider's `state` parameter. On failure it writes the error
// response and returns ok=false; the caller must return immediately.
func StartLogin(w http.ResponseWriter, r *http.Request, cookieName string) (stateValue string, ok bool) {
	nonce := r.URL.Query().Get("nonce")
	if nonce == "" {
		WriteError(w, http.StatusBadRequest, "missing nonce")
		return "", false
	}
	csrfState, err := RandomState()
	if err != nil {
		slog.Error("oauthkit: generate state", "err", err)
		WriteError(w, http.StatusInternalServerError, "failed to generate state")
		return "", false
	}
	stateValue = csrfState + "." + nonce
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // plugin is always loopback (Secure=false intentional)
		Name:     cookieName,
		Value:    stateValue,
		MaxAge:   StateCookieMaxAge,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteLaxMode,
		Path:     "/",
	})
	return stateValue, true
}

// ValidateState validates the OAuth callback CSRF state and recovers the nonce.
// It checks the state cookie is present, matches the `state` query parameter, and
// carries a non-empty nonce after the first "."; it then clears the cookie. On
// any failure it writes the error response and returns ok=false; the caller must
// return immediately. The returned nonce is core's flow-binding JWT.
func ValidateState(w http.ResponseWriter, r *http.Request, cookieName string) (nonce string, ok bool) {
	stateCookie, err := r.Cookie(cookieName)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "missing state cookie")
		return "", false
	}
	if r.URL.Query().Get("state") != stateCookie.Value {
		WriteError(w, http.StatusUnauthorized, "state mismatch")
		return "", false
	}

	// The state value is "<csrf>.<nonce>"; split on the first dot. The nonce is a
	// JWT which itself may contain dots, so only the first "." is a separator.
	dotIdx := strings.Index(stateCookie.Value, ".")
	if dotIdx < 0 {
		WriteError(w, http.StatusBadRequest, "malformed state: missing nonce")
		return "", false
	}
	nonce = stateCookie.Value[dotIdx+1:]
	if nonce == "" {
		WriteError(w, http.StatusBadRequest, "malformed state: empty nonce")
		return "", false
	}

	http.SetCookie(w, &http.Cookie{ //nolint:gosec // clearing cookie (MaxAge<0)
		Name:   cookieName,
		MaxAge: -1,
		Path:   "/",
	})
	return nonce, true
}

// ExtractCode reads the authorization code from the callback, mapping a
// provider-signalled `error` to 403 and a missing code to 400. On failure it
// writes the error response and returns ok=false; the caller must return.
func ExtractCode(w http.ResponseWriter, r *http.Request) (code string, ok bool) {
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		slog.Warn("oauthkit: oauth error from provider", "error", errParam, "description", r.URL.Query().Get("error_description"))
		WriteError(w, http.StatusForbidden, "authentication denied by provider")
		return "", false
	}
	code = r.URL.Query().Get("code")
	if code == "" {
		WriteError(w, http.StatusBadRequest, "missing code")
		return "", false
	}
	return code, true
}

// SessionRequest is the verified user profile a plugin presents to core to mint a
// dashboard session. Each provider maps its own profile onto these fields.
type SessionRequest struct {
	ProviderID  string
	Login       string
	DisplayName string
	AvatarURL   string
}

// CreateSession calls core's POST /api/auth/session with the profile and the
// flow-binding nonce, authenticated by the shared plugin secret, and returns the
// auth_token cookie core issues. The dashboardURL must be trailing-slash-trimmed.
func CreateSession(ctx context.Context, client *http.Client, dashboardURL, pluginSecret string, req SessionRequest, nonce string) (*http.Cookie, error) {
	body, err := json.Marshal(map[string]string{
		"provider_id":  req.ProviderID,
		"login":        req.Login,
		"display_name": req.DisplayName,
		"avatar_url":   req.AvatarURL,
		"nonce":        nonce,
	})
	if err != nil {
		return nil, fmt.Errorf("createCoreSession: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		dashboardURL+"/api/auth/session", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("createCoreSession: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+pluginSecret)

	resp, err := client.Do(httpReq)
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
