package channelconfig_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
	"github.com/stretchr/testify/require"
)

func TestWriteTempConfig_ReturnsFilePath(t *testing.T) {
	binaryPath := "/usr/local/bin/agent-dashboard"
	path, err := channelconfig.WriteTempConfig(binaryPath)
	require.NoError(t, err)
	require.NotEmpty(t, path)
	t.Cleanup(func() { _ = os.Remove(path) })
}

func TestWriteTempConfig_FileExists(t *testing.T) {
	binaryPath := "/usr/local/bin/agent-dashboard"
	path, err := channelconfig.WriteTempConfig(binaryPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })

	_, statErr := os.Stat(path)
	require.NoError(t, statErr, "returned file path must exist on disk")
}

func TestWriteTempConfig_FilePermissions(t *testing.T) {
	binaryPath := "/usr/local/bin/agent-dashboard"
	path, err := channelconfig.WriteTempConfig(binaryPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })

	info, err := os.Stat(path)
	require.NoError(t, err)
	// File must not be world-readable (permissions & 0o077 == 0).
	mode := info.Mode().Perm()
	require.Equal(t, os.FileMode(0), mode&0o077,
		"file must not be group- or world-readable, got %o", mode)
}

func TestWriteTempConfig_JSONContract(t *testing.T) {
	binaryPath := "/usr/local/bin/agent-dashboard"
	path, err := channelconfig.WriteTempConfig(binaryPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	require.NoError(t, json.Unmarshal(data, &cfg), "file must contain valid JSON")

	entry, ok := cfg.MCPServers["dashboard-channel"]
	require.True(t, ok, "mcpServers must contain 'dashboard-channel' key")
	require.Equal(t, binaryPath, entry.Command,
		"dashboard-channel.command must equal the provided binary path")
}

func TestWriteTempConfig_MultipleCalls(t *testing.T) {
	// Each call must produce a distinct temp file (no collisions).
	binaryPath := "/usr/local/bin/agent-dashboard"
	path1, err := channelconfig.WriteTempConfig(binaryPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path1) })

	path2, err := channelconfig.WriteTempConfig(binaryPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path2) })

	require.NotEqual(t, path1, path2, "successive calls must return distinct file paths")
}
