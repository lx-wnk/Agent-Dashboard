package permissions_test

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/permissions"
)

// TestWriteToolNames_ContainsExpectedEntries is the CQ-01 SSOT sync test.
// It verifies that WriteToolNames contains exactly the write-type tools that
// the edit gate is expected to intercept.  If a new write tool is added to
// WriteToolNames, this test must be updated — making the set of gated tools
// explicit and visible in the test suite.
func TestWriteToolNames_ContainsExpectedEntries(t *testing.T) {
	want := map[string]bool{
		"Edit":      true,
		"Write":     true,
		"MultiEdit": true,
	}

	got := make(map[string]bool, len(permissions.WriteToolNames))
	for _, name := range permissions.WriteToolNames {
		got[name] = true
	}

	// Verify every expected tool is present.
	for tool := range want {
		if !got[tool] {
			t.Errorf("WriteToolNames missing expected write tool %q", tool)
		}
	}

	// Verify no extra tools have been silently added.
	for tool := range got {
		if !want[tool] {
			t.Errorf("WriteToolNames contains unexpected tool %q — update this test if intentional", tool)
		}
	}
}

// TestWriteToolNames_NonWriteToolsAreAbsent verifies that commonly used
// non-write tools are not erroneously listed in WriteToolNames.
func TestWriteToolNames_NonWriteToolsAreAbsent(t *testing.T) {
	nonWriteTools := []string{"Bash", "Read", "Glob", "Grep", "LS", "WebFetch", "Task", "Agent"}
	got := make(map[string]bool, len(permissions.WriteToolNames))
	for _, name := range permissions.WriteToolNames {
		got[name] = true
	}
	for _, tool := range nonWriteTools {
		if got[tool] {
			t.Errorf("WriteToolNames must not contain non-write tool %q", tool)
		}
	}
}

// TestIsAllowedTool_KnownToolsAreAllowed verifies the IsAllowedTool function
// returns true for every tool in the default pipeline allow-list.
func TestIsAllowedTool_KnownToolsAreAllowed(t *testing.T) {
	knownAllowed := []string{
		"Read", "Write", "Edit", "MultiEdit", "Glob", "Grep", "LS", "Bash",
		"WebFetch", "WebSearch", "Task", "Agent", "TodoRead", "TodoWrite",
		"NotebookRead", "NotebookEdit",
		"mcp__dashboard-channel__dashboard_reply",
		"mcp__dashboard-channel__request_permission",
	}
	for _, tool := range knownAllowed {
		if !permissions.IsAllowedTool(tool) {
			t.Errorf("IsAllowedTool(%q) = false, want true", tool)
		}
	}
}

// TestIsAllowedTool_UnknownToolIsNotAllowed verifies that arbitrary tool names
// are rejected.
func TestIsAllowedTool_UnknownToolIsNotAllowed(t *testing.T) {
	if permissions.IsAllowedTool("rm -rf /") {
		t.Error("IsAllowedTool(\"rm -rf /\") = true, want false")
	}
	if permissions.IsAllowedTool("") {
		t.Error("IsAllowedTool(\"\") = true, want false")
	}
}
