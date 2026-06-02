package pipeline_test

import (
	"strings"
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

func TestBuildSpawnArgs_DefaultPermissionModeWithoutSkip(t *testing.T) {
	opts := pipeline.SpawnAgentOptions{
		Task:     &ent.Task{},
		StageRun: &ent.StageRun{},
		Prompt:   "p",
	}
	args := pipeline.BuildSpawnArgs(opts)
	require.Contains(t, args, "--permission-mode")
	require.Contains(t, args, "default")
}

func TestBuildSpawnArgs_OmitsPermissionModeForSkipSpawner(t *testing.T) {
	for _, flag := range []string{"--dangerously-skip-permissions", "--allow-dangerously-skip-permissions"} {
		opts := pipeline.SpawnAgentOptions{
			Task:     &ent.Task{},
			StageRun: &ent.StageRun{},
			Prompt:   "p",
			Spawner:  &ent.Spawner{Command: "claude", Args: []string{flag}},
		}
		args := pipeline.BuildSpawnArgs(opts)
		require.NotContains(t, args, "--permission-mode",
			"skip-permissions spawner (%s) must not get a forced --permission-mode, which overrides the skip", flag)
	}
}

func TestBuildSpawnArgs_OmitsDefaultWhenSpawnerSetsPermissionMode(t *testing.T) {
	// Spawner declares its own --permission-mode auto → dashboard must NOT append
	// a second --permission-mode default (claude errors on the duplicate flag).
	opts := pipeline.SpawnAgentOptions{
		Task:     &ent.Task{},
		StageRun: &ent.StageRun{},
		Prompt:   "p",
		Spawner:  &ent.Spawner{Command: "claude", Args: []string{"--permission-mode", "auto"}},
	}
	args := pipeline.BuildSpawnArgs(opts)
	require.NotContains(t, args, "default",
		"dashboard must not append --permission-mode default when the spawner sets its own mode")
}

func TestBuildSpawnArgs_AdditionalDirs(t *testing.T) {
	t.Run("no additional dirs produces no --add-dir flags", func(t *testing.T) {
		opts := pipeline.SpawnAgentOptions{
			Task:     &ent.Task{},
			StageRun: &ent.StageRun{},
			Prompt:   "p",
		}
		args := pipeline.BuildSpawnArgs(opts)
		for _, a := range args {
			require.NotEqual(t, "--add-dir", a)
		}
	})

	t.Run("each additional dir gets its own --add-dir flag", func(t *testing.T) {
		opts := pipeline.SpawnAgentOptions{
			Task:           &ent.Task{},
			StageRun:       &ent.StageRun{},
			Prompt:         "p",
			AdditionalDirs: []string{"/extra1", "/extra2"},
		}
		args := pipeline.BuildSpawnArgs(opts)

		var addDirValues []string
		for i, a := range args {
			if a == "--add-dir" && i+1 < len(args) {
				addDirValues = append(addDirValues, args[i+1])
			}
		}
		require.Equal(t, []string{"/extra1", "/extra2"}, addDirValues)
	})

	t.Run("empty string entries in AdditionalDirs are skipped", func(t *testing.T) {
		opts := pipeline.SpawnAgentOptions{
			Task:           &ent.Task{},
			StageRun:       &ent.StageRun{},
			Prompt:         "p",
			AdditionalDirs: []string{"", "/extra", ""},
		}
		args := pipeline.BuildSpawnArgs(opts)

		var addDirValues []string
		for i, a := range args {
			if a == "--add-dir" && i+1 < len(args) {
				addDirValues = append(addDirValues, args[i+1])
			}
		}
		require.Equal(t, []string{"/extra"}, addDirValues)
	})

	t.Run("--add-dir appears once per dir with correct values", func(t *testing.T) {
		opts := pipeline.SpawnAgentOptions{
			Task:           &ent.Task{},
			StageRun:       &ent.StageRun{},
			Prompt:         "p",
			AdditionalDirs: []string{"/a", "/b", "/c"},
		}
		args := pipeline.BuildSpawnArgs(opts)

		count := 0
		for _, a := range args {
			if a == "--add-dir" {
				count++
			}
		}
		require.Equal(t, 3, count)
	})
}

func TestBuildSpawnEnv_ExpandsTildeInSpawnerEnv(t *testing.T) {
	t.Setenv("HOME", "/tmp/fakehome")
	opts := pipeline.SpawnAgentOptions{
		Task:     &ent.Task{},
		StageRun: &ent.StageRun{},
		Prompt:   "p",
		Spawner: &ent.Spawner{
			Command: "claude",
			Env:     map[string]string{"CLAUDE_CONFIG_DIR": "~/.claude-work"},
		},
	}
	env := pipeline.BuildSpawnEnv(opts)
	require.Contains(t, env, "CLAUDE_CONFIG_DIR=/tmp/fakehome/.claude-work")
	require.NotContains(t, env, "CLAUDE_CONFIG_DIR=~/.claude-work")
}

func TestBuildSpawnEnv_DenyListExcludesSecrets(t *testing.T) {
	t.Setenv("DASHBOARD_JWT_SECRET", "super-secret-value")
	t.Setenv("DASHBOARD_HOOKS_SECRET", "hook-secret-value")

	opts := pipeline.SpawnAgentOptions{
		Task:     &ent.Task{ID: "t1"},
		StageRun: &ent.StageRun{ID: "r1"},
	}
	env := pipeline.BuildSpawnEnv(opts)

	for _, e := range env {
		require.False(t, strings.HasPrefix(e, "DASHBOARD_JWT_SECRET="),
			"DASHBOARD_JWT_SECRET must not appear in spawn env, got: %s", e)
		require.False(t, strings.HasPrefix(e, "DASHBOARD_HOOKS_SECRET="),
			"DASHBOARD_HOOKS_SECRET must not appear in spawn env, got: %s", e)
	}
}

func TestBuildSpawnEnv_ForwardsPath(t *testing.T) {
	t.Setenv("PATH", "/test/path:/usr/bin")

	opts := pipeline.SpawnAgentOptions{
		Task:     &ent.Task{ID: "t2"},
		StageRun: &ent.StageRun{ID: "r2"},
	}
	env := pipeline.BuildSpawnEnv(opts)

	require.Contains(t, env, "PATH=/test/path:/usr/bin")
}

func TestBuildSpawnEnv_InjectsDashboardIdentifiers(t *testing.T) {
	opts := pipeline.SpawnAgentOptions{
		Task:     &ent.Task{ID: "task-xyz"},
		StageRun: &ent.StageRun{ID: "run-abc"},
		MCPToken: "mcp-tok",
		MCPUrl:   "http://127.0.0.1:13120/api/mcp",
	}
	env := pipeline.BuildSpawnEnv(opts)

	require.Contains(t, env, "DASHBOARD_TASK_ID=task-xyz")
	require.Contains(t, env, "DASHBOARD_STAGE_RUN_ID=run-abc")
	require.Contains(t, env, "DASHBOARD_MCP_TOKEN=mcp-tok")
	require.Contains(t, env, "DASHBOARD_MCP_URL=http://127.0.0.1:13120/api/mcp")
}

func TestBuildSpawnEnv_ArbitraryVarsNotForwarded(t *testing.T) {
	t.Setenv("MY_SECRET_VAR", "leaked")

	opts := pipeline.SpawnAgentOptions{
		Task:     &ent.Task{ID: "t3"},
		StageRun: &ent.StageRun{ID: "r3"},
	}
	env := pipeline.BuildSpawnEnv(opts)

	for _, e := range env {
		require.False(t, strings.HasPrefix(e, "MY_SECRET_VAR="),
			"arbitrary env var must not be forwarded, got: %s", e)
	}
}

func TestBuildSpawnEnv_ForwardsDashboardPrefix(t *testing.T) {
	t.Setenv("DASHBOARD_MCP_URL", "http://example.com")
	t.Setenv("DASHBOARD_JWT_SECRET", "x") // deny-list must override prefix match

	opts := pipeline.SpawnAgentOptions{
		Task:     &ent.Task{ID: "t4"},
		StageRun: &ent.StageRun{ID: "r4"},
	}
	env := pipeline.BuildSpawnEnv(opts)

	// DASHBOARD_MCP_URL should be present (set via Setenv, then overridden by injection
	// since opts.MCPUrl is empty — but the env key itself must appear from prefix forwarding
	// or injection; what matters is the deny-listed secret is absent)
	for _, e := range env {
		require.False(t, strings.HasPrefix(e, "DASHBOARD_JWT_SECRET="),
			"DASHBOARD_JWT_SECRET must be blocked even with DASHBOARD_ prefix, got: %s", e)
	}

	// DASHBOARD_MCP_URL from the environment must be forwarded via prefix rule
	require.Contains(t, env, "DASHBOARD_MCP_URL=http://example.com")
}
