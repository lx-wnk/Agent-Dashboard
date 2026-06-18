package plugins_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/lx-wnk/agent-dashboard/server/internal/api/plugins"
	"github.com/lx-wnk/agent-dashboard/server/internal/auth"
	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

const testJWTSecret = "test-secret-plugins"

// withAuth adds a valid JWT session cookie so auth.RequireAuth passes.
func withAuth(t *testing.T, r *http.Request) *http.Request {
	t.Helper()
	token, err := auth.SignJWT(auth.JWTPayload{Sub: "user-1", Login: "testuser", IsAdmin: true}, testJWTSecret, 3600)
	if err != nil {
		t.Fatalf("sign jwt: %v", err)
	}
	r.AddCookie(&http.Cookie{Name: "auth_token", Value: token})
	return r
}

// buildFakePlugin starts an httptest server acting as a plugin and writes a
// plugin.json into a subdirectory of dir. Returns the registry (already
// loaded) and the plugin's base URL so callers can assert it is not leaked.
func buildFakePlugin(t *testing.T) (*plugin.Registry, string) {
	t.Helper()

	// Stand up an HTTP server that satisfies the health check.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	addr := srv.Listener.Addr().String()
	baseURL := "http://" + addr

	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "fake-plugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Include env and addr so we can assert neither leaks into the API response.
	desc := plugin.Descriptor{
		ID:           "fake-plugin",
		Version:      "1.0.0",
		Capabilities: []string{plugin.CapRouteExtension},
		Addr:         addr,
		Env:          []string{"SUPER_SECRET_TOKEN"},
	}
	data, err := json.Marshal(desc)
	if err != nil {
		t.Fatalf("marshal descriptor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644); err != nil {
		t.Fatalf("write plugin.json: %v", err)
	}

	reg := plugin.New(dir)
	if err := reg.Load(context.Background(), plugin.Hooks{}); err != nil {
		t.Fatalf("reg.Load: %v", err)
	}

	return reg, baseURL
}

// TestList_ExposesOnlyIDAndCapabilities verifies:
//  1. GET /api/settings/plugins returns 200 for an authenticated request.
//  2. Each JSON element contains EXACTLY the keys "id" and "capabilities" —
//     no "env", "baseURL", "descriptor", or any other field.
//  3. The values match the fake plugin.
//  4. The raw response body does not contain the plugin's baseURL host or the
//     env var name (defense-in-depth string guard for F028/F034).
func TestList_ExposesOnlyIDAndCapabilities(t *testing.T) {
	reg, baseURL := buildFakePlugin(t)

	// Mount handler behind auth middleware, mirroring the production router.
	h := plugins.New(reg)
	r := chi.NewRouter()
	r.Use(auth.RequireAuth(testJWTSecret))
	h.Mount(r)

	req := httptest.NewRequest(http.MethodGet, "/api/settings/plugins", nil)
	req = withAuth(t, req)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	rawBody := rr.Body.String()

	// --- Structural key-set assertion ---
	var items []map[string]any
	if err := json.Unmarshal([]byte(rawBody), &items); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 plugin in response, got %d", len(items))
	}

	item := items[0]

	// Assert exactly the two allowed keys are present.
	for _, required := range []string{"id", "capabilities"} {
		if _, ok := item[required]; !ok {
			t.Errorf("response item missing required key %q", required)
		}
	}

	// Assert no forbidden keys are present.
	for _, forbidden := range []string{"env", "baseURL", "descriptor", "addr", "command", "version"} {
		if _, ok := item[forbidden]; ok {
			t.Errorf("response item must not contain key %q (F028/F034 leak guard)", forbidden)
		}
	}

	// Assert exactly two keys total — catches future additions that aren't yet named above.
	if len(item) != 2 {
		t.Errorf("response item has %d keys, want exactly 2 (id + capabilities); got: %v", len(item), item)
	}

	// --- Value correctness ---
	if item["id"] != "fake-plugin" {
		t.Errorf("id: got %v, want fake-plugin", item["id"])
	}
	caps, _ := item["capabilities"].([]any)
	if len(caps) != 1 || caps[0] != plugin.CapRouteExtension {
		t.Errorf("capabilities: got %v, want [%s]", caps, plugin.CapRouteExtension)
	}

	// --- Defense-in-depth: raw body must not contain the baseURL host or env key name ---
	// The addr host is the httptest server's loopback address; if it appears in
	// the body the handler leaked the internal plugin address (F028 violation).
	host := strings.TrimPrefix(baseURL, "http://")
	if strings.Contains(rawBody, host) {
		t.Errorf("response body contains baseURL host %q — internal plugin address must not be exposed (F028)", host)
	}
	if strings.Contains(rawBody, "SUPER_SECRET_TOKEN") {
		t.Errorf("response body contains env var name SUPER_SECRET_TOKEN — env must not be exposed (F034)")
	}
}
