# MCP Endpoint

The dashboard exposes a stateless StreamableHTTP MCP server at `POST /api/mcp` for external agent control. Each request is self-contained — there is no server-side session map.

## Authentication

```
Authorization: Bearer mcp_<hex>
Accept: application/json, text/event-stream
```

Generate tokens in **Settings → API Keys**. Only the SHA-256 hash is stored; the raw token is shown once at creation and never again.

## Scopes

Scopes are hierarchical — a higher scope implies all lower ones.

| Scope | Access |
|---|---|
| `tasks:read` | List and read tasks, stage runs, audit log, permission requests |
| `tasks:write` | Create, update, delete tasks (implies `tasks:read`) |
| `pipeline:control` | Progress, approve, cancel, retry tasks; manage permissions (implies `tasks:read`) |
| `keys:manage` | Full access including API key management |

## Tools (21)

`list_tasks`, `get_task`, `list_stage_runs`, `list_audit`, `list_permission_requests`, `create_task`, `update_task`, `delete_task`, `manage_task`, `progress_task`, `cancel_task`, `retry_task`, `grant_permission`, `resolve_permission_request`, `add_dependency`, `remove_dependency`, `list_schedules`, `manage_schedule`, `list_api_keys`, `create_api_key`, `revoke_api_key`

Each tool checks its required scope at call time and returns an MCP error if the token's scope is insufficient.

## Connect the dashboard to Claude

The fastest way to wire a Claude Code session to the dashboard's task tools is the one-command
method available in the key dialog.

### One-command setup (recommended)

1. Open **Settings → API Keys** in the dashboard.
2. Click **+ Add Key**, choose the **Developer** or **Admin** role, and click **Create Key**.
3. In the token-reveal dialog, find the **CLI command** block — it shows a ready-to-run command
   like:
   ```sh
   claude mcp add --scope user --transport http agent-dashboard \
     http://127.0.0.1:13120/api/mcp \
     --header "Authorization: Bearer mcp_<your-token>"
   ```
4. Click the copy button, then run the command in your terminal. It writes the MCP server config to
   `~/.claude.json` at user scope — every Claude Code session you open will auto-connect to the
   dashboard.

The `--scope user` flag makes the connection global (all sessions). To scope it to one project
only, replace `--scope user` with `--scope project` — this writes to the project's `.mcp.json`
instead.

> **Verify the exact `claude mcp add` flags for your Claude Code version** by running
> `claude mcp add --help`. The flags above match the HTTP-transport syntax (`--transport http`,
> `--scope <local|user|project>`, `--header`) as of Claude Code 2025. If a flag name differs, adapt
> accordingly and update this doc.

### Make the session controllable (`agent-dashboard live`)

The MCP connection above lets a session **report to** the dashboard (task tools, replies,
permission requests). To also **control** a session from the dashboard — answer its
AskUserQuestion prompts, inject prompts, drive it from the Terminal tab — start it with:

```sh
agent-dashboard live -- <your usual claude args>
```

`live` runs your normal, interactive Claude session (it proxies your real terminal, so you use it
exactly as before) but wraps it so the dashboard owns an input path to it: it auto-loads the
channel MCP and picks a transport automatically — inside/with tmux it uses the tmux pane, otherwise
a built-in pty broker (no tmux required). Either way the session becomes **live-injectable**: its
AskUserQuestion prompts surface as answerable cards in the needs-you band and Terminal tab, and you
can push prompts to it. Add `--yolo` to skip permission prompts.

Sessions the dashboard **spawns** for you already run this way. A plain `claude` you started
yourself (not via `live`, not in tmux) is monitor-only — the dashboard can see it but has no input
path. This cannot be retrofitted onto an already-running session (a session's terminal is owned at
launch); relaunch it via `agent-dashboard live` to make it controllable.

### Manual / JSON config alternative

If you prefer to manage the config file directly, add an entry to your `.mcp.json` (project-scoped)
or `~/.claude.json` (user-scoped):

```json
{
  "mcpServers": {
    "agent-dashboard": {
      "type": "http",
      "url": "http://127.0.0.1:13120/api/mcp",
      "headers": {
        "Authorization": "Bearer mcp_<your-token>"
      }
    }
  }
}
```

`.mcp.json` is gitignored to prevent accidental token commits. The JSON block is also available in
the key dialog's **JSON config** block for one-click copy.
