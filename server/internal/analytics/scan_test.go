package analytics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupFakeClaudeDir builds a temp dir that mimics ~/.claude/projects layout
// and copies each (sessionID, fixture) into projects/<encodedProject>/<sessionID>.jsonl
// with deterministic mtimes for ordering tests.
func setupFakeClaudeDir(t *testing.T, sessions map[string]string) string {
	t.Helper()
	root := t.TempDir()
	projectDir := filepath.Join(root, "projects", "-tmp-fixture")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Use a fixed base time so mtimes are deterministic relative to From/To
	// bounds chosen by callers.
	base := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	i := 0
	for sessID, fixture := range sessions {
		src := filepath.Join("testdata", fixture)
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatalf("read fixture %s: %v", src, err)
		}
		dst := filepath.Join(projectDir, sessID+".jsonl")
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", dst, err)
		}
		mtime := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(dst, mtime, mtime); err != nil {
			t.Fatalf("chtimes: %v", err)
		}
		i++
	}
	return root
}

func TestDiscoverSessions_MaxSessionsCap(t *testing.T) {
	sessions := map[string]string{
		"00000000-0000-0000-0000-000000000001": "single-session-linear.jsonl",
		"00000000-0000-0000-0000-000000000002": "two-sessions-branching.jsonl",
		"00000000-0000-0000-0000-000000000003": "two-sessions-branching-2.jsonl",
	}
	root := setupFakeClaudeDir(t, sessions)

	got := DiscoverSessions(ScanOpts{MaxSessions: 2}, []string{root})
	if len(got) != 2 {
		t.Fatalf("want 2 sessions after cap, got %d", len(got))
	}
	// Newer mtimes should come first (sorted descending).
	if got[0].ModTime.Before(got[1].ModTime) {
		t.Errorf("results not sorted by mtime desc: %v < %v", got[0].ModTime, got[1].ModTime)
	}
}

func TestDiscoverSessions_TimeBounds(t *testing.T) {
	sessions := map[string]string{
		"00000000-0000-0000-0000-0000000000aa": "single-session-linear.jsonl",
		"00000000-0000-0000-0000-0000000000bb": "two-sessions-branching.jsonl",
	}
	root := setupFakeClaudeDir(t, sessions)

	// base = 2026-05-22T10:00 — pick a window that only includes the second
	// file (mtime base + 1m).
	from := time.Date(2026, 5, 22, 10, 0, 30, 0, time.UTC)
	got := DiscoverSessions(ScanOpts{From: from}, []string{root})
	if len(got) != 1 {
		t.Fatalf("want 1 session after From filter, got %d", len(got))
	}
}

func TestDiscoverSessions_SessionAllowList(t *testing.T) {
	wanted := "00000000-0000-0000-0000-0000000000cc"
	sessions := map[string]string{
		wanted: "single-session-linear.jsonl",
		"00000000-0000-0000-0000-0000000000dd": "two-sessions-branching.jsonl",
	}
	root := setupFakeClaudeDir(t, sessions)

	got := DiscoverSessions(ScanOpts{Sessions: []string{wanted}}, []string{root})
	if len(got) != 1 || got[0].SessionID != wanted {
		t.Fatalf("allow-list filter failed: %+v", got)
	}
}

func TestScanSessionsForTools_LinearFixture(t *testing.T) {
	sessID := "00000000-0000-0000-0000-0000000000ee"
	root := setupFakeClaudeDir(t, map[string]string{sessID: "single-session-linear.jsonl"})

	got, err := scanSessionsForTools(context.Background(), ScanOpts{}, []string{root})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	calls := got[sessID]
	if len(calls) != 3 {
		t.Fatalf("want 3 tool calls, got %d", len(calls))
	}
	want := []string{"Read", "Edit", "Bash"}
	for i, name := range want {
		if calls[i].Name != name {
			t.Errorf("call[%d] name = %q, want %q", i, calls[i].Name, name)
		}
	}
}

func TestScanSessionsForTools_ContextCancellation(t *testing.T) {
	sessID := "00000000-0000-0000-0000-0000000000ff"
	root := setupFakeClaudeDir(t, map[string]string{sessID: "single-session-linear.jsonl"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := scanSessionsForTools(ctx, ScanOpts{}, []string{root})
	if err == nil {
		t.Fatalf("expected context cancellation error")
	}
}
