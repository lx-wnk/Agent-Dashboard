package tools

import (
	"testing"

	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	"github.com/stretchr/testify/require"
)

func TestRegisterKeyTools_AllToolsPresent(t *testing.T) {
	registry := mcp.ToolRegistry{}
	RegisterKeyTools(registry, KeyDeps{})
	for _, name := range []string{
		"list_api_keys",
		"create_api_key",
		"revoke_api_key",
	} {
		require.Contains(t, registry, name, "expected tool %q to be registered", name)
	}
}
