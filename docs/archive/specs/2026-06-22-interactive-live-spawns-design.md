# Interactive live dashboard spawns

**Date:** 2026-06-22
**Branch:** `feat/spawn-live-ux`
**Status:** Design — awaiting review

## Problem

Agents spawned via the dashboard (`POST /api/agents/spawn`) run claude in `-p`
(print) mode: a one-shot that processes the prompt and exits. So a spawned agent
is never an interactive session — it shows "Not live-injectable — sending
resumes this session as a new session" and disappears when it finishes. The user
wants dashboard spawns to behave like terminal agents (`agent-dashboard live`):
a persistent **interactive** session they can converse with live.

## Root cause

Two facts combine:

1. `buildSpawnArgs` (spawn.go) always appends `"-p", req.prompt` → one-shot print mode.
2. Even interactive, live injection needs a tty: the dashboard injects via tmux
   `send-keys` (reads the pane's tty) or the pty broker's HTTP endpoint. A
   detached spawn has no tty. The channel MCP alone cannot inject keystrokes
   into an interactive Claude — it is a report-back channel only.

External `agent-dashboard live` sessions are live because they run **interactive
claude in a real terminal** (tmux pane or pty broker proxying the user's tty).

## Verified CLI behaviour (Claude Code)

- `claude "<prompt>"` **without** `-p` starts an **interactive** session, submits
  the prompt as the first message, and **stays open**. (Authoritative:
  code.claude.com/docs/en/cli-reference.)
- `--mcp-config <json>` loads the same in interactive and print modes.
- An interactive TUI reads input from its controlling tty; injected text +
  `Enter` (via tmux `send-keys` or pty write) submits a message.

So seeding the first prompt is just: drop `-p`, pass the prompt **positionally**.

## Decisions

- **Interactive by default** when a live transport is available (tmux or pty). No
  per-spawn toggle. One-shot `-p` is not used anymore for dashboard spawns.
- **Both transports built now:** tmux (primary) and a headless pty fallback.
- **Headless pty is owned by a detached subprocess** (survives a dashboard
  restart, consistent with how a tmux session survives).
- **The spawner is respected:** the transport wraps the spawner's *resolved*
  `(env, binary, args)`. We only change *how* the command is launched, never
  what is launched.

## Architecture

### 1. Interactive args (`spawn.go buildSpawnArgs`)
Replace `"-p", req.prompt` with the prompt as a **positional** argument (appended
after flags), and do not pass `-p`. Everything else (`--model`,
`--system-prompt`, `--permission-mode`, `--mcp-config`, spawner args/binary)
unchanged. Result: `claude [flags] --mcp-config <cfg> "<prompt>"` → interactive,
seeded.

### 2. Headless transport selection (server-side)
A spawned agent is never inside the server's own tmux, so selection is:
`tmux on PATH → new detached tmux session; else → headless pty subprocess`.
(Do **not** reuse the server's `$TMUX` pane — always a fresh session.)

`SpawnManager.Spawn` resolves `(binary, args, env, cwd)` as today, then launches
via the chosen transport instead of the current bare-detached `exec.Command`.

#### 2a. tmux transport (inline in Spawn)
```
tmux new-session -d -P -F '#{pane_pid}' -s claude-spawn-<uuid> \
  env <K=V…> <binary> <args…>
```
- `-d` headless; `-P -F '#{pane_pid}'` prints the pane command's PID = claude's
  real PID (captured from stdout; the tmux client exits immediately).
- Env reaches the pane via an `env K=V …` wrapper (argv form, no shell; tmux runs
  argv directly). Uses the same merged env as `resolveSpawnEnv` (already drops
  `DASHBOARD_JWT_SECRET`/`DASHBOARD_HOOKS_SECRET`).
- The channel bridge (claude's MCP child) inherits `$TMUX_PANE` and records
  `tmuxPane` in `{pid}.json` → `liveInjectable=true`. Follow-up messages use the
  **existing** `SendMessageToChannel` tmux `send-keys` path — no dashboard change.
- A single-pane session ends when claude exits (tmux default
  `remain-on-exit off`) — auto-cleanup.

#### 2b. headless pty transport (detached subprocess)
New CLI subcommand `agent-dashboard pty-host -- <binary> <args…>`:
- Allocates a pty, runs `<binary> <args>` on the pty slave with the inherited
  env (the dashboard sets `cmd.Env`/`cmd.Dir` when launching the subcommand) so
  the spawner env/cwd flow through.
- Serves the existing loopback-token injection HTTP and writes
  `{childPid}.pty.json` with `ptyInject:true` (reuse `startPtyHTTPServer` +
  `writePtyDiscovery` from `ptyhost.go`).
- **Drains** pty output (`io.Copy(io.Discard, ptmx)`) — no foreground terminal to
  proxy (this is the headless difference from `RunPTY`).
- Prints the child PID as the first stdout line, then blocks serving until the
  child exits, then cleans up (remove discovery, close pty) and exits.

`SpawnManager.Spawn` launches this subcommand **detached** (`Setpgid`,
`cmd.Env`=resolved env, `cmd.Dir`=cwd), reads the first stdout line for claude's
PID via `StdoutPipe`, and leaves the subprocess running.

New reusable `channel.RunHeadlessPTY(ctx, name string, args, env []string, cwd string, onPid func(int)) error` holds the shared logic; the `pty-host` cobra command is a thin wrapper that prints the pid via `onPid`.

### 3. PID + lifecycle tracking (`spawn.go`)
- **PID:** tmux → parsed `pane_pid`; pty → first stdout line of the subprocess.
  Stored in `spawnStore` and returned by `POST /api/agents/spawn` (so Feature B
  opens the right agent).
- **Exit / cfg cleanup:** the launched `tmux` client (and `cmd.Wait` on it)
  exits immediately, so it cannot track the agent. Replace the tmux-path watcher
  with a **poll loop** (signal-0 on the resolved claude PID every few seconds);
  on exit, remove the temp `--mcp-config` file and mark `spawnStore` status. For
  the pty path, watch the **subprocess** (`cmd.Wait`) — when it exits (after its
  child), remove the cfg + mark status.

### 4. Frontend
No change beyond Feature B (already shipped on this branch): the spawn dialog
opens the new agent's modal once it appears; with `liveInjectable=true` the
`PromptInput` is in live-inject mode and the user converses.

## Components / files

- `server/internal/api/agents/spawn.go` — positional prompt; transport launch
  (tmux inline + pty-subprocess), PID capture, poll/subprocess watchers.
- `server/internal/channel/ptyhost.go` (or new `headlesspty.go`) —
  `RunHeadlessPTY` (drain variant of `RunPTY`).
- `server/cmd/serve/` — new `pty-host` cobra subcommand.
- Reuse: `startPtyHTTPServer`, `writePtyDiscovery`, `channelconfig.WriteTempConfig`,
  `SendMessageToChannel` (tmux send-keys + pty HTTP — already transport-aware).

## Error handling

- tmux `new-session` fails → fall through to the pty transport.
- pty subcommand fails to start or never prints a PID within a timeout → spawn
  returns an error; remove the temp cfg.
- No tmux **and** pty allocation fails → spawn error (no silent one-shot).
- Env wrapper: `env` not on PATH (rare) → tmux path falls back to pty.

## Testing

- **Unit (Go):** headless transport selection (tmux-present → tmux; absent → pty,
  ignoring `$TMUX`); `buildSpawnArgs` emits a positional prompt and no `-p`;
  `env`-wrapper argv construction; pane_pid parsing from `-P -F` output.
- **Unit (Go):** `RunHeadlessPTY` against a trivial child (e.g. `sh -c 'read x'`)
  — writes `{pid}.pty.json` with `ptyInject:true`, the injection HTTP delivers
  input, drains output, cleans up on child exit. Use a temp HOME.
- **Spawn manager:** inject the launcher (seam) so tests assert the right
  transport command is built without spawning tmux/claude — mirror the existing
  `execStart` capture seam. Assert PID capture + cfg cleanup on exit.
- **No live-agent / real-claude tests** (project rule): use fakes/seams.

## Out of scope

- Changing pipeline/orchestrator spawns (they remain as-is).
- A per-spawn one-shot toggle (interactive is the default; revisit if needed).
- Reworking `agent-dashboard live` (foreground) — only sharing helpers where clean.

## Risks

- tmux env propagation: mitigated by the explicit `env K=V` wrapper rather than
  relying on tmux env inheritance.
- pty readiness: the seeded prompt is a CLI arg (not injected), so there is no
  startup-timing race for the *first* message; follow-ups inject into a
  ready TUI.
- Subprocess PID plumbing (pty path): the dashboard depends on the first stdout
  line being the PID — keep that contract explicit and tested.
