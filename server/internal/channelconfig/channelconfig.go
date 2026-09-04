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

	"github.com/lx-wnk/agent-dashboard/server/internal/mcp"
)

// mcpServerEntry mirrors the claude CLI's mcpServers JSON shape. A stdio
// server sets Command/Args; an HTTP server sets Type/URL/Headers. omitempty on
// every field keeps the stdio entry byte-identical to what this package wrote
// before the HTTP form existed.
type mcpServerEntry struct {
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Type    string            `json:"type,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// mcpConfig holds entries as any so a user-scope server carried over from
// ~/.claude.json can stay the raw JSON it was read as.
type mcpConfig struct {
	MCPServers map[string]any `json:"mcpServers"`
}

// TaskAPI describes the dashboard's own MCP endpoint and the credential the
// spawned agent presents to it. Both fields are required together: an entry
// with a URL and no token would look configured and fail every call 401.
type TaskAPI struct {
	URL   string
	Token string
}

// DiscoveryDir is the subdirectory under $HOME where the channel bridge writes
// its per-PID discovery files. Shared between the server (channelreply) and
// the channel bridge binary to avoid duplicating the path.
const DiscoveryDir = ".claude/dashboard-channel"

// Subcommand names the spawner re-executes on the dashboard binary. Every
// binary that can be re-executed must implement these.
const (
	SubcommandChannel = "channel"
	SubcommandPtyHost = "pty-host"
)

// ChannelServerName is the mcpServers key of the stdio channel bridge every
// spawn gets. Paired with mcp.ServerName (the HTTP task API) it names the two
// entries the dashboard owns.
const ChannelServerName = "dashboard-channel"

// reservedServerNames are the entries the dashboard writes itself. A user-scope
// server registered under one of these names is dropped instead of merged:
// `claude mcp add --scope user` registers dashboard-tasks with the broad,
// long-lived onboarding credential, and letting that through would hand every
// spawned stage agent back the scopes the per-stage-run key deliberately omits.
var reservedServerNames = map[string]bool{ChannelServerName: true, mcp.ServerName: true}

// DiscoveryFile returns the channel-bridge discovery file path for a pid:
// <home>/.claude/dashboard-channel/<pid>.json
func DiscoveryFile(home string, pid int) string {
	return filepath.Join(home, DiscoveryDir, strconv.Itoa(pid)+".json")
}

// DiscoveryPtyFile returns the pty-broker discovery file path for a pid:
// <home>/.claude/dashboard-channel/<pid>.pty.json
func DiscoveryPtyFile(home string, pid int) string {
	return filepath.Join(home, DiscoveryDir, strconv.Itoa(pid)+".pty.json")
}

// buildConfig returns the mcpConfig struct for the given binary path, adding
// the dashboard-tasks HTTP server when taskAPI is non-nil. It errors if
// taskAPI is set but only one of URL/Token is populated. This is the single
// definition of the channel MCP config shape.
func buildConfig(binaryPath string, taskAPI *TaskAPI, userServers map[string]json.RawMessage) (mcpConfig, error) {
	servers := map[string]any{
		ChannelServerName: mcpServerEntry{
			Command: binaryPath,
			Args:    []string{SubcommandChannel},
		},
	}
	if taskAPI != nil {
		if taskAPI.URL == "" || taskAPI.Token == "" {
			return mcpConfig{}, fmt.Errorf("channelconfig: TaskAPI needs both a URL and a token")
		}
		servers[mcp.ServerName] = mcpServerEntry{
			Type:    "http",
			URL:     taskAPI.URL,
			Headers: map[string]string{"Authorization": "Bearer " + taskAPI.Token},
		}
	}
	for name, entry := range userServers {
		if reservedServerNames[name] {
			continue
		}
		servers[name] = entry
	}
	return mcpConfig{MCPServers: servers}, nil
}

// ConfigJSON returns the inline JSON string for the dashboard-channel MCP
// server configuration. The string can be passed directly to claude via
// --mcp-config without writing a file.
//
// Example output:
//
//	{"mcpServers":{"dashboard-channel":{"command":"/path/to/agent-dashboard","args":["channel"]}}}
func ConfigJSON(binaryPath string, taskAPI *TaskAPI) (string, error) {
	cfg, err := buildConfig(binaryPath, taskAPI, nil)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("channelconfig: ConfigJSON marshal: %w", err)
	}
	return string(data), nil
}

// WriteTempConfig writes a temporary MCP config file that tells the claude CLI
// how to start the dashboard-channel MCP server.
//
// binaryPath is the absolute path to the agent-dashboard binary. userServers
// are the operator's own MCP servers (see claudeconfig.UserMCPServers) to carry
// into a spawn launched with --strict-mcp-config; pass nil to write the
// dashboard's servers alone.
//
// The caller is responsible for deleting the returned file path.
func WriteTempConfig(binaryPath string, taskAPI *TaskAPI, userServers map[string]json.RawMessage) (path string, err error) {
	cfg, err := buildConfig(binaryPath, taskAPI, userServers)
	if err != nil {
		return "", err
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
	if err := f.Chmod(0o600); err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("channelconfig: chmod temp file: %w", err)
	}
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
