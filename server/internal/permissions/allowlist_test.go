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

// ---------------------------------------------------------------------------
// ValidateGrantEntryWithOverride
// ---------------------------------------------------------------------------

func TestValidateGrantEntryWithOverride_OverrideAcceptsBlockedBashPatterns(t *testing.T) {
	cases := []struct {
		pattern string
		desc    string
	}{
		{"chmod +x ./x.sh", "chmod (not in allow-list)"},
		{"sha256sum file", "sha256sum (not in allow-list)"},
		{"curl https://api.github.com/x", "curl (explicitly blocked)"},
		{"rm ./build/*", "rm (not in allow-list)"},
		{"pnpm test && echo done", "AND-chain construct"},
		{"go build; echo done", "semicolon-chain construct"},
	}
	for _, tc := range cases {
		if err := permissions.ValidateGrantEntryWithOverride("Bash", tc.pattern, true); err != nil {
			t.Errorf("ValidateGrantEntryWithOverride(Bash, %q, override=true) [%s] = %v, want nil", tc.pattern, tc.desc, err)
		}
	}
}

func TestValidateGrantEntryWithOverride_NoOverrideRejectsBlockedBashPatterns(t *testing.T) {
	cases := []struct {
		pattern string
		desc    string
	}{
		{"chmod +x ./x.sh", "chmod"},
		{"curl https://api.github.com/x", "curl explicitly blocked"},
		{"rm ./build/*", "rm not in allow-list"},
		{"pnpm test && echo done", "AND-chain"},
	}
	for _, tc := range cases {
		if err := permissions.ValidateGrantEntryWithOverride("Bash", tc.pattern, false); err == nil {
			t.Errorf("ValidateGrantEntryWithOverride(Bash, %q, override=false) [%s] = nil, want error", tc.pattern, tc.desc)
		}
	}
}

func TestValidateGrantEntryWithOverride_WebFetchRequiresDomainEvenWithOverride(t *testing.T) {
	if err := permissions.ValidateGrantEntryWithOverride("WebFetch", "", true); err == nil {
		t.Error("ValidateGrantEntryWithOverride(WebFetch, empty, override=true) = nil, want error")
	}
	if err := permissions.ValidateGrantEntryWithOverride("WebFetch", "   ", true); err == nil {
		t.Error("ValidateGrantEntryWithOverride(WebFetch, whitespace, override=true) = nil, want error")
	}
}

func TestValidateGrantEntryWithOverride_EmptyBashPatternRejectedEvenWithOverride(t *testing.T) {
	if err := permissions.ValidateGrantEntryWithOverride("Bash", "", true); err == nil {
		t.Error("ValidateGrantEntryWithOverride(Bash, empty, override=true) = nil, want error")
	}
}

func TestValidateGrantEntryWithOverride_UnknownToolRejectedEvenWithOverride(t *testing.T) {
	if err := permissions.ValidateGrantEntryWithOverride("rm", "/etc/shadow", true); err == nil {
		t.Error("ValidateGrantEntryWithOverride(rm, ..., override=true) = nil, want error")
	}
}

func TestValidateGrantEntry_DelegatesToNoOverride(t *testing.T) {
	// ValidateGrantEntry must behave identically to ValidateGrantEntryWithOverride(override=false).
	cases := []struct {
		tool    string
		pattern string
		wantErr bool
	}{
		{"Bash", "go build ./...", false},
		{"Bash", "curl https://evil.com", true},
		{"Bash", "", true},
		{"WebFetch", "https://docs.example.com*", false},
		{"WebFetch", "", true},
		{"Read", "", false},
	}
	for _, tc := range cases {
		err1 := permissions.ValidateGrantEntry(tc.tool, tc.pattern)
		err2 := permissions.ValidateGrantEntryWithOverride(tc.tool, tc.pattern, false)
		if (err1 == nil) != (err2 == nil) {
			t.Errorf("ValidateGrantEntry(%q,%q) and ValidateGrantEntryWithOverride(...,false) disagree: %v vs %v",
				tc.tool, tc.pattern, err1, err2)
		}
	}
}
