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

func TestParseTmuxEnv(t *testing.T) {
	pane, socket := parseTmuxEnv("%3", "/tmp/tmux-501/default,1234,0")
	if pane != "%3" || socket != "/tmp/tmux-501/default" {
		t.Fatalf("got pane=%q socket=%q", pane, socket)
	}
	if p, s := parseTmuxEnv("", "/sock,1,0"); p != "" || s != "" {
		t.Fatalf("no pane → empty, got %q %q", p, s)
	}
	if p, s := parseTmuxEnv("%1", ""); p != "%1" || s != "" {
		t.Fatalf("pane without TMUX → socket empty, got %q %q", p, s)
	}
}
