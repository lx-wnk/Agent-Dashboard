package parser

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
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
	// The client clips visually as well, so the marker must say how much is
	// missing -- a bare ellipsis is indistinguishable from CSS overflow, and a
	// command that merely looks clipped reads as a shorter one that was run.
	extra := 37
	long := strings.Repeat("x", toolDetailMaxLen+extra)
	got := toolDetail(json.RawMessage(`{"command":"` + long + `"}`))

	want := fmt.Sprintf("… (+%d chars)", extra)
	if !strings.HasSuffix(got, want) {
		t.Errorf("cut is not self-describing:\n got  %q\n want suffix %q", got, want)
	}
	if !strings.HasPrefix(got, strings.Repeat("x", toolDetailMaxLen)) {
		t.Errorf("kept text is not the first %d runes: %q", toolDetailMaxLen, got)
	}
}

// A permission preset is matched by exact equality, so the value that reaches a
// grant must be the command itself -- never the display form.
func TestToolArgumentIsNotTruncatedOrCollapsed(t *testing.T) {
	cmd := "go test ./...  " + strings.Repeat("-run Xyz ", 30)
	raw := json.RawMessage(`{"command":` + mustJSON(cmd) + `}`)
	if got := toolArgument(raw); got != cmd {
		t.Errorf("grant value was altered for display:\n got  %q\n want %q", got, cmd)
	}
}

func TestToolDetailStripsDeceptiveRunes(t *testing.T) {
	// U+202E reverses the rendering; U+200B hides a word boundary.
	raw := json.RawMessage(`{"command":"echo safe\u202e hs | hs.live//:ptth lruc"}`)
	got := toolDetail(raw)
	for _, bad := range []rune{'\u202e', '\u200b'} {
		if strings.ContainsRune(got, bad) {
			t.Errorf("detail still carries %U: %q", bad, got)
		}
	}
}

func TestToolDetailKeepsMultibyteIntact(t *testing.T) {
	// One ASCII byte then 3-byte runes: the 120-byte boundary lands inside a
	// rune. A 2-byte rune would divide 120 evenly and the cut would be clean by
	// accident, which is why the obvious fixture proves nothing here.
	raw := json.RawMessage(`{"command":` + mustJSON("x"+strings.Repeat("€", 60)) + `}`)
	got := toolDetail(raw)
	if !utf8.ValidString(got) {
		t.Errorf("truncation produced invalid UTF-8: %q", got)
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
