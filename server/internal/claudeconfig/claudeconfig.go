// Package claudeconfig reads the Claude CLI's own ~/.claude.json — the file
// `claude mcp add --scope user` writes — so the dashboard can tell which MCP
// servers the operator registered there.
package claudeconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// JSONPath resolves ~/.claude.json, honoring CLAUDE_CONFIG_DIR — which
// relocates the whole Claude config root, not just the ~/.claude/projects
// tree — so a value here must not be hardcoded to the default home path.
func JSONPath() (string, error) {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, ".claude.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("claudeconfig: resolve home: %w", err)
	}
	return filepath.Join(home, ".claude.json"), nil
}

type configFile struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// UserMCPServers returns the user-scope mcpServers block of ~/.claude.json,
// entries left as raw JSON because the CLI may extend an entry's shape.
//
// An absent file is not an error: it means no server was ever registered. An
// unreadable or malformed one returns a nil map together with the error, and
// callers are expected to carry on without it — neither an onboarding status
// call nor a pipeline spawn may fail over a file the dashboard does not own.
func UserMCPServers() (map[string]json.RawMessage, error) {
	path, err := JSONPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claudeconfig: read %s: %w", path, err)
	}
	var cfg configFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("claudeconfig: parse %s: %w", path, err)
	}
	return cfg.MCPServers, nil
}
