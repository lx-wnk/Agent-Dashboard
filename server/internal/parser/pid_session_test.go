package parser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// writeSessionJSONL writes a minimal one-assistant-line session file whose
// LastActivity is `age` before now, returning the file path.
func writeSessionJSONL(t *testing.T, projectDir, sessionID string, age time.Duration) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	ts := time.Now().Add(-age).UTC().Format(time.RFC3339Nano)
	msg, _ := json.Marshal(map[string]any{
		"role":    "assistant",
		"content": []map[string]any{{"type": "text", "text": "hi from " + sessionID}},
	})
	entry, _ := json.Marshal(map[string]any{
		"type":      "assistant",
		"timestamp": ts,
		"message":   json.RawMessage(msg),
	})
	path := filepath.Join(projectDir, sessionID+".jsonl")
	require.NoError(t, os.WriteFile(path, append(entry, '\n'), 0o644))
	return path
}

// writePidSessionFile writes ~/.claude/sessions/{pid}.json.
func writePidSessionFile(t *testing.T, configDir string, pid int, sessionID, cwd string) {
	t.Helper()
	dir := filepath.Join(configDir, "sessions")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	b, _ := json.Marshal(map[string]any{
		"pid":        pid,
		"sessionId":  sessionID,
		"cwd":        cwd,
		"entrypoint": "cli",
		"status":     "busy",
	})
	require.NoError(t, os.WriteFile(filepath.Join(dir, jsonName(pid)), b, 0o644))
}

func jsonName(pid int) string { return itoa(pid) + ".json" }

// resetSessionCache clears the package-level FindSessionForProject cache so
// each test starts from a clean slate (internal-package access).
func resetSessionCache() {
	sessionCacheMu.Lock()
	sessionCache = make(map[sessionCacheKey]*sessionCacheEntry)
	sessionCacheMu.Unlock()
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestSessionIDFromArgs(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    string
	}{
		{"resume long", "claude --resume c8cb6d9b-5255-4892-b933-ecef415ed9f3", "c8cb6d9b-5255-4892-b933-ecef415ed9f3"},
		{"resume equals", "claude --resume=c8cb6d9b-5255-4892-b933-ecef415ed9f3", "c8cb6d9b-5255-4892-b933-ecef415ed9f3"},
		{"session-id flag", "claude --session-id 6e435d79-14a3-43a0-9d7d-8baf54b69c35", "6e435d79-14a3-43a0-9d7d-8baf54b69c35"},
		{"no session", "claude --allow-dangerously-skip-permissions", ""},
		{"resume without value", "claude --resume", ""},
		{"non-uuid value", "claude --resume latest", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, SessionIDFromArgs(tt.command))
		})
	}
}

func TestReadPidSession(t *testing.T) {
	cfg := t.TempDir()
	writePidSessionFile(t, cfg, 4242, "75230ea5-031f-4adc-9b26-e5c17de98c21", "/some/cwd")

	ps, ok := ReadPidSession(4242, []string{cfg})
	require.True(t, ok)
	require.Equal(t, "75230ea5-031f-4adc-9b26-e5c17de98c21", ps.SessionID)
	require.Equal(t, "/some/cwd", ps.CWD)

	_, ok = ReadPidSession(9999, []string{cfg})
	require.False(t, ok)
}

// TestResolveSessionForProcess_DistinctPerPID is the regression test for the
// reported bug: two processes in the SAME folder must resolve to their OWN
// session, not the single newest-mtime file shared by both.
func TestResolveSessionForProcess_DistinctPerPID(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	resetSessionCache()

	cwd := "/work/agent-dashboard"
	projectDir := filepath.Join(cfg, "projects", EncodePath(cwd))

	// sessionA is older, sessionB is the newest file (would win the old heuristic for BOTH pids).
	writeSessionJSONL(t, projectDir, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 60*time.Second)
	writeSessionJSONL(t, projectDir, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", 5*time.Second)

	// pid 100 owns sessionA, pid 200 owns sessionB (per the authoritative files).
	writePidSessionFile(t, cfg, 100, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", cwd)
	writePidSessionFile(t, cfg, 200, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", cwd)

	claimed := map[string]bool{}
	a, err := ResolveSessionForProcess(SessionRequest{CWD: cwd, PID: 100, Command: "claude", UptimeSeconds: 120}, claimed)
	require.NoError(t, err)
	b, err := ResolveSessionForProcess(SessionRequest{CWD: cwd, PID: 200, Command: "claude", UptimeSeconds: 120}, claimed)
	require.NoError(t, err)

	require.Equal(t, "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", a.SessionID, "pid 100 must resolve to its own session")
	require.Equal(t, "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", b.SessionID, "pid 200 must resolve to its own session")
}

// TestResolveSessionForProcess_FallbackDistinct verifies that when NO authoritative
// pid file and NO --resume arg exist, two bare-claude pids still get distinct
// sessions via the claimed-set mtime fallback.
func TestResolveSessionForProcess_FallbackDistinct(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	resetSessionCache()

	cwd := "/work/proj"
	projectDir := filepath.Join(cfg, "projects", EncodePath(cwd))
	writeSessionJSONL(t, projectDir, "cccccccc-cccc-cccc-cccc-cccccccccccc", 40*time.Second)
	writeSessionJSONL(t, projectDir, "dddddddd-dddd-dddd-dddd-dddddddddddd", 5*time.Second)

	claimed := map[string]bool{}
	a, err := ResolveSessionForProcess(SessionRequest{CWD: cwd, PID: 300, Command: "claude", UptimeSeconds: 120}, claimed)
	require.NoError(t, err)
	b, err := ResolveSessionForProcess(SessionRequest{CWD: cwd, PID: 400, Command: "claude", UptimeSeconds: 120}, claimed)
	require.NoError(t, err)

	require.NotEqual(t, a.SessionID, b.SessionID, "two bare-claude pids must not collide on the same session")
}
