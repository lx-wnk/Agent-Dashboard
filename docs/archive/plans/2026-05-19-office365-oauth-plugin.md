# Office365 OAuth Plugin Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `plugins/office365-oauth/` — a standalone `auth_provider` plugin that authenticates users via Microsoft single-tenant OAuth2/OIDC, with optional group-based access restriction.

**Architecture:** Mirrors `plugins/github-oauth/` exactly. Standalone Go module. Owns the full OAuth dance; calls `POST /api/auth/session` on core. No changes to core. Three routes: `/health`, `/login`, `/callback`. Group check (optional) uses Microsoft Graph `/me/memberOf`.

**Tech Stack:** Go 1.24 (stdlib `net/http`, `encoding/json`), Microsoft Identity Platform OAuth2, Microsoft Graph API

---

## File Map

- Create: `plugins/office365-oauth/plugin.json`
- Create: `plugins/office365-oauth/go.mod`
- Create: `plugins/office365-oauth/main.go`

---

### Task 1: plugin.json and go.mod

**Files:**
- Create: `plugins/office365-oauth/plugin.json`
- Create: `plugins/office365-oauth/go.mod`

- [ ] **Step 1: Create `plugins/office365-oauth/plugin.json`**

```json
{
  "id": "office365-oauth",
  "version": "1.0.0",
  "capabilities": ["auth_provider"],
  "addr": "127.0.0.1:19002",
  "command": ["./office365-oauth"],
  "env": [
    "AZURE_CLIENT_ID",
    "AZURE_CLIENT_SECRET",
    "AZURE_TENANT_ID",
    "DASHBOARD_URL",
    "DASHBOARD_AUTH_PLUGIN_SECRET",
    "OFFICE365_ALLOWED_GROUP_ID"
  ]
}
```

- [ ] **Step 2: Create `plugins/office365-oauth/go.mod`**

```
module github.com/lx-wnk/agent-dashboard-plugin-office365-oauth

go 1.24
```

- [ ] **Step 3: Commit**

```bash
git add plugins/office365-oauth/plugin.json plugins/office365-oauth/go.mod
git commit -m "feat(office365): scaffold plugin descriptor and go.mod"
```

---

### Task 2: main.go — full plugin implementation

**Files:**
- Create: `plugins/office365-oauth/main.go`

- [ ] **Step 1: Create `plugins/office365-oauth/main.go`**

