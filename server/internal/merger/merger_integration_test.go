// Package merger_test provides integration tests for the merger package.
package merger_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdk "github.com/lx-wnk/agent-dashboard/sdk"
	"github.com/lx-wnk/agent-dashboard/server/internal/merger"
	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/lx-wnk/agent-dashboard/server/internal/scanner"
)

// TestGetAgents_DoesNotPanic verifies that GetAgents does not panic when called
// in an environment with no running Claude processes.
// It may return an error (e.g. scanner not available) or an empty slice — both are valid.
func TestGetAgents_DoesNotPanic(t *testing.T) {
	merger.ResetStaleTrackerForTest()
	t.Cleanup(merger.ResetStaleTrackerForTest)
	agents, err := merger.GetAgents(context.Background(), merger.GetAgentsOpts{})
	if err != nil {
		// Scanner may fail in CI — acceptable.
		t.Logf("GetAgents returned error (acceptable in CI): %v", err)
		return
	}
	// If no error, the result must be a valid (possibly empty) slice of the correct type.
	require.NotNil(t, agents)
	assert.IsType(t, []sdk.Agent{}, agents)
}

// fixedScanFn returns a scanProcessesFn override that yields a fixed process list.
func fixedScanFn(procs []scanner.ProcessInfo) func(ctx context.Context) ([]scanner.ProcessInfo, error) {
	return func(ctx context.Context) ([]scanner.ProcessInfo, error) {
		return procs, nil
	}
}

// TestGetAgents_CodexProcess_NoFiles verifies a Codex process with no session
// JSONL under its config dir produces zero agents and no error.
func TestGetAgents_CodexProcess_NoFiles(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome) // exists but has no projects/ session files

	merger.ResetStaleTrackerForTest()
	t.Cleanup(merger.ResetStaleTrackerForTest)

	restore := merger.SetScanProcessesForTest(fixedScanFn([]scanner.ProcessInfo{
		{PID: 4242, CWD: "/some/project", Uptime: 30, Provider: sdk.ProviderCodex},
	}))
	defer restore()

	agents, err := merger.GetAgents(context.Background(), merger.GetAgentsOpts{})
	require.NoError(t, err)
	assert.Empty(t, agents, "no JSONL under codex dir must yield zero agents")
}

