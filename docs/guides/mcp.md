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
| `tasks:read` | List and read tasks, stage runs, audit log, permission requests, projects, spawners, schedules |
| `tasks:write` | Create, update, delete tasks; create projects (implies `tasks:read`) |
| `agent:coord` | Scratchpads, lease locks, and port waits shared between agents |
| `pipeline:control` | Progress, approve, cancel, retry tasks; manage permissions; refine and plan gates (implies `tasks:read` and `agent:coord`) |
| `keys:manage` | Full access including API key management |

## Tools (41)

**`tasks:read`** — `list_tasks`, `get_task`, `list_stage_runs`, `list_audit`, `list_permission_requests`, `list_projects`, `list_spawners`, `list_schedules`

**`tasks:write`** — `create_task`, `update_task`, `delete_task`, `manage_task`, `add_dependency`, `remove_dependency`, `create_project`, `manage_schedule`

**`agent:coord`** — `write_scratchpad`, `read_scratchpad`, `list_scratchpad`, `acquire_lock`, `release_lock`, `wait_for_port`

**`pipeline:control`** — `advance_task`, `hold_task`, `resume_task`, `progress_task`, `cancel_task`, `retry_task`, `grant_permission`, `resolve_permission_request`, `approve_all_pending`, `get_refine_status`, `approve_spec`, `refine_task`, `inject_concept`, `approve_plan`, `reject_plan`, `get_plan_status`

**`keys:manage`** — `list_api_keys`, `create_api_key`, `revoke_api_key`

### Attaching a task to a project

`create_task` takes either a `projectId` or a `projectSlug` — never both. The slug is resolved to
its project and the call fails if no project carries it, so a typo cannot silently produce an
unattached task. When no project matches, create one with `create_project` (slug and name required)
and use the returned id or slug. A project created this way has no folders yet, so the new-task form
in the UI cannot pre-fill a working directory for it until one is added under **Settings → Projects**.

`create_project` accepts `description`, `color`, and `defaultSpawnerId`, but **not** `setupCommand`.
That command is executed as `sh -c` in every worktree the project creates, so `POST /api/projects`
restricts it to admins; a `tasks:write` token is not an admin credential. Set it in the UI under
**Settings → Projects** instead.

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