```go
// Package main is the office365-oauth standalone auth plugin for agent-dashboard.
//
// The plugin owns the complete Microsoft OAuth2/OIDC dance and creates dashboard
// sessions by calling core's POST /api/auth/session endpoint. Core has no
// Microsoft-specific knowledge.
//
// # Architecture
//
//	Browser          Core                     Plugin              Microsoft
//	  │                │                        │                    │
//	  │ GET /api/auth/github                     │                    │
//	  │───────────────►│                        │                    │
//	  │ 302 → /login   │                        │                    │
//	  │◄───────────────│                        │                    │
//	  │ GET /login?nonce=<jwt>                  │                    │
//	  │───────────────────────────────────────►│                    │
//	  │ 302 → Microsoft OAuth                   │                    │
//	  │◄───────────────────────────────────────│                    │
//	  │                    Microsoft OAuth dance │                    │
//	  │◄────────────────────────────────────────────────────────────│
//	  │ GET /callback?code=…                   │                    │
//	  │───────────────────────────────────────►│                    │
//	  │                │  POST /api/auth/session│                    │
//	  │                │◄───────────────────────│                    │
//	  │                │  Set-Cookie: auth_token│                    │
//	  │                │───────────────────────►│                    │
//	  │ 302 → /        │                        │                    │
//	  │◄───────────────────────────────────────│                    │
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
	listenAddr = "127.0.0.1:19002"

	graphMeURL        = "https://graph.microsoft.com/v1.0/me"
	graphMemberOfURL  = "https://graph.microsoft.com/v1.0/me/memberOf?$select=id&$top=999"

	stateCookieName   = "ms_oauth_state"
	stateCookieMaxAge = 300 // 5 minutes
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
	allowedGroup := os.Getenv("OFFICE365_ALLOWED_GROUP_ID") // optional

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
		clientID:     clientID,
		clientSecret: clientSecret,
		tenantID:     tenantID,
		dashboardURL: strings.TrimRight(dashboardURL, "/"),
		pluginSecret: pluginSecret,
		allowedGroup: allowedGroup,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		callbackURL:  "http://" + listenAddr + "/callback",
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
	allowedGroup string // empty = no group restriction
	callbackURL  string
	httpClient   *http.Client
}

func (h *handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// login redirects the browser to Microsoft's OAuth2 authorization endpoint.
// It embeds core's nonce into the state cookie as "<csrfPart>.<nonce>" for CSRF protection.
// GET /login?nonce=<jwt>
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
		Secure:   false, // plugin is always loopback
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
		h.tenantID, v.Encode(),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// callback handles the Microsoft OAuth2 callback.
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

	// Split "<csrfPart>.<nonce>" on the first dot; the nonce is a JWT and may contain dots.
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

	// Clear CSRF cookie immediately.
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

	// Group restriction: check before issuing any session.
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

// exchangeCode exchanges an OAuth2 authorization code for an access token.
func (h *handler) exchangeCode(ctx context.Context, code string) (string, error) {
	v := url.Values{}
	v.Set("client_id", h.clientID)
	v.Set("client_secret", h.clientSecret)
	v.Set("code", code)
	v.Set("redirect_uri", h.callbackURL)
	v.Set("grant_type", "authorization_code")

	tokenURL := fmt.Sprintf(
		"https://login.microsoftonline.com/%s/oauth2/v2.0/token",
		h.tenantID,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(v.Encode()))
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
		return "", fmt.Errorf("exchangeCode: HTTP %d: %s", resp.StatusCode, body)
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

// getUser calls Microsoft Graph /me and returns the user profile.
func (h *handler) getUser(ctx context.Context, accessToken string) (*msUserProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, graphMeURL, nil)
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
		return nil, fmt.Errorf("getUser: HTTP %d: %s", resp.StatusCode, body)
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

// isMember checks whether the authenticated user is a member of groupID
// using GET /me/memberOf. Handles @odata.nextLink pagination.
func (h *handler) isMember(ctx context.Context, accessToken, groupID string) (bool, error) {
	nextURL := graphMemberOfURL
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
		nextURL = page.NextLink
	}
	return false, nil
}

// createCoreSession calls core's POST /api/auth/session and returns the auth_token cookie.
func (h *handler) createCoreSession(ctx context.Context, profile *msUserProfile, nonce string) (*http.Cookie, error) {
	body, err := json.Marshal(map[string]string{
		"github_id":    profile.ID,
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

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("randomState: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
```

- [ ] **Step 2: Build the plugin to verify it compiles**

```bash
cd plugins/office365-oauth && GOWORK=off go build -o office365-oauth .
```

Expected: binary `plugins/office365-oauth/office365-oauth` created, no errors.

- [ ] **Step 3: Verify health endpoint works**

```bash
cd plugins/office365-oauth && \
  AZURE_CLIENT_ID=test AZURE_CLIENT_SECRET=test AZURE_TENANT_ID=test \
  DASHBOARD_URL=http://127.0.0.1:13120 \
  DASHBOARD_AUTH_PLUGIN_SECRET=thisisatestsecrethelloworld12345 \
  ./office365-oauth &
sleep 1
curl -s http://127.0.0.1:19002/health
kill %1
```

Expected: `{"ok":true}` printed.

- [ ] **Step 4: Verify missing env var exits with error**

```bash
cd plugins/office365-oauth && AZURE_CLIENT_ID=test ./office365-oauth; echo "exit: $?"
```

Expected: logs `AZURE_CLIENT_SECRET must be set` and `exit: 1`.

- [ ] **Step 5: Verify short plugin secret exits with error**

```bash
cd plugins/office365-oauth && \
  AZURE_CLIENT_ID=x AZURE_CLIENT_SECRET=x AZURE_TENANT_ID=x \
  DASHBOARD_URL=http://127.0.0.1:13120 DASHBOARD_AUTH_PLUGIN_SECRET=short \
  ./office365-oauth; echo "exit: $?"
```

