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
	"sync"
)

var (
	configDirMu       sync.RWMutex
	configDirProvider func() string
)

// SetConfigDirProvider installs fn as the provider for the Claude config
// directory. The returned closure restores the previous provider (test seam).
func SetConfigDirProvider(fn func() string) func() {
	configDirMu.Lock()
	prev := configDirProvider
	configDirProvider = fn
	configDirMu.Unlock()
	return func() {
		configDirMu.Lock()
		configDirProvider = prev
		configDirMu.Unlock()
	}
}

// ConfigDir returns the Claude config base directory.
// Precedence: provider (settings) → CLAUDE_CONFIG_DIR env → ~/.claude.
func ConfigDir() string {
	configDirMu.RLock()
	fn := configDirProvider
	configDirMu.RUnlock()
	if fn != nil {
		if dir := fn(); dir != "" {
			return dir
		}
	}
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude")
}

// JSONPath resolves ~/.claude.json, honoring CLAUDE_CONFIG_DIR — which
// relocates the whole Claude config root, not just the ~/.claude/projects
// tree — so a value here must not be hardcoded to the default home path.
func JSONPath() (string, error) {
	return filepath.Join(ConfigDir(), ".claude.json"), nil
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

// WriteServerEntry writes mcpServers.<name> into Claude's config, leaving
// every other key and the file mode untouched. It refuses a symlinked path.
func WriteServerEntry(name string, entry json.RawMessage) error {
	return modifyServerEntries(func(servers map[string]json.RawMessage) {
		servers[name] = entry
	})
}

// RemoveServerEntry deletes mcpServers.<name>; a missing file or key is not
// an error.
func RemoveServerEntry(name string) error {
	path, err := JSONPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("claudeconfig: stat %s: %w", path, err)
	}
	return modifyServerEntries(func(servers map[string]json.RawMessage) {
		delete(servers, name)
	})
}

// modifyServerEntries loads Claude's config (an absent file behaves as an
// empty object), applies mutate to the mcpServers map, and writes the
// result back atomically without touching any other top-level key.
func modifyServerEntries(mutate func(servers map[string]json.RawMessage)) error {
	path, err := JSONPath()
	if err != nil {
		return err
	}

	mode := os.FileMode(0o600)
	data := []byte("{}")
	// Lstat, not Stat: a symlinked config must be refused, not followed.
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("claudeconfig: refusing to write through symlink %s", path)
		}
		mode = info.Mode().Perm()
		data, err = os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("claudeconfig: read %s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("claudeconfig: stat %s: %w", path, err)
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("claudeconfig: parse %s: %w", path, err)
	}
	// A config holding literal null parses without error and leaves obj nil.
	if obj == nil {
		obj = map[string]json.RawMessage{}
	}

	servers := map[string]json.RawMessage{}
	if raw, ok := obj["mcpServers"]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil {
			return fmt.Errorf("claudeconfig: parse %s mcpServers: %w", path, err)
		}
	}

	mutate(servers)

	encodedServers, err := json.Marshal(servers)
	if err != nil {
		return fmt.Errorf("claudeconfig: encode mcpServers: %w", err)
	}
	obj["mcpServers"] = encodedServers

	encoded, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return fmt.Errorf("claudeconfig: encode %s: %w", path, err)
	}

	return atomicWrite(path, encoded, mode)
}

// atomicWrite replaces path with data: temp file in the target's own
// directory, Sync, Close, Chmod, Rename. The deferred remove is a no-op once
// the rename succeeds — it is what stops a failed rename from leaving a
// stray temp file behind.
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".claudeconfig-*.tmp")
	if err != nil {
		return fmt.Errorf("claudeconfig: create temp file next to %s: %w", path, err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("claudeconfig: write %s: %w", tmp.Name(), err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("claudeconfig: sync %s: %w", tmp.Name(), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("claudeconfig: close %s: %w", tmp.Name(), err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return fmt.Errorf("claudeconfig: chmod %s: %w", tmp.Name(), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("claudeconfig: rename %s to %s: %w", tmp.Name(), path, err)
	}
	return nil
}
