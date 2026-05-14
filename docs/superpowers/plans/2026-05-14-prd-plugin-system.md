# PR-D: Plugin System + GitHub Auth Extraction Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a sidecar HTTP plugin system. Plugins are discovered from `plugin_dir`, started as child processes, and registered by capability (`auth_provider`, `route_extension`). Extract GitHub OAuth into a standalone plugin as the reference implementation.

**Architecture:** New package `server/internal/plugin/` contains `Registry` (discovery, start, health), `types.go` (descriptor, capability), `proxy.go` (HTTP reverse proxy for `route_extension`). The `auth.OAuthProvider` interface already exists — the registry returns an implementation if an `auth_provider` plugin is loaded. Fallback to bypass-auth if no auth plugin is present.

**Tech Stack:** Go stdlib `net/http`, `os/exec`, `encoding/json`, chi reverse proxy, existing `auth.OAuthProvider` interface

---

## Worktree Setup

```bash
git worktree add ../agent-dashboard-prd feat/plugin-system
cd ../agent-dashboard-prd/server
```

---

## File Map

| Action | File |
|--------|------|
| Create | `server/internal/plugin/types.go` |
| Create | `server/internal/plugin/registry.go` |
| Create | `server/internal/plugin/proxy.go` |
| Create | `server/internal/plugin/registry_test.go` |
| Create | `plugins/github-oauth/main.go` |
| Create | `plugins/github-oauth/plugin.json` |
| Create | `plugins/github-oauth/go.mod` |
| Modify | `server/internal/config/config.go` (remove GitHubClientID/Secret, add PluginDir doc) |
| Modify | `server/cmd/serve/wire.go` (or wherever GitHub auth is wired) |
| Modify | `server/internal/api/router.go` (mount plugin routes) |
| Create | `docs/plugin-guide.md` |

---

### Task 1: Plugin types

**Files:**
- Create: `server/internal/plugin/types.go`

- [ ] **Step 1.1: Write `types.go`**

```go
// Package plugin provides runtime plugin discovery and lifecycle management.
package plugin

// Descriptor is read from plugin.json in each plugin directory.
type Descriptor struct {
	ID           string   `json:"id"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
	// Addr is the HTTP address the plugin listens on (e.g. "127.0.0.1:13200").
	Addr    string   `json:"addr"`
	// Command is the executable + args to start the plugin process.
	// If empty, the plugin is expected to already be running.
	Command []string `json:"command"`
	// Env lists env var names the plugin reads from the parent environment.
	Env     []string `json:"env"`
}

// HasCapability reports whether the plugin declares the given capability.
func (d Descriptor) HasCapability(cap string) bool {
	for _, c := range d.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// Capability constants used in plugin.json.
const (
	CapAuthProvider   = "auth_provider"
	CapRouteExtension = "route_extension"
)
```

- [ ] **Step 1.2: Run build check**

```bash
go build ./internal/plugin/
```

Expected: no errors.

---

### Task 2: Plugin registry

**Files:**
- Create: `server/internal/plugin/registry.go`

- [ ] **Step 2.1: Write `registry.go`**

```go
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Registry discovers, starts, and health-checks plugins from a directory.
type Registry struct {
	dir     string
	plugins []Entry
}

// Entry is a loaded plugin with its descriptor and running process (if started by us).
type Entry struct {
	Descriptor Descriptor
	cmd        *exec.Cmd
	BaseURL    string // http://{addr}
}

// New creates a Registry that will discover plugins in dir.
// If dir is empty, the registry does nothing (no plugins).
func New(dir string) *Registry {
	return &Registry{dir: dir}
}

// Load scans dir, starts each plugin process, and waits for health.
// Call once during server startup. ctx is used for health-check timeouts.
func (r *Registry) Load(ctx context.Context) error {
	if r.dir == "" {
		return nil
	}
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no plugin dir is fine
		}
		return fmt.Errorf("plugin: read dir %s: %w", r.dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		descPath := filepath.Join(r.dir, entry.Name(), "plugin.json")
		data, err := os.ReadFile(descPath)
		if err != nil {
			slog.Warn("plugin: skip — no plugin.json", "dir", entry.Name())
			continue
		}
		var desc Descriptor
		if err := json.Unmarshal(data, &desc); err != nil {
			slog.Warn("plugin: skip — invalid plugin.json", "dir", entry.Name(), "err", err)
			continue
		}
		pluginEntry := Entry{
			Descriptor: desc,
			BaseURL:    "http://" + desc.Addr,
		}
		if len(desc.Command) > 0 {
			cmd := exec.CommandContext(ctx, desc.Command[0], desc.Command[1:]...)
			cmd.Dir = filepath.Join(r.dir, entry.Name())
			cmd.Env = os.Environ() // forward full env so GITHUB_CLIENT_ID etc. are available
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				slog.Error("plugin: failed to start", "id", desc.ID, "err", err)
				continue
			}
			pluginEntry.cmd = cmd
		}
		if err := r.waitHealthy(ctx, pluginEntry.BaseURL); err != nil {
			slog.Error("plugin: health check failed", "id", desc.ID, "err", err)
			if pluginEntry.cmd != nil {
				_ = pluginEntry.cmd.Process.Kill()
			}
			continue
		}
		slog.Info("plugin: loaded", "id", desc.ID, "capabilities", desc.Capabilities)
		r.plugins = append(r.plugins, pluginEntry)
	}
	return nil
}

