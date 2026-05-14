package tools

import (
	"testing"

	mcp "github.com/lx-wnk/agent-dashboard/server/internal/mcp"
)

func TestRegisterWriteTools_AllToolsPresent(t *testing.T) {
	registry := mcp.ToolRegistry{}
	RegisterWriteTools(registry, WriteDeps{})
	for _, name := range []string{
		"create_task",
		"update_task",
		"delete_task",
		"manage_task",
		"add_dependency",
		"remove_dependency",
	} {
		if _, ok := registry[name]; !ok {
			t.Errorf("expected tool %q to be registered, but it was not", name)
		}
	}
}
