package parser

import (
	"os"
	"path/filepath"
	"testing"
)

// A tail ending in a tool-only assistant message must still expose the
// preceding assistant text as LastOutput (the card's output preview source).
func TestParse_LastOutputSurvivesTrailingToolOnlyMessage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	lines := `{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-8","content":[{"type":"text","text":"Investigating the failing test now."}]}}
{"type":"assistant","message":{"role":"assistant","model":"claude-opus-4-8","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}
`
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := ParseSessionFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if data.LastOutput != "Investigating the failing test now." {
		t.Fatalf("LastOutput must survive a trailing tool-only message, got %q", data.LastOutput)
	}
	if data.CurrentAction != "Bash" {
		t.Fatalf("CurrentAction should be the last tool, got %q", data.CurrentAction)
	}
}
