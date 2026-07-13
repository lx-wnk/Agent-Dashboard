package pipeline_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	allow := pipeline.BuildAllowList("manual", perms, false, false)
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
	allow := pipeline.BuildAllowList("manual", perms, false, true)
	require.Contains(t, allow, "Bash(git push origin HEAD)")
}

func TestBuildAllowList_FiltersDenied(t *testing.T) {
	perms := []*ent.TaskPermission{
		{Tool: "Bash", Granted: false},
		{Tool: "Read", Granted: true},
	}
	allow := pipeline.BuildAllowList("manual", perms, false, false)
	require.Contains(t, allow, "Read")
	for _, a := range allow {
		require.NotEqual(t, "Bash", a)
	}
}

func TestBuildAllowList_IncludesChannelTools(t *testing.T) {
	allow := pipeline.BuildAllowList("manual", nil, true, false)
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
	t.Setenv("DASHBOARD_SECRET_KEY", "master-key-value")
	t.Setenv("DASHBOARD_JWT_SECRET", "super-secret-value")
	t.Setenv("DASHBOARD_AUTH_PLUGIN_SECRET", "auth-plugin-secret-value")
	t.Setenv("DASHBOARD_HOOKS_SECRET", "hook-secret-value")

	opts := pipeline.SpawnAgentOptions{
		Task:     &ent.Task{ID: "t1"},
		StageRun: &ent.StageRun{ID: "r1"},
	}
	env := pipeline.BuildSpawnEnv(opts)

	for _, e := range env {
		require.False(t, strings.HasPrefix(e, "DASHBOARD_SECRET_KEY="),
			"DASHBOARD_SECRET_KEY must not appear in spawn env, got: %s", e)
		require.False(t, strings.HasPrefix(e, "DASHBOARD_JWT_SECRET="),
			"DASHBOARD_JWT_SECRET must not appear in spawn env, got: %s", e)
		require.False(t, strings.HasPrefix(e, "DASHBOARD_AUTH_PLUGIN_SECRET="),
			"DASHBOARD_AUTH_PLUGIN_SECRET must not appear in spawn env, got: %s", e)
		require.False(t, strings.HasPrefix(e, "DASHBOARD_HOOKS_SECRET="),
			"DASHBOARD_HOOKS_SECRET must not appear in spawn env, got: %s", e)
	}
}

