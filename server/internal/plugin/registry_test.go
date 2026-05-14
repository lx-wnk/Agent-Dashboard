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
	require.NoError(t, r.Load(context.Background(), context.Background()))
	assert.Nil(t, r.FindByCapability(plugin.CapAuthProvider))
}

func TestRegistry_NonexistentDir_NoError(t *testing.T) {
	r := plugin.New("/does/not/exist")
	require.NoError(t, r.Load(context.Background(), context.Background()))
}

func TestRegistry_EmptyPluginDir_Skipped(t *testing.T) {
	r := plugin.New("")
	require.NoError(t, r.Load(context.Background(), context.Background()))
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
	require.NoError(t, r.Load(context.Background(), context.Background()))

	entry := r.FindByCapability(plugin.CapRouteExtension)
	require.NotNil(t, entry)
	assert.Equal(t, "test-plugin", entry.Descriptor.ID)
}