// Shutdown stops all plugin processes that were started by Load.
func (r *Registry) Shutdown() {
	for _, p := range r.plugins {
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	}
}

// FindByCapability returns the first plugin with the given capability, or nil.
func (r *Registry) FindByCapability(cap string) *Entry {
	for i := range r.plugins {
		if r.plugins[i].Descriptor.HasCapability(cap) {
			return &r.plugins[i]
		}
	}
	return nil
}

// AllWithCapability returns all plugins with the given capability.
func (r *Registry) AllWithCapability(cap string) []Entry {
	var out []Entry
	for _, p := range r.plugins {
		if p.Descriptor.HasCapability(cap) {
			out = append(out, p)
		}
	}
	return out
}

func (r *Registry) waitHealthy(ctx context.Context, baseURL string) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/health", nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return nil
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("plugin at %s did not become healthy within 5s", baseURL)
}
```

- [ ] **Step 2.2: Build check**

```bash
go build ./internal/plugin/
```

---

### Task 3: Route-extension proxy

**Files:**
- Create: `server/internal/plugin/proxy.go`

- [ ] **Step 3.1: Write `proxy.go`**

```go
package plugin

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// NewReverseProxy returns an http.Handler that proxies requests to the plugin's addr.
// The incoming path prefix /api/plugins/{id} is stripped before proxying.
func NewReverseProxy(entry Entry) http.Handler {
	target, _ := url.Parse(entry.BaseURL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	return proxy
}
```

- [ ] **Step 3.2: Build check**

```bash
go build ./internal/plugin/
```

---

### Task 4: Registry tests

**Files:**
- Create: `server/internal/plugin/registry_test.go`

- [ ] **Step 4.1: Write tests**

```go
package plugin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

func TestRegistry_EmptyDir_LoadsNothing(t *testing.T) {
	dir := t.TempDir()
	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background()))
	assert.Nil(t, r.FindByCapability(plugin.CapAuthProvider))
}

func TestRegistry_NonexistentDir_NoError(t *testing.T) {
	r := plugin.New("/does/not/exist")
	require.NoError(t, r.Load(context.Background()))
}

func TestRegistry_EmptyPluginDir_Skipped(t *testing.T) {
	r := plugin.New("")
	require.NoError(t, r.Load(context.Background()))
}

