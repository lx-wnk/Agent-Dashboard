package agents

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/lx-wnk/kontor/server/internal/db/ent"
	"github.com/lx-wnk/kontor/server/internal/services"
)

// stubPIDExec replaces the transport with `sh -c 'echo $$'`: the pty path
// reads the printed pid, then the watcher sees sh exit. No agent is started.
func stubPIDExec(t *testing.T) *[]string {
	t.Helper()
	prevLook := lookTmuxPath
	lookTmuxPath = func() string { return "" }
	sh, err := exec.LookPath("sh")
	require.NoError(t, err)
	var captured []string
	orig := execStart
	execStart = func(cmd *exec.Cmd) error {
		captured = slices.Clone(cmd.Args)
		cmd.Path, cmd.Args, cmd.Err = sh, []string{sh, "-c", "echo $$"}, nil
		return cmd.Start()
	}
	t.Cleanup(func() { execStart, lookTmuxPath = orig, prevLook })
	return &captured
}

func sessionHome(t *testing.T) string {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Setenv("HOME", home)
	return home
}

func TestSpawnSession_BuildsKontorArgsAndReportsExit(t *testing.T) {
	home := sessionHome(t)
	captured := stubPIDExec(t)
	cwd := filepath.Join(home, "session")
	require.NoError(t, os.MkdirAll(cwd, 0o700))

	m := NewSpawnManager(0, 0, 0, 0, nil, services.NewSpawnPolicy(nil))
	exited := make(chan int, 1)
	pid, err := m.SpawnSession(t.Context(), SessionSpawnOptions{
		Cwd: cwd, Prompt: "hallo", AppendSystemPrompt: "brief", Name: "Kontor",
		MCPConfigPath: "/tmp/kontor.json",
		AllowedTools:  []string{"mcp__kontor-tasks__get_task", "mcp__kontor-tasks__list_tasks"},
		OnExit:        func(p int) { exited <- p },
	})
	require.NoError(t, err)
	require.Positive(t, pid)

	args := *captured
	i := slices.Index(args, "--session-id")
	require.GreaterOrEqual(t, i, 0)
	_, err = uuid.Parse(args[i+1])
	require.NoError(t, err)
	require.True(t, containsConsecutive(args, "--permission-mode", "default"))
	require.True(t, containsConsecutive(args, "--append-system-prompt", "brief"))
	require.True(t, containsConsecutive(args, "-n", "Kontor"))
	require.True(t, containsConsecutive(args, "--allowedTools", "mcp__kontor-tasks__get_task"))
	require.True(t, containsConsecutive(args, "mcp__kontor-tasks__get_task", "mcp__kontor-tasks__list_tasks"))
	require.True(t, containsConsecutive(args, "--mcp-config", "/tmp/kontor.json"))
	require.Equal(t, []string{"--strict-mcp-config", "hallo"}, args[len(args)-2:],
		"the prompt must follow a boolean flag, never the variadic --allowedTools/--mcp-config")

	select {
	case got := <-exited:
		require.Equal(t, pid, got)
	case <-time.After(10 * time.Second):
		t.Fatal("OnExit was not called after the process exited")
	}
}

func TestSpawnSession_PolicyRefusalSpawnsNothing(t *testing.T) {
	home := sessionHome(t)
	captured := stubPIDExec(t)
	cwd := filepath.Join(home, ".ssh")
	require.NoError(t, os.MkdirAll(cwd, 0o700))

	m := NewSpawnManager(0, 0, 0, 0, nil, services.NewSpawnPolicy(nil))
	_, err := m.SpawnSession(t.Context(), SessionSpawnOptions{Cwd: cwd, Prompt: "x", Name: "Kontor", MCPConfigPath: "/tmp/k.json"})
	require.ErrorIs(t, err, services.ErrCwdBlacklisted)
	require.Nil(t, *captured)
}

// The operator's default spawner runs --permission-mode auto; a Kontor session
// must still prompt before a write, so the spawner's posture is replaced.
func TestSpawnSession_OverridesTheSpawnersPermissionPosture(t *testing.T) {
	home := sessionHome(t)
	captured := stubPIDExec(t)
	stored := []string{"--permission-mode", "auto", "--dangerously-skip-permissions", "--verbose"}
	row := &ent.Spawner{ID: "d", Name: "auto", AdapterType: "claude", Command: "claude", IsDefault: true, Args: slices.Clone(stored)}
	m := NewSpawnManager(0, 0, 0, 0, &fakeSpawnerRepo{byID: map[string]*ent.Spawner{"d": row}}, services.NewSpawnPolicy(nil))

	_, err := m.SpawnSession(t.Context(), SessionSpawnOptions{Cwd: home, Name: "Kontor", MCPConfigPath: "/tmp/k.json"})
	require.NoError(t, err)
	args := *captured
	require.True(t, containsConsecutive(args, "--permission-mode", "default"))
	require.NotContains(t, args, "auto")
	require.NotContains(t, args, "--dangerously-skip-permissions")
	require.Contains(t, args, "--verbose", "other spawner args survive")
	require.Equal(t, "--strict-mcp-config", args[len(args)-1], "an empty prompt adds no positional argument")
	require.Equal(t, stored, row.Args, "the stored spawner row is not mutated")
}

func TestTerminateSession_RefusesAPidItDidNotSpawn(t *testing.T) {
	sessionHome(t)
	sleeper := exec.Command("sleep", "30")
	require.NoError(t, sleeper.Start())
	t.Cleanup(func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() })

	m := NewSpawnManager(0, 0, 0, 0, nil, services.NewSpawnPolicy(nil))
	require.Error(t, m.TerminateSession(sleeper.Process.Pid))
	require.True(t, ProcessAlive(sleeper.Process.Pid))
}
