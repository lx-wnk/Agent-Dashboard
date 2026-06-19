package parser_test

import (
	"os"
	"path/filepath"
	"testing"

	sdk "github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
)

// TestAllAgentConfigDirs_MissingDirs verifies that config directories absent
// from disk are silently skipped: every returned path must exist, and a
// non-existent CODEX_HOME must not produce a Codex entry.
func TestAllAgentConfigDirs_MissingDirs(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	t.Setenv("CODEX_HOME", missing)

	dirs := parser.AllAgentConfigDirs()
	for _, d := range dirs {
		info, err := os.Stat(d.Path)
		if err != nil || !info.IsDir() {
			t.Errorf("AllAgentConfigDirs returned non-existent path %q (provider %s)", d.Path, d.Provider)
		}
		if d.Provider == sdk.ProviderCodex && d.Path == missing {
			t.Errorf("non-existent CODEX_HOME %q must not be returned", missing)
		}
	}
}