// TestGetAgents_CodexProcess_WithJSONL verifies a Codex process with a session
// JSONL under its config dir surfaces an agent flagged provider=codex and
// CostUnknown=true (no Codex pricing entry).
func TestGetAgents_CodexProcess_WithJSONL(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)

	merger.ResetStaleTrackerForTest()
	t.Cleanup(merger.ResetStaleTrackerForTest)

	cwd := "/some/project"
	projectDir := filepath.Join(codexHome, "projects", parser.EncodePath(cwd))
	require.NoError(t, os.MkdirAll(projectDir, 0o755))

	// Minimal Claude-compatible JSONL: one assistant turn with usage but no
	// model — an unknown model forces CostUnknown for the non-Claude provider.
	line := `{"type":"assistant","timestamp":"` + time.Now().UTC().Format(time.RFC3339) +
		`","message":{"role":"assistant","usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"
	sessionFile := filepath.Join(projectDir, "11111111-2222-3333-4444-555555555555.jsonl")
	require.NoError(t, os.WriteFile(sessionFile, []byte(line), 0o644))

	restore := merger.SetScanProcessesForTest(fixedScanFn([]scanner.ProcessInfo{
		{PID: 4243, CWD: cwd, Uptime: 30, Provider: sdk.ProviderCodex},
	}))
	defer restore()

	agents, err := merger.GetAgents(context.Background(), merger.GetAgentsOpts{})
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.Equal(t, sdk.ProviderCodex, agents[0].Provider)
	assert.True(t, agents[0].CostUnknown, "codex cost must be unknown")
	assert.Equal(t, 0.0, agents[0].CostEstimate)
}

// TestGetAgents_EmitsFinishedAfterProcessExits verifies a controllable agent
// seen while live is re-emitted as a finished card after its process exits,
// driven by the package stale tracker.
func TestGetAgents_EmitsFinishedAfterProcessExits(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	merger.ResetStaleTrackerForTest()
	t.Cleanup(merger.ResetStaleTrackerForTest)

	const sessionID = "11111111-2222-3333-4444-555555555555"
	const pid = 4242
	cwd := filepath.Join(home, "work", "project")

	projectDir := filepath.Join(home, ".claude", "projects", parser.EncodePath(cwd))
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	ts := time.Now().UTC().Format(time.RFC3339)
	lines := `{"type":"user","sessionId":"` + sessionID + `","timestamp":"` + ts +
		`","message":{"role":"user","content":"hi"}}` + "\n" +
		`{"type":"assistant","sessionId":"` + sessionID + `","timestamp":"` + ts +
		`","message":{"role":"assistant","model":"claude-opus-4-8","usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"
	sessionFile := filepath.Join(projectDir, sessionID+".jsonl")
	require.NoError(t, os.WriteFile(sessionFile, []byte(lines), 0o644))

	discoveryDir := filepath.Join(home, ".claude", "dashboard-channel")
	require.NoError(t, os.MkdirAll(discoveryDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(discoveryDir, "4242.json"), []byte(`{"port":1}`), 0o644))

	restore := merger.SetScanProcessesForTest(fixedScanFn([]scanner.ProcessInfo{
		{PID: pid, CWD: cwd, Uptime: 30, Provider: sdk.ProviderClaude},
	}))

	// Tick 1: process is live, session resolves, snapshot recorded.
	live, err := merger.GetAgents(context.Background(), merger.GetAgentsOpts{})
	require.NoError(t, err)
	require.Len(t, live, 1)
	require.Equal(t, sessionID, live[0].SessionID)
	require.NotEqual(t, sdk.AgentStatusFinished, live[0].Status, "agent must be live on tick 1")

	// Tick 2: process is gone; the finished card must be emitted from the tracker.
	restore()
	restore = merger.SetScanProcessesForTest(fixedScanFn(nil))
	defer restore()

	finished, err := merger.GetAgents(context.Background(), merger.GetAgentsOpts{})
	require.NoError(t, err)
	require.Len(t, finished, 1)
	assert.Equal(t, sessionID, finished[0].SessionID)
	assert.Equal(t, sdk.AgentStatusFinished, finished[0].Status)
	assert.Equal(t, pid, finished[0].PID)
}

// TestGetAgents_FinishedSurvivesDiscoveryFileRemoval models the real bridge,
// which removes ~/.claude/dashboard-channel/{pid}.json when the process exits.
// The finished card must still appear, driven by the in-memory tracker rather
// than the discovery file's continued existence.
func TestGetAgents_FinishedSurvivesDiscoveryFileRemoval(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	merger.ResetStaleTrackerForTest()
	t.Cleanup(merger.ResetStaleTrackerForTest)

	const sessionID = "11111111-2222-3333-4444-555555555555"
	const pid = 4242
	cwd := filepath.Join(home, "work", "project")
	projectDir := filepath.Join(home, ".claude", "projects", parser.EncodePath(cwd))
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	ts := time.Now().UTC().Format(time.RFC3339)
	lines := `{"type":"user","sessionId":"` + sessionID + `","timestamp":"` + ts +
		`","message":{"role":"user","content":"hi"}}` + "\n" +
		`{"type":"assistant","sessionId":"` + sessionID + `","timestamp":"` + ts +
		`","message":{"role":"assistant","model":"claude-opus-4-8","usage":{"input_tokens":10,"output_tokens":5}}}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, sessionID+".jsonl"), []byte(lines), 0o644))

	discDir := filepath.Join(home, ".claude", "dashboard-channel")
	require.NoError(t, os.MkdirAll(discDir, 0o755))
	discFile := filepath.Join(discDir, "4242.json")
	require.NoError(t, os.WriteFile(discFile, []byte(`{"port":1}`), 0o644))

	// Tick 1: live + channel-available → recorded.
	restore := merger.SetScanProcessesForTest(fixedScanFn([]scanner.ProcessInfo{
		{PID: pid, CWD: cwd, Uptime: 30, Provider: sdk.ProviderClaude},
	}))
	live, err := merger.GetAgents(context.Background(), merger.GetAgentsOpts{})
	require.NoError(t, err)
	require.Len(t, live, 1)
	require.True(t, live[0].ChannelAvailable)
	restore()

	// Process exits AND the bridge removes its discovery file (real behaviour).
	require.NoError(t, os.Remove(discFile))
	restore = merger.SetScanProcessesForTest(fixedScanFn(nil))
	defer restore()

	finished, err := merger.GetAgents(context.Background(), merger.GetAgentsOpts{})
	require.NoError(t, err)
	require.Len(t, finished, 1)
	assert.Equal(t, sdk.AgentStatusFinished, finished[0].Status)
	assert.Equal(t, sessionID, finished[0].SessionID)
}

// TestGetAgents_NonChannelAgentNoFinishedCard verifies an agent that was never
// channel-available (no discovery file) does not get a finished card after exit.
func TestGetAgents_NonChannelAgentNoFinishedCard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	merger.ResetStaleTrackerForTest()
	t.Cleanup(merger.ResetStaleTrackerForTest)

	const sessionID = "22222222-2222-3333-4444-555555555555"
	const pid = 5252
	cwd := filepath.Join(home, "work", "p2")
	projectDir := filepath.Join(home, ".claude", "projects", parser.EncodePath(cwd))
	require.NoError(t, os.MkdirAll(projectDir, 0o755))
	ts := time.Now().UTC().Format(time.RFC3339)
	lines := `{"type":"user","sessionId":"` + sessionID + `","timestamp":"` + ts +
		`","message":{"role":"user","content":"hi"}}` + "\n" +
		`{"type":"assistant","sessionId":"` + sessionID + `","timestamp":"` + ts +
		`","message":{"role":"assistant","model":"claude-opus-4-8","usage":{"input_tokens":1,"output_tokens":1}}}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, sessionID+".jsonl"), []byte(lines), 0o644))
	// NO discovery file → not channel-available.

	restore := merger.SetScanProcessesForTest(fixedScanFn([]scanner.ProcessInfo{
		{PID: pid, CWD: cwd, Uptime: 30, Provider: sdk.ProviderClaude},
	}))
	_, err := merger.GetAgents(context.Background(), merger.GetAgentsOpts{})
	require.NoError(t, err)
	restore()

	restore = merger.SetScanProcessesForTest(fixedScanFn(nil))
	defer restore()
	finished, err := merger.GetAgents(context.Background(), merger.GetAgentsOpts{})
	require.NoError(t, err)
	for _, a := range finished {
		if a.SessionID == sessionID {
			t.Fatalf("non-channel agent must not get a finished card")
		}
	}
}
