# Interactive Live Dashboard Spawns — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make dashboard-spawned agents persistent, live-injectable interactive sessions (like `agent-dashboard live`) instead of one-shot `-p` runs, by launching the spawner's command under a tmux session (preferred) or a headless pty (fallback).

**Architecture:** `SpawnManager.Spawn` keeps resolving the spawner's `(binary, args, env, cwd)` but (a) seeds the prompt as a positional arg instead of `-p` (interactive), and (b) launches via a headless transport: tmux `new-session -d` inline (returns the pane PID; bridge records `tmuxPane` → live via existing send-keys), or a detached `agent-dashboard pty-host` subprocess that owns a pty + the existing injection HTTP. Exit/cfg-cleanup tracking polls the resolved claude PID (tmux) or waits on the subprocess (pty).

**Tech Stack:** Go 1.26 (`creack/pty` already vendored — see ptyhost.go), cobra CLI, chi; existing `channel/ptyhost.go` helpers (`startPtyHTTPServer`, `writePtyDiscovery`, `newRotatingToken`, `startTokenRotation`).

**Spec:** `docs/superpowers/specs/2026-06-22-interactive-live-spawns-design.md`

---

## File structure

- `server/internal/api/agents/spawn.go` — positional prompt (`buildSpawnArgs`); transport launch + PID capture + watchers (`Spawn`, new `launchInteractive`).
- `server/internal/api/agents/transport.go` — **new**: headless transport selection + tmux argv builder + pane-pid parse (pure, unit-tested).
- `server/internal/channel/headlesspty.go` — **new**: `RunHeadlessPTY` (drain variant of `RunPTY`).
- `server/cmd/serve/ptyhost.go` — **new**: `agent-dashboard pty-host` cobra subcommand wrapping `RunHeadlessPTY`.
- `server/cmd/serve/root.go` (or wherever subcommands register) — register `pty-host`.
- Tests alongside each.

---

## Task 1: Interactive args — positional prompt, drop `-p`

**Files:**
- Modify: `server/internal/api/agents/spawn.go:294` (in `buildSpawnArgs`)
- Test: `server/internal/api/agents/spawn_test.go`

- [ ] **Step 1: Write the failing test**

Add to `spawn_test.go`:

```go
func TestBuildSpawnArgs_InteractivePositionalPrompt(t *testing.T) {
	m := &SpawnManager{}
	binary, args, err := m.buildSpawnArgs(&spawnRequest{prompt: "hello world", permissionMode: "default"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if binary != claudeBin {
		t.Errorf("binary = %q", binary)
	}
	for _, a := range args {
		if a == "-p" {
			t.Fatal("must not pass -p (interactive mode)")
		}
	}
	// prompt is the last positional arg
	if args[len(args)-1] != "hello world" {
		t.Errorf("last arg = %q, want the prompt positional", args[len(args)-1])
	}
}
```

- [ ] **Step 2: Run → fail**

Run: `cd server && go test ./internal/api/agents/ -run TestBuildSpawnArgs_Interactive -v`
Expected: FAIL (currently `-p` present, prompt not last positional).

- [ ] **Step 3: Implement**

In `buildSpawnArgs`, the current line builds `canonicalArgs = append(canonicalArgs, "-p", req.prompt)`. Remove the `-p` and the inline prompt there; instead append the prompt as the final positional **after** all flags. Change the assembly at the end of the function:

```go
	// Order: spawner args, then canonical flags, then the prompt as a trailing
	// positional so claude starts an interactive session seeded with it (no -p).
	args = append(spawnerArgs, canonicalArgs...)
	if req.prompt != "" {
		args = append(args, req.prompt)
	}
	return binary, args, nil
```

And remove `"-p", req.prompt` from the `canonicalArgs` construction (keep `--resume`, `--model`, `--system-prompt`, `--permission-mode`, `--add-dir` as-is).

- [ ] **Step 4: Run → pass**

