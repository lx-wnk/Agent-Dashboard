package system

import (
	"sort"
	"testing"
)

// TestQuotaSortPicksNewest verifies that reverse-lexicographic sort of filenames
// puts the lexicographically largest name first — the assumption Quota relies on
// to find the most-recent usage JSON (Claude names them by date/time).
func TestQuotaSortPicksNewest(t *testing.T) {
	files := []string{
		"usage-2024-01-01.json",
		"usage-2024-03-15.json",
		"usage-2024-02-28.json",
		"usage-2024-03-16.json",
	}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	if files[0] != "usage-2024-03-16.json" {
		t.Errorf("expected newest first, got %q", files[0])
	}
	if files[len(files)-1] != "usage-2024-01-01.json" {
		t.Errorf("expected oldest last, got %q", files[len(files)-1])
	}
}

func TestQuotaSortSingleFile(t *testing.T) {
	files := []string{"usage-2024-01-01.json"}
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	if files[0] != "usage-2024-01-01.json" {
		t.Errorf("unexpected: got %q", files[0])
	}
}

func TestQuotaSortEmptySlice(t *testing.T) {
	var files []string
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	if len(files) != 0 {
		t.Errorf("expected empty slice, got %v", files)
	}
}
