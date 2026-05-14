// Package permissions defines the domain constants for the pipeline permission model.
package permissions

// IsAllowedTool reports whether name is in the pipeline tool allow-list.
// Use this instead of accessing allowedToolNames directly so the set
// cannot be mutated by other packages.
func IsAllowedTool(name string) bool { return allowedToolNames[name] }

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