Run: `cd server && go test ./internal/api/agents/ -run TestBuildSpawnArgs -v` → PASS. Also run the whole package: `go test ./internal/api/agents/` (existing spawn tests may assert `-p`; if any do, update them to expect the positional prompt — that is the intended behaviour change).

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/agents/spawn.go server/internal/api/agents/spawn_test.go
git commit -S -m "feat: spawn claude interactively (positional prompt, no -p)"
```

---

## Task 2: Headless transport selection + tmux argv builder (pure helpers)

**Files:**
- Create: `server/internal/api/agents/transport.go`
- Test: `server/internal/api/agents/transport_test.go`

- [ ] **Step 1: Write the failing test**

`transport_test.go`:

```go
package agents

import (
	"reflect"
	"testing"
)

func TestSelectHeadlessTransport(t *testing.T) {
	if got := selectHeadlessTransport("/usr/bin/tmux"); got != transportTmux {
		t.Errorf("tmux present → %v, want transportTmux", got)
	}
	if got := selectHeadlessTransport(""); got != transportPTY {
		t.Errorf("no tmux → %v, want transportPTY", got)
	}
}

func TestBuildTmuxArgs(t *testing.T) {
	got := buildTmuxArgs("claude-spawn-x", []string{"FOO=bar", "BAZ=qux"}, "claude", []string{"--model", "opus", "hi there"})
	want := []string{
		"new-session", "-d", "-P", "-F", "#{pane_pid}", "-s", "claude-spawn-x",
		"env", "FOO=bar", "BAZ=qux", "claude", "--model", "opus", "hi there",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestParsePanePID(t *testing.T) {
	pid, err := parsePanePID("48213\n")
	if err != nil || pid != 48213 {
		t.Fatalf("pid=%d err=%v", pid, err)
	}
	if _, err := parsePanePID("not-a-pid"); err == nil {
		t.Error("expected error on non-numeric output")
	}
}
```

- [ ] **Step 2: Run → fail**

Run: `cd server && go test ./internal/api/agents/ -run 'Transport|TmuxArgs|PanePID' -v`
Expected: FAIL (undefined).

- [ ] **Step 3: Implement**

`transport.go`:

```go
package agents

import (
	"fmt"
	"strconv"
	"strings"
)

// headlessTransport is the live-injection transport for a server-spawned
// (terminal-less) agent.
type headlessTransport int

const (
	transportTmux headlessTransport = iota
	transportPTY
)

// selectHeadlessTransport picks tmux when it is on PATH, else the pty broker.
// tmuxPath is exec.LookPath("tmux") ("" on failure). Unlike the live command we
// never reuse the server's own $TMUX pane — a spawned agent always gets a fresh
// detached session.
func selectHeadlessTransport(tmuxPath string) headlessTransport {
	if tmuxPath != "" {
		return transportTmux
	}
	return transportPTY
}

// buildTmuxArgs builds the `tmux …` argv that starts a detached session running
// `binary args…` with env applied via an `env K=V…` wrapper (argv form, no
// shell). -P -F '#{pane_pid}' makes tmux print the pane command's PID.
func buildTmuxArgs(session string, env []string, binary string, args []string) []string {
	out := []string{"new-session", "-d", "-P", "-F", "#{pane_pid}", "-s", session, "env"}
	out = append(out, env...)
	out = append(out, binary)
	out = append(out, args...)
	return out
}

// parsePanePID parses the `#{pane_pid}` value tmux prints to stdout.
func parsePanePID(out string) (int, error) {
	pid, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return 0, fmt.Errorf("parse pane pid %q: %w", out, err)
	}
	return pid, nil
}
```

- [ ] **Step 4: Run → pass**

Run: `cd server && go test ./internal/api/agents/ -run 'Transport|TmuxArgs|PanePID' -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/agents/transport.go server/internal/api/agents/transport_test.go
git commit -S -m "feat: headless transport selection + tmux argv builder"
```

---

## Task 3: `RunHeadlessPTY` (drain-mode pty broker)

**Files:**
- Create: `server/internal/channel/headlesspty.go`
- Test: `server/internal/channel/headlesspty_test.go`

- [ ] **Step 1: Write the failing test**

`headlesspty_test.go` — run a trivial long-lived child on the pty, assert the discovery file is written with `ptyInject:true` and the child PID is reported, then end the child and assert cleanup:

```go
package channel

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/lx-wnk/agent-dashboard/server/internal/channelconfig"
)

