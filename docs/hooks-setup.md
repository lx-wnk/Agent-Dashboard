# Hooks Setup

Claude Code lifecycle hooks let the dashboard receive push notifications on every
tool call rather than waiting for the next SSE poll interval (default 3 s).


> **Not the permission bridge.** Answering a session's permission prompts from
> the dashboard is a different feature with its own installer —
> `agent-dashboard hooks install`, described in
> [agent-control.md](guides/agent-control.md#answering-a-permission-prompt-from-the-dashboard).
> It manages its own entries in the same `hooks` object and preserves the ones
> below, but both write to the same file: edit by hand with that in mind.

## How it works

`scripts/hooks/notify.js` is a fire-and-forget Node.js script. Claude Code runs it
for each lifecycle hook you install. The script reads the event payload from stdin
and sends it to the dashboard. The dashboard acknowledges immediately and triggers a
debounced SSE rescan within `DASHBOARD_HOOKS_DEBOUNCE_MS` milliseconds (default 100 ms).

Everything here is **opt-in**: with no hook installed the dashboard behaves exactly
as before (process/JSONL scan only), and SSE payloads are byte-identical.

### Per-event granularity

When installed, the receiver also records a small, bounded history of recent hook
events per session (`PreToolUse`, `PostToolUse`, `Stop`, …) and surfaces them in the
agent modal under **Hook events**. This adds per-event detail on top of the periodic
scan. The history is in-memory only — nothing is written to disk or a database — and
the stored payload preview is truncated to 512 bytes, so raw `tool_input` /
`tool_response` are never persisted. The cap is `DASHBOARD_HOOK_EVENTS_PER_SESSION`
(default 50).

## Installation

### 1. Add the hooks to `~/.claude/settings.json`

Open `~/.claude/settings.json` (create it if it does not exist). The same
`notify.js` command works for every hook type, so register the events you want.
For full per-event granularity, install all of them:

```json
{
  "hooks": {
    "PreToolUse": [{ "matcher": "", "hooks": [{ "type": "command", "command": "node /absolute/path/to/agent-dashboard/scripts/hooks/notify.js" }] }],
    "PostToolUse": [{ "matcher": "", "hooks": [{ "type": "command", "command": "node /absolute/path/to/agent-dashboard/scripts/hooks/notify.js" }] }],
    "Notification": [{ "matcher": "", "hooks": [{ "type": "command", "command": "node /absolute/path/to/agent-dashboard/scripts/hooks/notify.js" }] }],
    "Stop": [{ "matcher": "", "hooks": [{ "type": "command", "command": "node /absolute/path/to/agent-dashboard/scripts/hooks/notify.js" }] }],
    "SubagentStop": [{ "matcher": "", "hooks": [{ "type": "command", "command": "node /absolute/path/to/agent-dashboard/scripts/hooks/notify.js" }] }],
    "UserPromptSubmit": [{ "matcher": "", "hooks": [{ "type": "command", "command": "node /absolute/path/to/agent-dashboard/scripts/hooks/notify.js" }] }],
    "SessionStart": [{ "matcher": "", "hooks": [{ "type": "command", "command": "node /absolute/path/to/agent-dashboard/scripts/hooks/notify.js" }] }]
  }
}
```

Replace `/absolute/path/to/agent-dashboard` with the actual path on your machine.
To keep it minimal, install only `PostToolUse` — you still get faster refreshes,
just without the other event types in the Hook events list.

Claude Code passes each event's payload (including `session_id` and, for tool
hooks, `tool_name`) on stdin; `notify.js` forwards it as-is with the hook type, so
no per-event configuration is needed.

### 2. Set environment variables

In `~/.zshrc` (or wherever you configure your shell environment):

```bash
# Required on both the hook script side and the server side — must match
export DASHBOARD_HOOKS_SECRET=your-random-secret

# Optional — defaults shown
export DASHBOARD_HOOKS_URL=http://127.0.0.1:13120/api/hooks/event
```

The dashboard server also needs `DASHBOARD_HOOKS_SECRET` set (add it to the
terminal session where you run `task dev`, or export it in `~/.zshrc`).

### 3. Restart the dashboard server

```bash
task dev
```

## Verification

After setup, open a Claude Code session and trigger any tool use. The dashboard
cards should update within ~100 ms rather than waiting 3 s.

To manually test the endpoint (no secret configured):

```bash
curl -s -o /dev/null -w "%{http_code}" -X POST \
  http://127.0.0.1:13120/api/hooks/event \
  -H "Content-Type: application/json" \
  -d '{"hookType":"PostToolUse","tool":"Read"}'
# Expected: 204
```

With a secret:

```bash
curl -s -o /dev/null -w "%{http_code}" -X POST \
  http://127.0.0.1:13120/api/hooks/event \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-random-secret" \
  -d '{"hookType":"PostToolUse","tool":"Read"}'
# Expected: 204

# Without secret header — should be rejected
curl -s -o /dev/null -w "%{http_code}" -X POST \
  http://127.0.0.1:13120/api/hooks/event \
  -H "Content-Type: application/json" \
  -d '{"hookType":"PostToolUse"}'
# Expected: 401
```

## Tuning

| Env var                       | Default | Effect                                                                    |
| ----------------------------- | ------- | ------------------------------------------------------------------------- |
| `DASHBOARD_HOOKS_DEBOUNCE_MS`       | `100`   | Batches rapid hook events; lower = fresher data, higher = less CPU load    |
| `DASHBOARD_HOOKS_SECRET`            | (none)  | Shared bearer token; highly recommended for any multi-user deployment      |
| `DASHBOARD_HOOK_EVENTS_PER_SESSION` | `50`    | Max recent hook events kept in memory per session for the Hook events view |

## Security note

`/api/hooks/event` is exempt from the dashboard's session-cookie authentication
so that `notify.js` can POST without a login cookie. It is protected only by the
`DASHBOARD_HOOKS_SECRET` bearer token. Because the server always binds to
`127.0.0.1` by default, external hosts cannot reach this endpoint without
explicit SSH tunnelling or a VPN — the same constraint that applies to the rest
of the dashboard.
