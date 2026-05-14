// Package permissions defines the domain constants for the pipeline permission model.
package permissions

// AllowedToolNames is the set of tools pipeline agents may be granted.
var AllowedToolNames = map[string]bool{
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