func TestRunHeadlessPTY_WritesDiscoveryAndReportsPID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pidCh := make(chan int, 1)
	done := make(chan error, 1)
	go func() {
		// `cat` stays alive reading its pty stdin until the pty closes.
		done <- RunHeadlessPTY(ctx, "cat", nil, nil, "", func(pid int) { pidCh <- pid })
	}()

	var childPID int
	select {
	case childPID = <-pidCh:
	case <-time.After(5 * time.Second):
		t.Fatal("RunHeadlessPTY never reported a pid")
	}
	if childPID <= 0 {
		t.Fatalf("bad child pid %d", childPID)
	}

	discFile := filepath.Join(home, channelconfig.DiscoveryDir, strconv.Itoa(childPID)+".pty.json")
	// Poll briefly for the discovery file.
	var data []byte
	for i := 0; i < 50; i++ {
		if b, err := os.ReadFile(discFile); err == nil {
			data = b
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if data == nil {
		t.Fatalf("pty discovery file not written: %s", discFile)
	}
	if !contains(string(data), `"ptyInject":true`) {
		t.Errorf("discovery missing ptyInject:true: %s", data)
	}

	cancel() // ends the child + triggers cleanup
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunHeadlessPTY did not return after cancel")
	}
	if _, err := os.Stat(discFile); !os.IsNotExist(err) {
		t.Errorf("discovery file not cleaned up")
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (func() bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
})() }
```

- [ ] **Step 2: Run → fail**

Run: `cd server && go test ./internal/channel/ -run TestRunHeadlessPTY -v`
Expected: FAIL (undefined `RunHeadlessPTY`).

- [ ] **Step 3: Implement**

`headlesspty.go` — mirror `RunPTY` but: set `cmd.Env`/`cmd.Dir`, no raw-terminal/winch/stdin-proxy, drain output to discard, report the pid via callback:

```go
package channel

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/creack/pty"
)

