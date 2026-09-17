package mcpapps_test

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/mcpapps"
)

func TestIsApplicationTool(t *testing.T) {
	cases := map[string]bool{
		"mcp__mail__imap_search_emails":           true,
		"mcp__obsidian__obsidian_read_note":       true,
		"mcp__dashboard-channel__dashboard_reply": false,
		"mcp__dashboard-tasks__list_tasks":        false,
		"Bash":                                    false,
		"mcp__":                                   false,
		"mcp__mail":                               false,
	}
	for tool, want := range cases {
		if got := mcpapps.IsApplicationTool(tool); got != want {
			t.Errorf("IsApplicationTool(%q) = %v, want %v", tool, got, want)
		}
	}
}
