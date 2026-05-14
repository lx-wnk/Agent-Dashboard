# Hooks Setup

Claude Code lifecycle hooks let the dashboard receive push notifications on every
tool call rather than waiting for the next SSE poll interval (default 3 s).

## How it works

`scripts/hooks/notify.js` is a fire-and-forget Node.js script. Claude Code runs it
after each tool call. The script reads the event payload from stdin and sends it
to the dashboard. The dashboard acknowledges immediately and triggers a debounced
SSE rescan within `DASHBOARD_HOOKS_DEBOUNCE_MS` milliseconds (default 100 ms).

## Installation

### 1. Add the hook to `~/.claude/settings.json`

Open `~/.claude/settings.json` (create it if it does not exist) and add:

```json
{
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "node /absolute/path/to/agent-dashboard/scripts/hooks/notify.js"
          }
        ]
      }
    ]
  }
}
```

Replace `/absolute/path/to/agent-dashboard` with the actual path on your machine.

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
| `DASHBOARD_HOOKS_DEBOUNCE_MS` | `100`   | Batches rapid hook events; lower = fresher data, higher = less CPU load   |
| `DASHBOARD_HOOKS_SECRET`      | (none)  | Shared bearer token; highly recommended for any multi-user deployment     |

## Security note

`/api/hooks/event` is exempt from the dashboard's session-cookie authentication
so that `notify.js` can POST without a login cookie. It is protected only by the
`DASHBOARD_HOOKS_SECRET` bearer token. Because the server always binds to
`127.0.0.1` by default, external hosts cannot reach this endpoint without
explicit SSH tunnelling or a VPN — the same constraint that applies to the rest
of the dashboard.