// RunHeadlessPTY runs `name args…` on a pseudo-terminal the dashboard owns (no
// controlling terminal to proxy), serving the same loopback-token injection HTTP
// and {pid}.pty.json discovery as RunPTY so the dashboard's existing /message
// delivery works. Output is drained (the agent has no human-facing terminal).
// onPid, when non-nil, is called once with the child's PID. Returns when the
// child exits or ctx is cancelled.
func RunHeadlessPTY(ctx context.Context, name string, args, env []string, cwd string, onPid func(int)) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if env != nil {
		cmd.Env = env
	}
	if cwd != "" {
		cmd.Dir = cwd
	}
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("headlesspty: start: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	childPid := cmd.Process.Pid
	if onPid != nil {
		onPid(childPid)
	}

	initialToken, err := generateToken()
	if err != nil {
		return fmt.Errorf("headlesspty: token: %w", err)
	}
	token := newRotatingToken(initialToken)
	srv, port, err := startPtyHTTPServer(ptmx, token)
	if err != nil {
		return fmt.Errorf("headlesspty: http: %w", err)
	}
	discPath, derr := writePtyDiscovery(childPid, port, token.value())
	if derr != nil {
		slog.Warn("headlesspty: discovery write failed", "err", derr)
	}
	go startTokenRotation(ctx, token, injectTokenRotateInterval(), func(newToken string) error {
		_, werr := writePtyDiscovery(childPid, port, newToken)
		return werr
	})
	defer func() {
		if discPath != "" {
			_ = os.Remove(discPath)
		}
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	// Drain output so the child never blocks on a full pty buffer.
	go func() { _, _ = io.Copy(io.Discard, ptmx) }()

	return cmd.Wait()
}
```

Add the `exec` import (`os/exec`). Confirm `pty`, `generateToken`, `newRotatingToken`, `startPtyHTTPServer`, `writePtyDiscovery`, `startTokenRotation`, `injectTokenRotateInterval` are all in package `channel` (they are — used by RunPTY/bridge).

- [ ] **Step 4: Run → pass**

Run: `cd server && go test ./internal/channel/ -run TestRunHeadlessPTY -v` → PASS. Then `go test ./internal/channel/`.

- [ ] **Step 5: Commit**

```bash
git add server/internal/channel/headlesspty.go server/internal/channel/headlesspty_test.go
git commit -S -m "feat: RunHeadlessPTY drain-mode pty broker"
```

---

## Task 4: `agent-dashboard pty-host` subcommand

**Files:**
- Create: `server/cmd/serve/ptyhost.go`
- Modify: wherever cobra subcommands are registered (grep `newLiveCmd()` to find the parent `AddCommand` site, e.g. `server/cmd/serve/root.go`)

- [ ] **Step 1: Implement the subcommand**

`server/cmd/serve/ptyhost.go`:

```go
package main

import (
	"fmt"

	"github.com/lx-wnk/agent-dashboard/server/internal/channel"
	"github.com/spf13/cobra"
)

// newPtyHostCmd builds `agent-dashboard pty-host -- <binary> <args…>`. It runs
// the command on a dashboard-owned pty (headless), serving live injection, and
// prints the child PID as the first stdout line so the spawner can capture it.
func newPtyHostCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "pty-host [-- command args...]",
		Short:              "Run a command on a dashboard-owned pty for headless live injection",
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 && args[0] == "--" {
				args = args[1:]
			}
			if len(args) == 0 {
				return fmt.Errorf("pty-host: no command given")
			}
			return channel.RunHeadlessPTY(cmd.Context(), args[0], args[1:], nil, "", func(pid int) {
				fmt.Println(pid) // first stdout line = child PID (spawner reads it)
			})
		},
	}
}
```

(env/cwd are inherited from the process the spawner launches with `cmd.Env`/`cmd.Dir`, so `RunHeadlessPTY` is called with `nil, ""` here.)

- [ ] **Step 2: Register it**

Grep: `cd server && grep -rn "newLiveCmd()" cmd/serve/`. At the `AddCommand(newLiveCmd())` site add `newPtyHostCmd()`:

```go
rootCmd.AddCommand(newLiveCmd(), newPtyHostCmd())
```
(Adapt to the actual call — it may be separate `AddCommand` lines.)

- [ ] **Step 3: Verify build + manual smoke**

Run: `cd server && go build ./...` → clean.
Smoke (optional, not a unit test): `./<built-binary> pty-host -- cat` prints a PID line and stays alive; `Ctrl-C` exits. Skip if no built binary handy.

- [ ] **Step 4: Commit**

```bash
git add server/cmd/serve/ptyhost.go server/cmd/serve/root.go
git commit -S -m "feat: add pty-host subcommand for headless live injection"
```

---

## Task 5: Wire `SpawnManager.Spawn` to the transports

**Files:**
- Modify: `server/internal/api/agents/spawn.go` (the `Spawn` launch block ~lines 422-455; add `launchInteractive` helper; adjust watcher)
- Test: `server/internal/api/agents/spawn_test.go`

This task introduces a launch seam so tests assert the chosen command without spawning tmux/claude — mirror the existing `execStart` capture pattern.

- [ ] **Step 1: Write the failing test (tmux path builds the right command)**

Add a seam + test. In `spawn.go` near `execStart`, add:

```go
// lookTmuxPath is a seam so tests can force tmux present/absent.
var lookTmuxPath = func() string { p, _ := exec.LookPath("tmux"); return p }
```

Test (captures the command via the existing `execStart` seam — see `captureExec` in spawn_test.go):

```go
func TestSpawn_TmuxTransportBuildsInteractiveSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// force tmux present
	prevLook := lookTmuxPath
	lookTmuxPath = func() string { return "/usr/bin/tmux" }
	t.Cleanup(func() { lookTmuxPath = prevLook })

	captured := captureExec(t) // swaps execStart, records *exec.Cmd, returns true-binary
	mgr := newTestSpawnManager(t) // existing helper / construct with fakeSpawnerRepo + policy

	_, err := mgr.Spawn("__global__", map[string]any{"prompt": "do the thing", "cwd": t.TempDir()})
	require.NoError(t, err)

	cmd := *captured
	if filepath.Base(cmd.Path) != "tmux" {
		t.Fatalf("expected tmux launch, got %s", cmd.Path)
	}
	joined := strings.Join(cmd.Args, " ")
	if !strings.Contains(joined, "new-session -d -P -F #{pane_pid}") {
		t.Errorf("missing detached pane-pid session: %v", cmd.Args)
	}
	if !strings.Contains(joined, "do the thing") {
		t.Errorf("prompt not seeded positionally: %v", cmd.Args)
	}
	if strings.Contains(joined, " -p ") {
		t.Errorf("must be interactive, not -p: %v", cmd.Args)
	}
}
```

> Inspect `spawn_test.go` for the real `captureExec` signature and the existing way a `SpawnManager` is constructed in tests; adapt the helper names. If `captureExec` returns the `*exec.Cmd` differently, match it. The assertions (tmux binary, detached pane-pid args, positional prompt, no `-p`) are the contract.

- [ ] **Step 2: Run → fail**

Run: `cd server && go test ./internal/api/agents/ -run TestSpawn_TmuxTransport -v`
Expected: FAIL (Spawn still launches the bare binary, not tmux).

- [ ] **Step 3: Implement the launch branch**

Replace the launch portion of `Spawn` (currently `cmd := exec.Command(binary, args...)` … `execStart(cmd)` … `pid := cmd.Process.Pid` … `go m.watchProcess(...)`) with transport dispatch. After `args` include `--mcp-config` (existing channel block), and `env := resolveSpawnEnv(spawnerRow)`:

```go
	env := resolveSpawnEnv(spawnerRow)
	pid, watch, err := m.launchInteractive(binary, args, env, req.cwd, channelCfgPath)
	if err != nil {
		if channelCfgPath != "" {
			_ = os.Remove(channelCfgPath)
		}
		return 0, fmt.Errorf("spawn failed: %w", err)
	}
	status := &SpawnStatus{PID: pid, Status: "running", StartedAt: time.Now().UTC().Format(time.RFC3339), Prompt: req.prompt[:min(len(req.prompt), 200)], Cwd: req.cwd}
	if spawnerRow != nil {
		status.SpawnerID = spawnerRow.ID
	}
	m.mu.Lock()
	m.spawnStore[pid] = status
	m.mu.Unlock()
	go watch()
	return pid, nil
