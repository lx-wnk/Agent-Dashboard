package pipeline_test

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/db/ent"
	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/stretchr/testify/require"
)

func TestBuildAllowList_ExcludesGitPushByDefault(t *testing.T) {
	pattern := "git push origin HEAD"
	perms := []*ent.TaskPermission{
		{Tool: "Bash", Pattern: &pattern, Granted: true},
		{Tool: "Read", Granted: true},
	}
	allow := pipeline.BuildAllowList(perms, false, false)
	require.Contains(t, allow, "Read")
	for _, a := range allow {
		require.NotContains(t, a, "git push")
	}
}

func TestBuildAllowList_AllowsGitPushWhenEnabled(t *testing.T) {
	pattern := "git push origin HEAD"
	perms := []*ent.TaskPermission{
		{Tool: "Bash", Pattern: &pattern, Granted: true},
	}
	allow := pipeline.BuildAllowList(perms, false, true)
	require.Contains(t, allow, "Bash(git push origin HEAD)")
}

func TestBuildAllowList_FiltersDenied(t *testing.T) {
	perms := []*ent.TaskPermission{
		{Tool: "Bash", Granted: false},
		{Tool: "Read", Granted: true},
	}
	allow := pipeline.BuildAllowList(perms, false, false)
	require.Contains(t, allow, "Read")
	for _, a := range allow {
		require.NotEqual(t, "Bash", a)
	}
}

func TestBuildAllowList_IncludesChannelTools(t *testing.T) {
	allow := pipeline.BuildAllowList(nil, true, false)
	require.Contains(t, allow, "mcp__dashboard-channel__request_permission")
	require.Contains(t, allow, "mcp__dashboard-channel__dashboard_reply")
}

func TestBuildSpawnArgs_Basic(t *testing.T) {
	opts := pipeline.SpawnAgentOptions{
		Task:     &ent.Task{},
		StageRun: &ent.StageRun{},
		Prompt:   "do the thing",
		Model:    "claude-opus-4-7",
	}
	args := pipeline.BuildSpawnArgs(opts)
	require.Contains(t, args, "-p")
	require.Contains(t, args, "do the thing")
	require.Contains(t, args, "--model")
	require.Contains(t, args, "claude-opus-4-7")
}

func TestBuildSpawnArgs_WithResume(t *testing.T) {
	opts := pipeline.SpawnAgentOptions{
		Task:            &ent.Task{},
		StageRun:        &ent.StageRun{},
		Prompt:          "p",
		ResumeSessionID: "abc123",
	}
	args := pipeline.BuildSpawnArgs(opts)
	require.Contains(t, args, "--resume")
	require.Contains(t, args, "abc123")
}
