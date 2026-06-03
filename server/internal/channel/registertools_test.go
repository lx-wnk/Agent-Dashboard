package channel

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestRegisterTools_NoPanic guards against MCP go-sdk struct-tag breakage:
// AddTool panics at registration if a jsonschema struct tag uses a format the
// SDK rejects (e.g. "description=...,required"). Such a panic crashes the bridge
// on startup → no discovery file → channelAvailable is silently always false.
func TestRegisterTools_NoPanic(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "dashboard-channel", Version: "0.1.0"}, nil)
	// Must not panic.
	registerTools(server, "http://127.0.0.1:13120", "tok", "stage-run-1", 4242)
}