```

Add `launchInteractive` (same file):

```go
// launchInteractive starts the resolved command under a headless live transport
// (tmux session, else pty-host subprocess) and returns the agent PID plus a
// watch func that blocks until the agent exits and then cleans up.
func (m *SpawnManager) launchInteractive(binary string, args, env []string, cwd, channelCfgPath string) (int, func(), error) {
	switch selectHeadlessTransport(lookTmuxPath()) {
	case transportTmux:
		session := "claude-spawn-" + newSpawnID()
		tmuxArgs := buildTmuxArgs(session, env, binary, args)
		cmd := exec.Command("tmux", tmuxArgs...)
		cmd.Dir = cwd
		out, err := captureStdout(cmd) // runs execStart-equivalent; returns stdout
		if err != nil {
			return 0, nil, err
		}
		pid, err := parsePanePID(out)
		if err != nil {
			return 0, nil, err
		}
		return pid, m.pollExitWatch(pid, channelCfgPath), nil
	default: // transportPTY
		self, err := channelconfig.SelfBinaryPath()
		if err != nil {
			return 0, nil, err
		}
		hostArgs := append([]string{"pty-host", "--", binary}, args...)
		cmd := exec.Command(self, hostArgs...)
		cmd.Dir = cwd
		cmd.Env = env
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		pidPipe, _ := cmd.StdoutPipe()
		if err := execStart(cmd); err != nil {
			return 0, nil, err
		}
		pid, err := readPID(pidPipe) // reads first stdout line
		if err != nil {
			_ = cmd.Process.Kill()
			return 0, nil, err
		}
		return pid, m.subprocessExitWatch(cmd, channelCfgPath), nil
	}
}
```

Helpers (same file): `newSpawnID()` (random/uuid — reuse an existing id helper or `crypto/rand` hex), `captureStdout(cmd)` (set `cmd.Stdout` to a buffer, run via `execStart`, return string), `readPID(io.Reader)` (read first line, `parsePanePID`-style int), `pollExitWatch(pid, cfg)` (loop: `syscall.Kill(pid,0)`; on ESRCH → remove cfg + mark status "exited"; sleep ~2s), `subprocessExitWatch(cmd, cfg)` (`cmd.Wait()`; then remove cfg + mark status).

> Keep using the existing `execStart` seam inside `captureStdout` and the pty branch so `captureExec` in tests intercepts both. For the tmux branch, `captureStdout` must route through `execStart` (so tests capture the `tmux` cmd); have it set `cmd.Stdout` then call `execStart(cmd)` then `cmd.Wait()` (tmux client exits immediately). Reconcile with how `captureExec` stubs `execStart` (it may not actually run the process — in that case `captureStdout` returns empty and the test asserts the *command*, not the pid; gate the pid-parse so a stubbed run doesn't error, e.g. return pid 0 when stdout empty under test). Match the existing test seam's behaviour precisely when implementing.

- [ ] **Step 4: Run tests**

Run: `cd server && go test ./internal/api/agents/ -count=1` → all PASS (the new tmux test + existing spawn tests; update any existing test that assumed the old bare-detached launch or `-p`).

- [ ] **Step 5: Commit**

```bash
git add server/internal/api/agents/spawn.go server/internal/api/agents/spawn_test.go
git commit -S -m "feat: launch dashboard spawns under tmux/pty for live injection"
```

---

## Task 6: Message wording + docs

**Files:**
- Modify: `src/components/PromptInput.vue:328` (the "Not live-injectable" copy)
- Modify: `README.md`, `CHANGELOG.md`

- [ ] **Step 1: Reword the fallback message**

The message now only appears for genuinely non-live sessions (e.g. external resume targets). Keep it accurate; drop the implication that dashboard spawns need `agent-dashboard live` (they are live now). Update copy in `PromptInput.vue:328` to something like: "Not live-injectable — sending resumes this session as a new session." (Remove the `agent-dashboard live` sentence, or keep it only for the no-tmux case.) Verify the surrounding template still renders (`pnpm typecheck`).

- [ ] **Step 2: CHANGELOG + README**

`CHANGELOG.md` Unreleased → Added: "Dashboard-spawned agents now run as interactive live sessions (tmux or headless pty), so you can converse with them live instead of a one-shot run."
`README.md`: in the spawn/agent section, note spawns are interactive live sessions and live-injection uses tmux when available (pty otherwise).

- [ ] **Step 3: Verify gate**

Run: `pnpm lint && pnpm typecheck && pnpm test` (frontend) and `cd server && go build ./... && go test ./... -count=1`. All green.

- [ ] **Step 4: Commit**

```bash
git add src/components/PromptInput.vue README.md CHANGELOG.md
git commit -S -m "docs: interactive live spawns + reword resume-mode hint"
```

---

## Final verification

- [ ] `cd server && go build ./... && go test ./... -count=1` (incl. `-race` on channel + agents) — PASS
- [ ] `pnpm lint && pnpm typecheck && pnpm test` — PASS
- [ ] Manual (optional): spawn from the dashboard on a tmux host → agent shows live-injectable; type a follow-up → delivered via send-keys; on a non-tmux host → pty path, still live.

---

## Notes / decisions (from spec)

- Interactive by default; one-shot `-p` removed for dashboard spawns.
- tmux primary (no persistent owner — tmux server owns the session), headless-pty via a **detached subprocess** (survives dashboard restart).
- Spawner respected — transport wraps the resolved `(env, binary, args)`.
- PID: tmux `#{pane_pid}` / pty subprocess first stdout line. Exit/cfg-cleanup: poll signal-0 (tmux) / `cmd.Wait` (pty).
- No real-claude/real-tmux tests — use the `execStart`/`lookTmuxPath` seams and a trivial child (`cat`) for the pty unit test.
