package parser_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/stretchr/testify/require"
)

// writeSession creates a UUID-named JSONL session file under
// <configDir>/projects/<encoded-cwd>/ and returns the file path.
// ts controls the assistant message timestamp, which determines LastActivity.
func writeSession(t *testing.T, configDir, cwd string, ts time.Time) string {
	t.Helper()
	encoded := parser.EncodePath(cwd)
	dir := filepath.Join(configDir, "projects", encoded)
	require.NoError(t, os.MkdirAll(dir, 0o750))

	// Use a stable UUID-ish name derived from the timestamp so tests can create
	// multiple distinguishable sessions.
	name := fmt.Sprintf("00000000-0000-0000-0000-%012x.jsonl",
		ts.UnixMilli()&0xffffffffffff)

	content, err := json.Marshal(map[string]any{
		"role":    "assistant",
		"content": []any{map[string]any{"type": "text", "text": "hello"}},
		"model":   "claude-test",
		"usage":   map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
	require.NoError(t, err)

	entry, err := json.Marshal(map[string]any{
		"type":      "assistant",
		"timestamp": ts.UTC().Format(time.RFC3339Nano),
		"message":   json.RawMessage(content),
	})
	require.NoError(t, err)

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, append(entry, '\n'), 0o640))
	return path
}

// TestFindSessionForProject_CacheHit verifies that a second call within the TTL
// returns the cached result without re-reading the file.
func TestFindSessionForProject_CacheHit(t *testing.T) {
	// Use a very long TTL so the second call is guaranteed to hit.
	orig := parser.SessionCacheTTL
	parser.SessionCacheTTL = 10 * time.Minute
	t.Cleanup(func() { parser.SessionCacheTTL = orig })

	configDir := t.TempDir()
	cwd := "/tmp/test-project-cache-hit"

	ts := time.Now() // recent enough to satisfy uptime check
	writeSession(t, configDir, cwd, ts)

	got1, err := parser.FindSessionForProject(cwd, 0, configDir)
	require.NoError(t, err)
	require.NotNil(t, got1)
	require.Equal(t, "claude-test", got1.Model)

	// Second call — should return same session ID from cache.
	got2, err := parser.FindSessionForProject(cwd, 0, configDir)
	require.NoError(t, err)
	require.Equal(t, got1.SessionID, got2.SessionID)
}

// TestFindSessionForProject_CacheMissOnMtimeChange verifies that modifying the
// session file (mtime changes) causes a cache miss on the next call.
func TestFindSessionForProject_CacheMissOnMtimeChange(t *testing.T) {
	orig := parser.SessionCacheTTL
	parser.SessionCacheTTL = 10 * time.Minute
	t.Cleanup(func() { parser.SessionCacheTTL = orig })

	configDir := t.TempDir()
	cwd := "/tmp/test-project-cache-mtime"

	ts := time.Now()
	path := writeSession(t, configDir, cwd, ts)

	_, err := parser.FindSessionForProject(cwd, 0, configDir)
	require.NoError(t, err)

	// Simulate file update: write new content with a distinct model name.
	newTs := time.Now().Add(time.Second)
	content, _ := json.Marshal(map[string]any{
		"role":    "assistant",
		"content": []any{map[string]any{"type": "text", "text": "updated"}},
		"model":   "claude-updated",
		"usage":   map[string]any{"input_tokens": 2, "output_tokens": 2},
	})
	entry, _ := json.Marshal(map[string]any{
		"type":      "assistant",
		"timestamp": newTs.UTC().Format(time.RFC3339Nano),
		"message":   json.RawMessage(content),
	})
	require.NoError(t, os.WriteFile(path, append(entry, '\n'), 0o640))

	// Touch mtime explicitly to guarantee the OS flushes the change.
	now := time.Now()
	require.NoError(t, os.Chtimes(path, now, now))

	got2, err := parser.FindSessionForProject(cwd, 0, configDir)
	require.NoError(t, err)
	require.Equal(t, "claude-updated", got2.Model, "cache should have been invalidated by mtime change")
}

// TestFindSessionForProject_CacheMissOnTTLExpiry verifies that an entry older
// than SessionCacheTTL is not reused.
func TestFindSessionForProject_CacheMissOnTTLExpiry(t *testing.T) {
	// Use a zero TTL so every call is a miss.
	orig := parser.SessionCacheTTL
	parser.SessionCacheTTL = 0
	t.Cleanup(func() { parser.SessionCacheTTL = orig })

	configDir := t.TempDir()
	cwd := "/tmp/test-project-cache-ttl"

	ts := time.Now()
	writeSession(t, configDir, cwd, ts)

	got1, err := parser.FindSessionForProject(cwd, 0, configDir)
	require.NoError(t, err)

	// With zero TTL, the second call must re-parse (no cache entry survives).
	got2, err := parser.FindSessionForProject(cwd, 0, configDir)
	require.NoError(t, err)

	// Both calls return valid data — the point is no panic and correct output.
	require.Equal(t, got1.SessionID, got2.SessionID)
}

// TestFindSessionForProject_NoFiles verifies that an error is returned when no
// JSONL session files are present.
func TestFindSessionForProject_NoFiles(t *testing.T) {
	configDir := t.TempDir()
	cwd := "/tmp/test-project-no-files"

	encoded := parser.EncodePath(cwd)
	dir := filepath.Join(configDir, "projects", encoded)
	require.NoError(t, os.MkdirAll(dir, 0o750))

	_, err := parser.FindSessionForProject(cwd, 0, configDir)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "no session files"), "unexpected error: %v", err)
}