func TestRegistry_PluginWithHealthy_Loaded(t *testing.T) {
	// Start a real HTTP server acting as a plugin.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Extract host:port from srv.URL
	addr := srv.Listener.Addr().String()

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "test-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))

	desc := plugin.Descriptor{
		ID:           "test-plugin",
		Version:      "1.0.0",
		Capabilities: []string{plugin.CapRouteExtension},
		Addr:         addr,
		// No Command — server already running
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background()))

	entry := r.FindByCapability(plugin.CapRouteExtension)
	require.NotNil(t, entry)
	assert.Equal(t, "test-plugin", entry.Descriptor.ID)
}
```

- [ ] **Step 4.2: Run**

```bash
go test -race ./internal/plugin/ -v
```

Expected: all tests `PASS`.

- [ ] **Step 4.3: Commit plugin package**

```bash
git add server/internal/plugin/
git commit -m "feat: plugin package — registry, types, reverse proxy"
```

---

### Task 5: auth_provider plugin bridge

- [ ] **Step 5.1: Create the plugin auth adapter**

Create `server/internal/plugin/auth_adapter.go`:

```go
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
	resp, err := http.Get(p.entry.BaseURL + "/capabilities/auth/authorize-url?" + q.Encode())
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var out struct{ URL string `json:"url"` }
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
	var out struct{ Token string `json:"token"` }
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
```

- [ ] **Step 5.2: Build check**

```bash
go build ./internal/plugin/
```

- [ ] **Step 5.3: Commit**

```bash
git add server/internal/plugin/auth_adapter.go
git commit -m "feat: PluginAuthProvider bridges auth_provider plugin to OAuthProvider interface"
```

---

### Task 6: Wire plugin registry into server startup

- [ ] **Step 6.1: Find the wire/composition root**

```bash
ls server/cmd/serve/
```

Read `wire.go` or whichever file constructs `OAuthProvider` and passes it to `NewRouter`.

- [ ] **Step 6.2: Add plugin registry to startup**

In `server/cmd/serve/wire.go` (or the equivalent composition root), after `cfg` is loaded:

```go
// Load plugins from configured plugin_dir.
pluginRegistry := plugin.New(cfg.PluginDir)
if err := pluginRegistry.Load(ctx); err != nil {
    slog.Warn("plugin registry: load failed", "err", err)
}
defer pluginRegistry.Shutdown()

// Wire auth provider: prefer plugin, fall back to bypass-auth.
var oauthProvider auth.OAuthProvider
if entry := pluginRegistry.FindByCapability(plugin.CapAuthProvider); entry != nil {
    oauthProvider = plugin.NewAuthProvider(*entry)
    slog.Info("auth: using plugin provider", "plugin", entry.Descriptor.ID)
} else {
    slog.Info("auth: no auth_provider plugin found — bypass-auth active for loopback")
}
```

Pass `oauthProvider` (may be nil) to `RouterDeps.OAuthProvider`.

- [ ] **Step 6.3: Mount route_extension plugins on the router**

In `server/internal/api/router.go`, after existing routes, add:

```go
// Mount route_extension plugins (if any).
if deps.PluginRegistry != nil {
    for _, entry := range deps.PluginRegistry.AllWithCapability(plugin.CapRouteExtension) {
        id := entry.Descriptor.ID
        r.Mount("/api/plugins/"+id, plugin.NewReverseProxy(entry))
        slog.Info("router: mounted plugin route", "id", id, "prefix", "/api/plugins/"+id)
    }
}
```

Add `PluginRegistry *plugin.Registry` field to `RouterDeps`.

- [ ] **Step 6.4: Remove GitHub client ID/Secret from Config**

In `server/internal/config/config.go`, remove:
```go
GitHubClientID         string `koanf:"github_client_id"`
GitHubClientSecret     string `koanf:"github_client_secret"`
```

Remove from the `CallbackURL` method if it references them. The callback URL is now plugin-handled.

- [ ] **Step 6.5: Build check**

```bash
go build ./...
```

Fix any compilation errors. The key change is `OAuthProvider` is now nil when no plugin is loaded, and the router's `BypassAuth` logic handles that case (existing behaviour).

- [ ] **Step 6.6: Run tests**

```bash
go test -race ./...
```

- [ ] **Step 6.7: Commit**

```bash
git add server/
git commit -m "feat: wire plugin registry into server startup, remove hardcoded GitHub config"
```

---

### Task 7: GitHub OAuth reference plugin

**Files:**
- Create: `plugins/github-oauth/main.go`
- Create: `plugins/github-oauth/plugin.json`
- Create: `plugins/github-oauth/go.mod`

- [ ] **Step 7.1: Create `plugin.json`**

```json
{
  "id": "github-oauth",
  "version": "1.0.0",
  "capabilities": ["auth_provider"],
  "addr": "127.0.0.1:13201",
  "command": ["./github-oauth"],
  "env": ["GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET"]
}
```

- [ ] **Step 7.2: Create `go.mod`**

```
module github.com/lx-wnk/agent-dashboard/plugins/github-oauth

