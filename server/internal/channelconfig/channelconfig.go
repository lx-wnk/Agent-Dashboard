// Package channelconfig builds the MCP config file that claude CLI reads to discover
// the dashboard-channel MCP server. The config points to the `agent-dashboard channel`
// binary (same executable, "channel" subcommand) so Claude Code spawns it via stdio.
package channelconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// mcpServerEntry mirrors the claude CLI's mcpServers JSON shape.
type mcpServerEntry struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

type mcpConfig struct {
	MCPServers map[string]mcpServerEntry `json:"mcpServers"`
}

// WriteTempConfig writes a temporary MCP config file that tells the claude CLI
// how to start the dashboard-channel MCP server.
//
// binaryPath is the absolute path to the agent-dashboard binary.
// The caller is responsible for deleting the returned file path.
func WriteTempConfig(binaryPath string) (path string, err error) {
	cfg := mcpConfig{
		MCPServers: map[string]mcpServerEntry{
			"dashboard-channel": {
				Command: binaryPath,
				Args:    []string{"channel"},
			},
		},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("channelconfig: marshal: %w", err)
	}
	// Write to a per-user 0700 directory to prevent world-readable exposure on Linux /tmp.
	dir := filepath.Join(os.TempDir(), "dashboard-"+strconv.Itoa(os.Getuid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("channelconfig: mkdir: %w", err)
	}
	f, err := os.CreateTemp(dir, "dashboard-channel-mcp-*.json")
	if err != nil {
		return "", fmt.Errorf("channelconfig: create temp file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("channelconfig: write temp file: %w", err)
	}
	return f.Name(), nil
}

// SelfBinaryPath returns the absolute path of the currently running binary.
// Used by the spawner to locate the agent-dashboard binary for the channel config.
func SelfBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("channelconfig: Executable: %w", err)
	}
	abs, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("channelconfig: EvalSymlinks: %w", err)
	}
	return abs, nil
}
