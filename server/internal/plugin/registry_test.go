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
