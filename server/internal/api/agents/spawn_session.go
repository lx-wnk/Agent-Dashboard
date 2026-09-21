package agents

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/lx-wnk/kontor/server/internal/channelconfig"
	"github.com/lx-wnk/kontor/server/internal/db/ent"
)

// SessionSpawnOptions describe a server-owned interactive claude session.
// MCPConfigPath belongs to the caller: the manager neither writes nor removes it.
type SessionSpawnOptions struct {
	Cwd                string
	Prompt             string
	AppendSystemPrompt string
	Name               string
	MCPConfigPath      string
	AllowedTools       []string
	OnExit             func(pid int)
}

// SpawnSession starts the default spawner's native claude for a server-owned
// session. OnExit runs once the process is gone, with no deadline: unlike
// pollExitWatch, a session has no idle timeout.
func (m *SpawnManager) SpawnSession(ctx context.Context, opts SessionSpawnOptions) (int, error) {
	if err := m.spawnPolicy.Allow(ctx, opts.Cwd); err != nil {
		return 0, err
	}
	row, err := m.defaultSpawner(ctx)
	if err != nil {
		return 0, err
	}
	if !nativeClaudeAdapter(row) {
		return 0, fmt.Errorf("default spawner %q is not a claude adapter", row.Name)
	}
	row = withoutPermissionPosture(row)
	req := &spawnRequest{cwd: opts.Cwd, permissionMode: "default"}
	if row != nil && row.ModelOverride != nil {
		req.model = *row.ModelOverride
	}
	binary, args, err := m.buildSpawnArgs(req, row)
	if err != nil {
		return 0, err
	}
	args = append(args, "--append-system-prompt", opts.AppendSystemPrompt, "-n", opts.Name)
	if len(opts.AllowedTools) > 0 {
		args = append(append(args, "--allowedTools"), opts.AllowedTools...)
	}
	args = append(args, "--mcp-config", opts.MCPConfigPath, "--strict-mcp-config")
	if opts.Prompt != "" {
		args = append(args, opts.Prompt)
	}

	pid, watch, err := m.launchInteractive(binary, args, resolveSpawnEnv(row), opts.Cwd, "")
	if err != nil {
		return 0, fmt.Errorf("spawn failed: %w", err)
	}
	if pid == 0 {
		return 0, errors.New("spawn failed: the transport reported no pid")
	}
	m.mu.Lock()
	m.spawnStore[pid] = &SpawnStatus{PID: pid, Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339),
		Prompt: opts.Prompt[:min(len(opts.Prompt), 200)], Cwd: opts.Cwd}
	m.mu.Unlock()
	go func() {
		watch()
		WaitForExit(pid)
		if opts.OnExit != nil {
			opts.OnExit(pid)
		}
	}()
	return pid, nil
}

func (m *SpawnManager) defaultSpawner(ctx context.Context) (*ent.Spawner, error) {
	if m.spawnerRepo == nil {
		return nil, nil
	}
	row, err := m.spawnerRepo.GetDefault(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("default spawner lookup failed: %w", err)
	}
	return row, nil
}

// withoutPermissionPosture returns a copy of row without the flags
// spawnerArgsControlPermissionMode recognises, so buildSpawnArgs appends the
// session's own --permission-mode. The stored row is never mutated.
func withoutPermissionPosture(row *ent.Spawner) *ent.Spawner {
	if row == nil || !spawnerArgsControlPermissionMode(row.Args) {
		return row
	}
	clone := *row
	clone.Args = nil
	for i := 0; i < len(row.Args); i++ {
		a := row.Args[i]
		switch {
		case a == "--permission-mode":
			i++ // skip its value
		case strings.HasPrefix(a, "--permission-mode="),
			a == "--dangerously-skip-permissions",
			a == "--allow-dangerously-skip-permissions":
		default:
			clone.Args = append(clone.Args, a)
		}
	}
	return &clone
}

// TerminateSession sends SIGTERM to a session this dashboard started: one
// still running in this manager, or one whose channel bridge wrote a
// discovery file (a session reattached after a restart). A bare pid could
// by then belong to an unrelated process.
func (m *SpawnManager) TerminateSession(pid int) error {
	if pid <= 1 {
		return fmt.Errorf("agents: refusing to signal pid %d", pid)
	}
	s := m.GetStatus(pid)
	if (s == nil || s.Status != "running") && !hasDiscoveryFile(pid) {
		return fmt.Errorf("agents: pid %d is not a session this dashboard spawned", pid)
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err == nil {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

func hasDiscoveryFile(pid int) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(channelconfig.DiscoveryFile(home, pid))
	return err == nil
}

func ProcessAlive(pid int) bool { return processAlive(pid) }

func WaitForExit(pid int) {
	for processAlive(pid) {
		time.Sleep(2 * time.Second)
	}
}
