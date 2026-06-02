package pipeline_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/parser"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/stretchr/testify/require"
)

// writeSession creates <projectDir>/<id>.jsonl with the given mtime.
func writeSession(t *testing.T, projectDir, id string, mtime time.Time) {
	t.Helper()
	p := filepath.Join(projectDir, id+".jsonl")
	require.NoError(t, os.WriteFile(p, []byte("{}\n"), 0o600))
	require.NoError(t, os.Chtimes(p, mtime, mtime))
}

func TestFindNewestSessionID_ExcludesSessionsBeforeCutoff(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)
	cwd := t.TempDir()
	projectDir, derr := pipeline.ResolvedProjectDir(cwd)
	require.NoError(t, derr)
	require.NoError(t, os.MkdirAll(projectDir, 0o700))

	start := time.Date(2026, 6, 1, 15, 7, 26, 0, time.Local)
	// Prior iteration's session — finished well before this run started.
	writeSession(t, projectDir, "old-prior-iteration", start.Add(-10*time.Minute))
	// This run's own session — created shortly after start.
	writeSession(t, projectDir, "new-this-run", start.Add(5*time.Second))

	afterISO := start.Format("2006-01-02T15:04:05Z") // same layout the orchestrator uses
	got, err := pipeline.FindNewestSessionID(cwd, afterISO)
	require.NoError(t, err)
	require.Equal(t, "new-this-run", got, "must not return the prior iteration's stale session")
}

func TestFindNewestSessionID_NoSessionAfterCutoffReturnsEmpty(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)
	cwd := t.TempDir()
	projectDir, derr := pipeline.ResolvedProjectDir(cwd)
	require.NoError(t, derr)
	require.NoError(t, os.MkdirAll(projectDir, 0o700))

	start := time.Date(2026, 6, 1, 15, 7, 26, 0, time.Local)
	writeSession(t, projectDir, "only-old", start.Add(-10*time.Minute))

	afterISO := start.Format("2006-01-02T15:04:05Z")
	got, err := pipeline.FindNewestSessionID(cwd, afterISO)
	require.NoError(t, err)
	require.Equal(t, "", got, "a dead agent that wrote no session must not resurrect a stale one")
}

func TestFindNewestSessionID_SearchesAllConfigDirs(t *testing.T) {
	// Server runs under one config dir; the spawner's agent wrote its session
	// under a DIFFERENT one (custom CLAUDE_CONFIG_DIR). Must still be found.
	server := t.TempDir()
	agentCfg := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", server)
	t.Setenv("DASHBOARD_CLAUDE_CONFIG_DIRS", agentCfg)
	cwd := t.TempDir()
	resolved, err := filepath.EvalSymlinks(cwd)
	require.NoError(t, err)
	agentProj := filepath.Join(agentCfg, "projects", parser.EncodePath(resolved))
	require.NoError(t, os.MkdirAll(agentProj, 0o700))
	writeSession(t, agentProj, "agent-wrote-here", time.Date(2026, 6, 1, 15, 0, 0, 0, time.Local))

	got, err := pipeline.FindNewestSessionID(cwd, "")
	require.NoError(t, err)
	require.Equal(t, "agent-wrote-here", got, "session under a custom config dir must be discovered")
}

func TestFindNewestSessionID_NoCutoffReturnsNewest(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", cfgDir)
	cwd := t.TempDir()
	projectDir, derr := pipeline.ResolvedProjectDir(cwd)
	require.NoError(t, derr)
	require.NoError(t, os.MkdirAll(projectDir, 0o700))

	base := time.Date(2026, 6, 1, 12, 0, 0, 0, time.Local)
	writeSession(t, projectDir, "older", base)
	writeSession(t, projectDir, "newer", base.Add(time.Hour))

	got, err := pipeline.FindNewestSessionID(cwd, "")
	require.NoError(t, err)
	require.Equal(t, "newer", got)
}

func TestExtractJsonBlock_LastBlock(t *testing.T) {
	text := "prose\n```json\n{\"a\":1}\n```\nmore\n```json\n{\"b\":2}\n```"
	result := pipeline.ExtractJsonBlock(text)
	require.NotNil(t, result)
	require.Equal(t, float64(2), result["b"])
}

func TestExtractJsonBlock_NoBlock(t *testing.T) {
	require.Nil(t, pipeline.ExtractJsonBlock("no code blocks here"))
}

func TestExtractJsonBlock_InvalidJSON(t *testing.T) {
	require.Nil(t, pipeline.ExtractJsonBlock("```json\n{invalid}\n```"))
}
