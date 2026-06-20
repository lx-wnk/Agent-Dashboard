package parser

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// sampleSubagentJSONL contains three assistant messages and one user message.
// Timestamps span 90 seconds. Two tool_use blocks appear; the second is the
// latest so it wins CurrentAction. The second text block wins LatestOutput.
// Token counts: first msg (10+1+100+1000=1111), second msg (20+2+200+2000=2222).
// Total = 3333.
const sampleSubagentJSONL = `{"type":"assistant","timestamp":"2026-06-04T10:00:00.000Z","message":{"role":"assistant","content":[{"type":"text","text":"first output"},{"type":"tool_use","name":"Read","id":"t1"}],"usage":{"input_tokens":10,"output_tokens":1,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000}}}
{"type":"user","timestamp":"2026-06-04T10:00:30.000Z","message":{"role":"user","content":[]}}
{"type":"assistant","timestamp":"2026-06-04T10:01:30.000Z","message":{"role":"assistant","content":[{"type":"text","text":"latest output"},{"type":"tool_use","name":"Bash","id":"t2"}],"usage":{"input_tokens":20,"output_tokens":2,"cache_creation_input_tokens":200,"cache_read_input_tokens":2000}}}
`

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return p
}

func TestParseSubagentFile_TokensAndDuration(t *testing.T) {
	dir := t.TempDir()
	p := writeFixture(t, dir, "sub.jsonl", sampleSubagentJSONL)

	got, err := ParseSubagentFile(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.TokensUsed != 3333 {
		t.Errorf("TokensUsed = %d, want 3333", got.TokensUsed)
	}
	if got.DurationSeconds != 90 {
		t.Errorf("DurationSeconds = %d, want 90", got.DurationSeconds)
	}
}

func TestParseSubagentFile_LatestOutputAndAction(t *testing.T) {
	dir := t.TempDir()
	p := writeFixture(t, dir, "sub.jsonl", sampleSubagentJSONL)

	got, err := ParseSubagentFile(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.LatestOutput != "latest output" {
		t.Errorf("LatestOutput = %q, want %q", got.LatestOutput, "latest output")
	}
	if got.CurrentAction != "Bash" {
		t.Errorf("CurrentAction = %q, want %q", got.CurrentAction, "Bash")
	}
}

func TestParseSubagentFile_LastActivity(t *testing.T) {
	dir := t.TempDir()
	p := writeFixture(t, dir, "sub.jsonl", sampleSubagentJSONL)

	got, err := ParseSubagentFile(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want, _ := time.Parse(time.RFC3339, "2026-06-04T10:01:30Z")
	if !got.LastActivity.Equal(want) {
		t.Errorf("LastActivity = %v, want %v", got.LastActivity, want)
	}
}

func TestParseSubagentFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := writeFixture(t, dir, "empty.jsonl", "")

	got, err := ParseSubagentFile(p)
	if err != nil {
		t.Fatalf("unexpected error on empty file: %v", err)
	}
	if got.TokensUsed != 0 || got.DurationSeconds != 0 || got.LatestOutput != "" {
		t.Errorf("expected zero value for empty file, got %+v", got)
	}
}

func TestParseSubagentFile_UnreadablePath(t *testing.T) {
	_, err := ParseSubagentFile("/nonexistent/path/sub.jsonl")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestParseSubagentFileCached_ReturnsSameResult(t *testing.T) {
	dir := t.TempDir()
	p := writeFixture(t, dir, "cached.jsonl", sampleSubagentJSONL)

	first, err := ParseSubagentFileCached(p)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := ParseSubagentFileCached(p)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if first != second {
		t.Errorf("cached result differs: first=%+v second=%+v", first, second)
	}
	if first.TokensUsed != 3333 {
		t.Errorf("TokensUsed = %d, want 3333", first.TokensUsed)
	}
}

func TestPruneSubagentCache_RemovesStaleEntries(t *testing.T) {
	dir := t.TempDir()
	kept := writeFixture(t, dir, "kept.jsonl", sampleSubagentJSONL)
	evicted := writeFixture(t, dir, "evicted.jsonl", sampleSubagentJSONL)

	// Warm the cache for both paths.
	if _, err := ParseSubagentFileCached(kept); err != nil {
		t.Fatalf("warm kept: %v", err)
	}
	if _, err := ParseSubagentFileCached(evicted); err != nil {
		t.Fatalf("warm evicted: %v", err)
	}

	subagentCacheMu.RLock()
	beforeKept := subagentCache[kept]
	beforeEvicted := subagentCache[evicted]
	subagentCacheMu.RUnlock()
	if beforeKept == (subagentCacheEntry{}) {
		t.Fatal("expected kept path in cache before prune")
	}
	if beforeEvicted == (subagentCacheEntry{}) {
		t.Fatal("expected evicted path in cache before prune")
	}

	// Prune with only the kept path live.
	PruneSubagentCache(map[string]bool{kept: true})

	subagentCacheMu.RLock()
	afterKept := subagentCache[kept]
	_, evictedPresent := subagentCache[evicted]
	subagentCacheMu.RUnlock()

	if afterKept == (subagentCacheEntry{}) {
		t.Error("kept path should still be in cache after prune")
	}
	if evictedPresent {
		t.Error("evicted path should have been removed from cache after prune")
	}
}

func TestParseSubagentFileCached_ReParseOnMtimeChange(t *testing.T) {
	dir := t.TempDir()
	p := writeFixture(t, dir, "reparsed.jsonl", sampleSubagentJSONL)

	first, err := ParseSubagentFileCached(p)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	// Overwrite with different content (one fewer assistant message = fewer tokens)
	singleLine := `{"type":"assistant","timestamp":"2026-06-04T10:00:00.000Z","message":{"role":"assistant","content":[{"type":"text","text":"new output"}],"usage":{"input_tokens":5,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}}` + "\n"
	// Force a new mtime by writing then touching
	if err := os.WriteFile(p, []byte(singleLine), 0600); err != nil {
		t.Fatal(err)
	}
	// Bump mtime by 1 second to guarantee a cache miss
	newMtime := time.Now().Add(time.Second)
	if err := os.Chtimes(p, newMtime, newMtime); err != nil {
		t.Fatal(err)
	}

	second, err := ParseSubagentFileCached(p)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if first.TokensUsed == second.TokensUsed {
		t.Errorf("expected re-parse after mtime change, but TokensUsed unchanged: %d", first.TokensUsed)
	}
	if second.TokensUsed != 6 {
		t.Errorf("second TokensUsed = %d, want 6", second.TokensUsed)
	}
	if second.LatestOutput != "new output" {
		t.Errorf("second LatestOutput = %q, want %q", second.LatestOutput, "new output")
	}
}
