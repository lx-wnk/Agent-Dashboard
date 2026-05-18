// Package permissions defines the domain constants for the pipeline permission model.
package permissions

import "regexp"

// DangerousBashRE matches shell patterns that must never appear in a Bash allow-list entry.
// Referenced by both the pipeline spawner (spawn-time) and the REST grant endpoint (grant-time).
var DangerousBashRE = regexp.MustCompile(
	"(?i)(curl\\b|wget\\b|\\bnc\\b|\\bncat\\b|\\bnetcat\\b|bash\\s+-c|sh\\s+-c|\\beval\\b" +
		// SEC-01: match versioned Python/Node/Perl/Ruby interpreters (python3, python3.11, node, nodejs, perl5, ruby3)
		"|python[23]?(?:\\.\\d+)?\\s+-c|node(?:js)?\\s+-e|perl\\d*\\s+-e|ruby\\d*\\s+-e" +
		"|base64\\s+-d|\\$\\(|`|&&|\\||;\\s*\\w" +
		// SEC-02: block redirects to both relative names (>foo) and absolute paths (>/tmp/evil)
		"|>\\s*[\\w/]|<\\s*[\\w/]" +
		"|chmod\\s+\\+x|rm\\s+-rf|exec\\s+\\w|\\bxargs\\b|find\\s+.*-exec)",
)

// IsAllowedTool reports whether name is in the pipeline tool allow-list.
// Use this instead of accessing allowedToolNames directly so the set
// cannot be mutated by other packages.
func IsAllowedTool(name string) bool { return allowedToolNames[name] }

// writeToolNames is the unexported set of tools that mutate file contents.
// External packages must use IsWriteTool to prevent mutation of the map.
var writeToolNames = map[string]bool{
	"Write":     true,
	"Edit":      true,
	"MultiEdit": true,
}

// IsWriteTool reports whether name is in the edit-gate write tool set.
// Use this instead of accessing writeToolNames directly so the set cannot
// be mutated by other packages.
func IsWriteTool(name string) bool { return writeToolNames[name] }

// allowedToolNames is the unexported source of truth for grantable tools.
// All callers must go through IsAllowedTool.
var allowedToolNames = map[string]bool{
	"Read":         true,
	"Write":        true,
	"Edit":         true,
	"MultiEdit":    true,
	"Glob":         true,
	"Grep":         true,
	"LS":           true,
	"Bash":         true,
	"WebFetch":     true,
	"WebSearch":    true,
	"Task":         true,
	"Agent":        true,
	"TodoRead":     true,
	"TodoWrite":    true,
	"NotebookRead": true,
	"NotebookEdit": true,
	"mcp__dashboard-channel__dashboard_reply":    true,
	"mcp__dashboard-channel__request_permission": true,
}
