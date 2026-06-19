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
