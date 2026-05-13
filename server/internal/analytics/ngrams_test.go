package analytics

import (
	"testing"
)

func TestExtractNgrams_empty(t *testing.T) {
	got := ExtractNgrams(nil)
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}
}

func TestExtractNgrams_tooShort(t *testing.T) {
	got := ExtractNgrams([]string{"Read", "Write"})
	if len(got) != 0 {
		t.Fatalf("expected empty map for 2-element slice, got %v", got)
	}
}

func TestExtractNgrams_exactN(t *testing.T) {
	tools := []string{"Read", "Write", "Bash"}
	got := ExtractNgrams(tools)
	if len(got) != 1 {
		t.Fatalf("expected 1 gram, got %d: %v", len(got), got)
	}
	key := "Read → Write → Bash"
	if got[key] != 1 {
		t.Fatalf("expected gram %q count=1, got %d", key, got[key])
	}
}

func TestExtractNgrams_repeated(t *testing.T) {
	tools := []string{"Read", "Write", "Bash", "Read", "Write", "Bash"}
	got := ExtractNgrams(tools)
	key := "Read → Write → Bash"
	if got[key] != 2 {
		t.Fatalf("expected gram %q count=2, got %d", key, got[key])
	}
}

func TestExtractNgrams_multipleGrams(t *testing.T) {
	tools := []string{"A", "B", "C", "D"}
	got := ExtractNgrams(tools)
	// expect: "A → B → C" and "B → C → D"
	if len(got) != 2 {
		t.Fatalf("expected 2 grams, got %d: %v", len(got), got)
	}
	if got["A → B → C"] != 1 {
		t.Errorf("expected A→B→C count=1, got %d", got["A → B → C"])
	}
	if got["B → C → D"] != 1 {
		t.Errorf("expected B→C→D count=1, got %d", got["B → C → D"])
	}
}

func TestParseToolsFromRaw(t *testing.T) {
	// Minimal JSONL with one assistant message containing two tool_use blocks.
	raw := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Read"},{"type":"tool_use","name":"Bash"}]}}` + "\n"
	got := parseToolsFromRaw(raw)
	if len(got) != 2 || got[0] != "Read" || got[1] != "Bash" {
		t.Fatalf("unexpected tools: %v", got)
	}
}

func TestParseToolsFromRaw_skipsInvalidNames(t *testing.T) {
	// Tool name with leading digit should be skipped.
	raw := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"1bad"},{"type":"tool_use","name":"Good"}]}}` + "\n"
	got := parseToolsFromRaw(raw)
	if len(got) != 1 || got[0] != "Good" {
		t.Fatalf("unexpected tools: %v", got)
	}
}

func TestParseToolsFromRaw_skipsNonAssistant(t *testing.T) {
	// user-role messages should be ignored.
	raw := `{"type":"user","message":{"role":"user","content":[{"type":"tool_use","name":"Read"}]}}` + "\n"
	got := parseToolsFromRaw(raw)
	if len(got) != 0 {
		t.Fatalf("expected empty, got: %v", got)
	}
}