// TestBuildSpawnEnv_MCPTokenSurvivesSecretStrip is the CQ-02 required
// assertion: the 4-key canonical deny-set (envsec.DeniedSecretEnvKeys) must
// be stripped from a pipeline agent's env while the per-task
// DASHBOARD_MCP_TOKEN injected at Stage 3 survives the final defense-in-depth
// delete loop — i.e. a live agent can still reach /api/mcp after the strip.
func TestBuildSpawnEnv_MCPTokenSurvivesSecretStrip(t *testing.T) {
	t.Setenv("DASHBOARD_SECRET_KEY", "master-key-value")
	t.Setenv("DASHBOARD_JWT_SECRET", "super-secret-value")
	t.Setenv("DASHBOARD_AUTH_PLUGIN_SECRET", "auth-plugin-secret-value")
	t.Setenv("DASHBOARD_HOOKS_SECRET", "hook-secret-value")
	t.Setenv("DASHBOARD_MCP_TOKEN", "leaked-ambient-token")

	opts := pipeline.SpawnAgentOptions{
		Task:     &ent.Task{ID: "t1"},
		StageRun: &ent.StageRun{ID: "r1"},
		MCPToken: "per-task-mcp-token",
		MCPUrl:   "http://127.0.0.1:13120/api/mcp",
	}
	env := pipeline.BuildSpawnEnv(opts)

	for _, denied := range []string{
		"DASHBOARD_SECRET_KEY",
		"DASHBOARD_JWT_SECRET",
		"DASHBOARD_AUTH_PLUGIN_SECRET",
		"DASHBOARD_HOOKS_SECRET",
	} {
		for _, e := range env {
			require.False(t, strings.HasPrefix(e, denied+"="),
				"%s must not appear in spawn env, got: %s", denied, e)
		}
	}

	// The Stage-3 per-task token must win over — and survive stripping
	// alongside — any ambient DASHBOARD_MCP_TOKEN inherited from os.Environ().
	require.Contains(t, env, "DASHBOARD_MCP_TOKEN=per-task-mcp-token")
	require.Contains(t, env, "DASHBOARD_MCP_URL=http://127.0.0.1:13120/api/mcp")
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

// ---------------------------------------------------------------------------
// BuildAllowList — ManualOverride bypass
// ---------------------------------------------------------------------------

func TestBuildAllowList_ManualOverride_BypassesAllowList(t *testing.T) {
	pattern := "chmod +x ./x.sh"
	perms := []*ent.TaskPermission{
		{Tool: "Bash", Pattern: &pattern, Granted: true, ManualOverride: true},
	}
	allow := pipeline.BuildAllowList("manual", perms, false, false)
	require.Contains(t, allow, "Bash(chmod +x ./x.sh)")
}

func TestBuildAllowList_NoManualOverride_StripsBlockedPattern(t *testing.T) {
	pattern := "chmod +x ./x.sh"
	perms := []*ent.TaskPermission{
		{Tool: "Bash", Pattern: &pattern, Granted: true, ManualOverride: false},
	}
	allow := pipeline.BuildAllowList("manual", perms, false, false)
	for _, a := range allow {
		if strings.Contains(a, "chmod") {
			t.Errorf("BuildAllowList without override must strip 'chmod' pattern, got: %s", a)
		}
	}
}

func TestBuildAllowList_ManualOverride_BypassesGitPushGate(t *testing.T) {
	pattern := "git push origin HEAD"
	perms := []*ent.TaskPermission{
		{Tool: "Bash", Pattern: &pattern, Granted: true, ManualOverride: true},
	}
	// allowGitPush=false — override must still pass.
	allow := pipeline.BuildAllowList("manual", perms, false, false)
	require.Contains(t, allow, "Bash(git push origin HEAD)")
}

func TestBuildAllowList_AllowAllAutonomy_ReturnsBlanketBash(t *testing.T) {
	for _, autonomy := range []string{"spec_gated", "full"} {
		allow := pipeline.BuildAllowList(autonomy, nil, false, false)
		require.Contains(t, allow, "Bash", "autonomy=%s must include blanket Bash", autonomy)
		require.Contains(t, allow, "Read", "autonomy=%s must include Read", autonomy)
	}
}

func TestBuildAllowList_ManualAutonomy_PreservesGatedBehaviour(t *testing.T) {
	// manual with no granted perms → only channel tools (if any), no Bash
	allow := pipeline.BuildAllowList("manual", nil, false, false)
	for _, a := range allow {
		require.NotEqual(t, "Bash", a, "manual autonomy with no perms must not include blanket Bash")
	}
}

func TestBuildAllowList_EmptyAutonomy_PreservesGatedBehaviour(t *testing.T) {
	allow := pipeline.BuildAllowList("", nil, false, false)
	for _, a := range allow {
		require.NotEqual(t, "Bash", a, "empty autonomy must not include blanket Bash")
	}
}

// ---------------------------------------------------------------------------
// BuildDenyList — git-push containment on the allow-all path
// ---------------------------------------------------------------------------

func TestBuildDenyList_AllowAll_GitPushDisabled_ReturnsDeny(t *testing.T) {
	for _, autonomy := range []string{"spec_gated", "full"} {
		deny := pipeline.BuildDenyList(autonomy, false)
		require.Contains(t, deny, "Bash(git push:*)",
			"autonomy=%s, allowGitPush=false must include deny entry", autonomy)
	}
}

func TestBuildDenyList_AllowAll_GitPushEnabled_ReturnsNil(t *testing.T) {
	for _, autonomy := range []string{"spec_gated", "full"} {
		deny := pipeline.BuildDenyList(autonomy, true)
		require.Empty(t, deny,
			"autonomy=%s, allowGitPush=true must return no deny entries", autonomy)
	}
}

func TestBuildDenyList_ManualAutonomy_AlwaysNil(t *testing.T) {
	// manual autonomy never triggers the deny-list path (git push is already
	// blocked at the allow-list level via gitPushRE gate).
	deny := pipeline.BuildDenyList("manual", false)
	require.Empty(t, deny, "manual autonomy must return no deny entries")
}

func TestWriteSettingsFile_AllowAll_GitPushDisabled_IncludesDeny(t *testing.T) {
	cwd := t.TempDir()
	path, wrote, isLocal, err := pipeline.ExportedWriteSettingsFile("spec_gated", cwd, nil, false, false)
	require.NoError(t, err)
	require.True(t, wrote)
	require.False(t, isLocal)
	require.NotEmpty(t, path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	perms, _ := parsed["permissions"].(map[string]any)
	deny, _ := perms["deny"].([]any)
	require.Contains(t, deny, "Bash(git push:*)", "settings.json must contain deny entry for git push")
}

func TestWriteSettingsFile_AllowAll_GitPushEnabled_NoDeny(t *testing.T) {
	cwd := t.TempDir()
	path, wrote, _, err := pipeline.ExportedWriteSettingsFile("spec_gated", cwd, nil, false, true)
	require.NoError(t, err)
	require.True(t, wrote)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	perms, _ := parsed["permissions"].(map[string]any)
	deny, _ := perms["deny"].([]any)
	require.Empty(t, deny, "settings.json must NOT contain deny when allowGitPush=true")
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

// ---------------------------------------------------------------------------
// CQ-06 — writeSettingsFile failure must fail loud under restrictive autonomy
// ---------------------------------------------------------------------------

// brokenSpawnCwd returns a path to a regular file (not a directory), which
// makes writeSettingsFile's os.MkdirAll(".claude") fail deterministically —
// independent of filesystem permissions or CI user privileges.
func brokenSpawnCwd(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
	return path
}

func TestSpawnStageAgent_CQ06_FailsLoudUnderRestrictiveAutonomy(t *testing.T) {
	opts := pipeline.SpawnAgentOptions{
		Task:          &ent.Task{ID: "t-restrictive", Cwd: brokenSpawnCwd(t), Autonomy: "manual"},
		StageRun:      &ent.StageRun{ID: "r-restrictive"},
		EnableChannel: true, // forces a non-empty allow list so writeSettingsFile attempts the write
		Spawner:       &ent.Spawner{Command: "/nonexistent/does-not-exist-xyz"},
	}
	_, err := pipeline.SpawnStageAgent(opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "writeSettingsFile:",
		"restrictive autonomy must fail loud on a writeSettingsFile error instead of spawning with no allow-list")
}

func TestSpawnStageAgent_CQ06_WarnsAndContinuesUnderAllowAllAutonomy(t *testing.T) {
	for _, autonomy := range []string{"spec_gated", "full"} {
		t.Run(autonomy, func(t *testing.T) {
			opts := pipeline.SpawnAgentOptions{
				Task:     &ent.Task{ID: "t-allow-all", Cwd: brokenSpawnCwd(t), Autonomy: autonomy},
				StageRun: &ent.StageRun{ID: "r-allow-all"},
				Spawner:  &ent.Spawner{Command: "/nonexistent/does-not-exist-xyz"},
			}
			_, err := pipeline.SpawnStageAgent(opts)
			// The spawn still fails overall (the spawner binary does not exist), but
			// it must fail at the process-start stage, not at writeSettingsFile — proving
			// the writeSettingsFile error was logged and swallowed, not returned.
			require.Error(t, err)
			require.NotContains(t, err.Error(), "writeSettingsFile:",
				"allow-all autonomy must warn and continue past a writeSettingsFile error")
		})
	}
}