Expected: logs `DASHBOARD_AUTH_PLUGIN_SECRET must be at least 32 characters` and `exit: 1`.

- [ ] **Step 6: Commit**

```bash
git add plugins/office365-oauth/main.go
git commit -m "feat(office365): implement standalone OAuth2 plugin with group restriction support"
```

---

### Task 3: Build documentation

**Files:**
- Modify: `docs/plugin-guide.md` — add office365-oauth setup section (reference alongside github-oauth)

- [ ] **Step 1: Add office365-oauth to the plugin guide**

At the end of `docs/plugin-guide.md`, append:

```markdown
---

## Reference: office365-oauth plugin

`plugins/office365-oauth/` implements the `auth_provider` capability for Microsoft single-tenant Azure AD.

### Files

| File | Purpose |
|------|---------|
| `plugin.json` | Descriptor — capability `auth_provider`, addr `127.0.0.1:19002`, command `./office365-oauth` |
| `go.mod` | Standalone module (`github.com/lx-wnk/agent-dashboard-plugin-office365-oauth`) |
| `main.go` | HTTP server implementing standalone OAuth2 flow |

### Azure App Registration

1. Go to [Azure portal](https://portal.azure.com) → **Azure Active Directory** → **App registrations** → **New registration**.
2. Set redirect URI to `http://127.0.0.1:19002/callback` (type: Web).
3. Under **Certificates & secrets**, create a new client secret.
4. Under **API permissions**, add `User.Read` (delegated). If using group restriction, also add `GroupMember.Read.All` (delegated). Grant admin consent.

### Setup

```bash
# 1. Build the plugin binary
cd plugins/office365-oauth
GOWORK=off go build -o office365-oauth .

# 2. Export credentials
export AZURE_CLIENT_ID=your_application_client_id
export AZURE_CLIENT_SECRET=your_client_secret
export AZURE_TENANT_ID=your_tenant_directory_id
export DASHBOARD_URL=http://127.0.0.1:13120
export DASHBOARD_AUTH_PLUGIN_SECRET=$(openssl rand -hex 32)

# Optional: restrict to a specific Azure AD group
export OFFICE365_ALLOWED_GROUP_ID=your_group_object_id

# 3. Point the dashboard at the plugin dir and start
export PLUGIN_DIR=/path/to/plugins   # directory containing office365-oauth/
./agent-dashboard serve
```

### Endpoint summary

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check — returns `{"ok":true}` |
| `GET` | `/login?nonce=<jwt>` | Start OAuth dance (primary entry point) |
| `GET` | `/callback?code=&state=` | OAuth callback — creates session, redirects to dashboard |
```

- [ ] **Step 2: Commit**

```bash
git add docs/plugin-guide.md
git commit -m "docs: add office365-oauth reference to plugin guide"
```

---

### Task 4: Final verification and PR

- [ ] **Step 1: Clean build**

```bash
cd plugins/office365-oauth && GOWORK=off go build ./... && echo "OK"
task test
```

Expected: `OK`, all tests pass.

- [ ] **Step 2: Create PR**

```bash
git push -u origin feat/office365-oauth-plugin
gh pr create \
  --title "feat: add office365-oauth standalone auth plugin" \
  --base upcoming \
  --body "$(cat <<'EOF'
## Summary
- New `plugins/office365-oauth/` standalone plugin implementing `auth_provider` capability for Microsoft single-tenant Azure AD
- Same flow as `github-oauth`: owns the complete OAuth2 dance, calls `POST /api/auth/session` on core
- Optional group restriction via `OFFICE365_ALLOWED_GROUP_ID` — non-members get HTTP 403 before any session is created
- Pagination-safe group membership check (`@odata.nextLink` loop)

## Test plan
- [ ] Build plugin binary: `cd plugins/office365-oauth && GOWORK=off go build -o office365-oauth .`
- [ ] Health check responds `{"ok":true}` at `http://127.0.0.1:19002/health`
- [ ] Missing env vars exit with clear error messages
- [ ] `task test` passes

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```