go 1.22
```

- [ ] **Step 7.3: Create `main.go`** — self-contained HTTP server implementing the `auth_provider` contract:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	tokenURL = "https://github.com/login/oauth/access_token"
	userURL  = "https://api.github.com/user"
	authURL  = "https://github.com/login/oauth/authorize"
	addr     = "127.0.0.1:13201"
)

func main() {
	clientID := os.Getenv("GITHUB_CLIENT_ID")
	clientSecret := os.Getenv("GITHUB_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" {
		slog.Error("GITHUB_CLIENT_ID and GITHUB_CLIENT_SECRET must be set")
		os.Exit(1)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/capabilities/auth/authorize-url", func(w http.ResponseWriter, r *http.Request) {
		state := r.URL.Query().Get("state")
		redirectURI := r.URL.Query().Get("redirect_uri")
		q := url.Values{
			"client_id":    {clientID},
			"redirect_uri": {redirectURI},
			"state":        {state},
			"scope":        {"read:user"},
		}
		authRedirect := authURL + "?" + q.Encode()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"url": authRedirect})
	})

	mux.HandleFunc("/capabilities/auth/exchange", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Code        string `json:"code"`
			RedirectURI string `json:"redirect_uri"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		token, err := exchangeCode(clientID, clientSecret, body.Code, body.RedirectURI)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"token": token})
	})

	mux.HandleFunc("/capabilities/auth/user", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		profile, err := fetchUser(r.Context(), token)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(profile)
	})

	slog.Info("github-oauth plugin listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("server error", "err", err)
		os.Exit(1)
	}
}

func exchangeCode(clientID, clientSecret, code, redirectURI string) (string, error) {
	params := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}
	req, _ := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(params.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != "" {
		return "", fmt.Errorf("github: %s", result.Error)
	}
	return result.AccessToken, nil
}

func fetchUser(ctx context.Context, token string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var profile map[string]any
	if err := json.Unmarshal(body, &profile); err != nil {
		return nil, err
	}
	// Normalise to OAuthUserProfile shape expected by the server's auth_adapter
	return map[string]any{
		"ID":          fmt.Sprintf("%v", profile["id"]),
		"Login":       profile["login"],
		"DisplayName": profile["name"],
		"AvatarURL":   profile["avatar_url"],
	}, nil
}
```

- [ ] **Step 7.4: Build the plugin**

```bash
cd plugins/github-oauth && go build -o github-oauth .
```

Expected: binary `plugins/github-oauth/github-oauth` created.

- [ ] **Step 7.5: Commit**

```bash
git add plugins/
git commit -m "feat: github-oauth reference plugin implementing auth_provider contract"
```

---

### Task 8: Plugin guide

**Files:**
- Create: `docs/plugin-guide.md`

- [ ] **Step 8.1: Write the plugin guide**

The guide must cover:
1. **What is a plugin?** — A standalone HTTP process exposing capability endpoints
2. **`plugin.json` reference** — all fields with types and descriptions
3. **Capability contracts** — `auth_provider` and `route_extension` endpoint specs
4. **Security model** — plugins MUST bind to `127.0.0.1` only; server only calls local addresses
5. **Step-by-step: build a plugin in any language** — using the HTTP contract
6. **Reference implementation** — `plugins/github-oauth/` walkthrough
7. **Config** — set `DASHBOARD_PLUGIN_DIR=/path/to/plugins` to activate

- [ ] **Step 8.2: Commit and push**

```bash
git add docs/plugin-guide.md
git commit -m "docs: add plugin guide with auth_provider contract and github-oauth walkthrough"
git push -u origin feat/plugin-system
```

---

### Task 9: Final verification

- [ ] **Step 9.1: Full test suite**

```bash
cd server && go test -race ./...
```

- [ ] **Step 9.2: Build all binaries**

```bash
task build
cd plugins/github-oauth && go build -o github-oauth .
```

Expected: both succeed.
