package pipeline

// AllowedToolNames is the set of tools pipeline agents may be granted.
// Exported so that consumers outside the pipeline package (e.g. mcp/tools)
// can validate permission grants without duplicating the list.
var AllowedToolNames = map[string]bool{
	"Read":      true,
	"Write":     true,
	"Edit":      true,
	"MultiEdit": true,
	"Glob":      true,
	"Grep":      true,
	"LS":        true,
	"Bash":      true,
	"WebFetch":  true,
	"mcp__dashboard-channel__dashboard_reply":    true,
	"mcp__dashboard-channel__request_permission": true,
}
