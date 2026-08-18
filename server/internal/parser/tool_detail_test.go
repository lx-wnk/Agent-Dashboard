package parser

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolDetail(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"bash command", `{"command":"go test ./..."}`, "go test ./..."},
		{"edit path", `{"file_path":"/tmp/x.go"}`, "/tmp/x.go"},
		{"neither", `{"todos":[]}`, ""},
		{"malformed input degrades to empty", `not json`, ""},
		{"newlines collapse so one entry stays one line", "{\"command\":\"a\\n\\n  b\"}", "a b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := toolDetail(json.RawMessage(c.input)); got != c.want {
				t.Errorf("toolDetail(%s) = %q, want %q", c.input, got, c.want)
			}
		})
	}
}

func TestToolDetailMarksTheCut(t *testing.T) {
	long := strings.Repeat("x", toolDetailMaxLen+50)
	got := toolDetail(json.RawMessage(`{"command":"` + long + `"}`))
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a truncated detail must be marked as cut, got %q", got[len(got)-10:])
	}
	if len([]rune(got)) != toolDetailMaxLen+1 {
		t.Errorf("truncated length = %d runes, want %d", len([]rune(got)), toolDetailMaxLen+1)
	}
}
