package plugin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/agent-dashboard/server/internal/plugin"
)

func TestRegistry_EmptyDir_LoadsNothing(t *testing.T) {
	dir := t.TempDir()
	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	assert.Nil(t, r.FindByCapability(plugin.CapAuthProvider))
}

func TestRegistry_NonexistentDir_NoError(t *testing.T) {
	r := plugin.New("/does/not/exist")
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
}

func TestRegistry_EmptyPluginDir_Skipped(t *testing.T) {
	r := plugin.New("")
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
}

func TestRegistry_InvalidPluginID_Uppercase_Skipped(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "my-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))

	desc := plugin.Descriptor{
		ID:           "MY-PLUGIN",
		Version:      "1.0.0",
		Capabilities: []string{plugin.CapRouteExtension},
		Addr:         "127.0.0.1:19001",
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	require.Nil(t, r.FindByCapability(plugin.CapRouteExtension))
}

func TestRegistry_InvalidPluginID_WithSpace_Skipped(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "my-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))

	desc := plugin.Descriptor{
		ID:           "my plugin",
		Version:      "1.0.0",
		Capabilities: []string{plugin.CapRouteExtension},
		Addr:         "127.0.0.1:19001",
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	require.Nil(t, r.FindByCapability(plugin.CapRouteExtension))
}

func TestRegistry_NonLoopbackIP_Skipped(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "valid-plugin")
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))

	desc := plugin.Descriptor{
		ID:           "valid-plugin",
		Version:      "1.0.0",
		Capabilities: []string{plugin.CapRouteExtension},
		Addr:         "192.168.1.100:8080",
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	require.Nil(t, r.FindByCapability(plugin.CapRouteExtension))
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
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))

	entry := r.FindByCapability(plugin.CapRouteExtension)
	require.NotNil(t, entry)
	assert.Equal(t, "test-plugin", entry.Descriptor.ID)
}

func writePluginJSON(t *testing.T, dir, id string, caps []string) {
	t.Helper()
	pluginDir := filepath.Join(dir, id)
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	desc := plugin.Descriptor{
		ID:           id,
		Version:      "1.0.0",
		Capabilities: caps,
		Addr:         "127.0.0.1:0",
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))
}

// startHealthStub starts an in-process HTTP server on 127.0.0.1 that returns
// 200 on /health, registers cleanup, and returns it for its address.
func startHealthStub(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// writePluginJSONAddr writes a plugin.json with the given addr and NO Command
// field, so the registry only health-probes and never spawns a subprocess.
func writePluginJSONAddr(t *testing.T, dir, id string, caps []string, addr string) {
	t.Helper()
	pluginDir := filepath.Join(dir, id)
	require.NoError(t, os.MkdirAll(pluginDir, 0o755))
	desc := plugin.Descriptor{
		ID:           id,
		Version:      "1.0.0",
		Capabilities: caps,
		Addr:         addr,
	}
	data, _ := json.Marshal(desc)
	require.NoError(t, os.WriteFile(filepath.Join(pluginDir, "plugin.json"), data, 0o644))
}

func TestRegistry_StartOneStopOne(t *testing.T) {
	dir := t.TempDir()
	ts := startHealthStub(t)
	addr := strings.TrimPrefix(ts.URL, "http://")
	writePluginJSONAddr(t, dir, "live-plugin", []string{plugin.CapRouteExtension}, addr)

	r := plugin.New(dir)
	r.SetEnabled(func(string) bool { return false })
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	require.Nil(t, r.FindByCapability(plugin.CapRouteExtension))

	require.NoError(t, r.StartOne(context.Background(), "live-plugin"))
	require.NotNil(t, r.FindByCapability(plugin.CapRouteExtension))

	// Idempotent start.
	require.NoError(t, r.StartOne(context.Background(), "live-plugin"))
	require.NotNil(t, r.FindByCapability(plugin.CapRouteExtension))

	require.NoError(t, r.StopOne("live-plugin"))
	require.Nil(t, r.FindByCapability(plugin.CapRouteExtension))

	// Idempotent stop.
	require.NoError(t, r.StopOne("live-plugin"))
	require.Nil(t, r.FindByCapability(plugin.CapRouteExtension))
}

// writeHealthyPlugin creates a plugin dir under dir/{id}/plugin.json backed by
// a real in-process HTTP server that returns 200 on every path including /health.
// Reused by Tasks 2, 5, 6, 7, 8.
func writeHealthyPlugin(t *testing.T, dir, id string) {
	t.Helper()
	ts := startHealthStub(t)
	addr := strings.TrimPrefix(ts.URL, "http://")
	writePluginJSONAddr(t, dir, id, []string{plugin.CapRouteExtension}, addr)
}

func TestStartedPluginIsHealthy(t *testing.T) {
	dir := t.TempDir()
	writeHealthyPlugin(t, dir, "alive")

	r := plugin.New(dir)
	require.NoError(t, r.Load(context.Background(), plugin.Hooks{}))
	t.Cleanup(r.Shutdown)

	got, ok := r.Lookup("alive")
	require.True(t, ok)
	require.True(t, got.Healthy())
}

func TestLookupReturnsHealthyEntryCopy(t *testing.T) {
	r := plugin.New("")
	require.False(t, exists(r, "missing"))

	// Inject a running, healthy entry through the exported test seam.
	r.InjectEntryForTest(plugin.Descriptor{ID: "voice", Addr: "127.0.0.1:19010"}, true)

	got, ok := r.Lookup("voice")
	require.True(t, ok)
	require.Equal(t, "voice", got.Descriptor.ID)
	require.True(t, got.Healthy())

	_, ok = r.Lookup("nope")
	require.False(t, ok)
}

func exists(r *plugin.Registry, id string) bool {
	_, ok := r.Lookup(id)
	return ok
}

func TestRegistry_LoadSkipsDisabled(t *testing.T) {
	dir := t.TempDir()
	writePluginJSON(t, dir, "enabled-one", []string{plugin.CapRouteExtension})
	writePluginJSON(t, dir, "disabled-auth", []string{plugin.CapAuthProvider})

	r := plugin.New(dir)
	r.SetEnabled(func(id string) bool { return id == "enabled-one" })

	// Neither plugin actually starts (no Command/health), but the capability
	// recording must reflect ONLY enabled plugins.
	_ = r.Load(context.Background(), plugin.Hooks{})

	assert.False(t, r.HasAttemptedCapability(plugin.CapAuthProvider),
		"disabled auth_provider plugin must not be recorded as attempted")
}
