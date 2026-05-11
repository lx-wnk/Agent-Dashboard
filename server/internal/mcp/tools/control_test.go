package tools

import (
	"testing"

	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
	"github.com/stretchr/testify/require"
)

func TestRegisterControlTools_AllToolsPresent(t *testing.T) {
	registry := mcp.ToolRegistry{}
	RegisterControlTools(registry, ControlDeps{})
	for _, name := range []string{
		"progress_task",
		"cancel_task",
		"retry_task",
		"grant_permission",
		"resolve_permission_request",
	} {
		require.Contains(t, registry, name, "expected tool %q to be registered", name)
	}
}
