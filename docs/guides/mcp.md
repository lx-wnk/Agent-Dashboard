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
| `memory:read` | Search and read entries from the system memory store |
| `memory:write` | Write entries to the system memory store |
| `obsidian:read` | Read and search notes in the configured Obsidian vault |
| `obsidian:write` | Create, overwrite, or delete a note in the configured Obsidian vault (implies `obsidian:read`) |
| `github:read` | List open pull requests and search issues/pull requests in the configured GitHub repositories |
| `github:write` | Post a comment on a GitHub issue or pull request (implies `github:read`; does **not** imply `github:merge`) |
| `github:merge` | Merge a GitHub pull request (implies `github:read`; does **not** imply `github:write`) |
| `keys:manage` | Full access including API key management |

## Tools (51)

**`tasks:read`** — `list_tasks`, `get_task`, `list_stage_runs`, `list_audit`, `list_permission_requests`, `list_projects`, `list_spawners`, `list_schedules`

**`tasks:write`** — `create_task`, `update_task`, `delete_task`, `manage_task`, `add_dependency`, `remove_dependency`, `create_project`, `manage_schedule`

**`agent:coord`** — `write_scratchpad`, `read_scratchpad`, `list_scratchpad`, `acquire_lock`, `release_lock`, `wait_for_port`

**`pipeline:control`** — `advance_task`, `hold_task`, `resume_task`, `progress_task`, `cancel_task`, `retry_task`, `grant_permission`, `resolve_permission_request`, `approve_all_pending`, `get_refine_status`, `approve_spec`, `refine_task`, `inject_concept`, `approve_plan`, `reject_plan`, `get_plan_status`

**`memory:read`** — `memory_search`

**`memory:write`** — `memory_write`

**`obsidian:read`** — `obsidian_read`, `obsidian_search`

**`obsidian:write`** — `obsidian_write`, `obsidian_delete`

**`github:read`** — `github_read`, `github_search`

**`github:write`** — `github_comment`

**`github:merge`** — `github_merge`

**`keys:manage`** — `list_api_keys`, `create_api_key`, `revoke_api_key`

The four `obsidian_*` tools reach the vault configured under **Settings → Obsidian**
(`server/internal/apps/obsidian`); when that vault is not fully configured (`obsidian.baseURL`,
`obsidian.vaultRoot`, and `obsidian.apiKey` are a required trio), none of the four are registered at
all rather than being registered and always failing. `obsidian_write` and `obsidian_delete` are
irreversible: a write overwrites any existing note at that path, and a delete cannot be undone.

The four `github_*` tools reach the repositories configured under **Settings → GitHub**
(`server/internal/apps/github`); when `github.token`/`github.repos` are not both set, none of the
four are registered, for the same reason. `github:write` implies `github:read` but deliberately
**not** `github:merge`, and `github:merge` implies `github:read` but deliberately not `github:write`
— a key that may comment must not be able to merge by accident, and a key that may merge has no
business editing discussions. That scope check is the coarser net above the capability gate:
`github_merge` still needs its own `github.merge` grant, and that capability's class (`spend`)
denies it outright with no grant at all, regardless of scope — see
[Security](security.md#githubs-token-and-repository-boundary).

### Attaching a task to a project

`create_task` takes either a `projectId` or a `projectSlug` — never both. The slug is resolved to
its project and the call fails if no project carries it, so a typo cannot silently produce an
unattached task. When no project matches, create one with `create_project` (slug and name required)
and use the returned id or slug. A project created this way has no folders yet, and the UI's New-Task
form takes its working directory *only* from a project's folders — so that form cannot be submitted
for the new project until a folder is added under **Settings → Projects**. `create_project` says so
in its tool description and returns it as `nextStep` on the created project, so the agent can pass
the handover on; tasks created over MCP are unaffected because `create_task` carries its own `cwd`.

`create_project` accepts `description`, `color`, and `defaultSpawnerId`, but **not** `setupCommand`.
That command is executed as `sh -c` in every worktree the project creates, so `POST /api/projects`
restricts it to admins **when `auth.mode` is not `none`**; the MCP tool omits the field
unconditionally. It is the one tool whose schema declares `"additionalProperties": false`, and the
handler enforces it: any key outside `slug`, `name`, `description`, `color`, and `defaultSpawnerId`
fails the call by name instead of being dropped in silence. `name` is capped at 200 characters and
`description` at 10 000 — the same limits `POST /api/projects` applies, so the rule does not depend
on which door the caller used. Set it in the UI under **Settings → Projects** instead. For the same reason
`list_projects` and `create_project` report `hasSetupCommand` (a boolean) rather than the command
itself: the text is deliberately never put on the wire, because those strings routinely carry
registry tokens and `tasks:read` is enough to call `list_projects`.

A successful `create_project` publishes a `project_created` event on `/api/projects/stream`, the
same channel `POST /api/projects` uses, so the dashboard picks the new project up without a reload.

It is also attributed: an audit event (`project_created`, target `project:<id>`, metadata
`{slug, source: "mcp_create_project"}`) is written, and a `mcp: project created` line is logged with
the slug, the project id, and the id of the API key that made the call. Neither records the name or
description — those are agent-supplied free text.

Each tool checks its required scope at call time and returns an MCP error if the token's scope is insufficient.

## Pipeline agents already have a key

A stage run the dashboard's own task pipeline spawns needs none of the setup below: the spawner
mints a per-stage-run credential and writes it into the agent's `--mcp-config` as a second server,
`dashboard-tasks` (`http`, alongside the stdio `dashboard-channel` server every spawn already
gets) — no hand-made key, no `claude mcp add`. That spawn runs with `--strict-mcp-config`, so
the written file is the agent's whole MCP surface: your user-scope servers from
`~/.claude.json` are copied into it, but a user-scope `dashboard-tasks` registration is
deliberately not — the stage run uses its own narrower credential, never yours. That credential is scoped to the stage run that
holds it, expires with it, and is revoked the moment the run ends; see
[Security](security.md#capabilities-and-the-permission-gate) for what it can do and how long it
lives.

The rest of this page is for **your own** Claude Code client — a session you start yourself in a
terminal — which still needs a key from **Settings → API Keys**, because there is no stage run to
attribute it to.

## MCP applications

MCP servers you register for Claude Code at user scope become *applications* in the dashboard. A pipeline run gets an application only when its routine attaches it, the dashboard holds the application's secrets, and grants decide which of its tools an agent may call.

### Register a server

```bash
claude mcp add --scope user mail -- npx -y imap-mcp-server
```

The dashboard mirrors user-scope servers from `~/.claude.json` when it starts, so restart it after adding one. The server then appears in **Settings → Applications** and in the resource registry as `kind = application` with the slug `mcp-<name>`.

- Only user-scope servers are mirrored; `local` and `project` scope servers are not.
- `dashboard-channel` and `dashboard-tasks` are the dashboard's own servers and are never mirrored.
- The name must be a valid slug — lowercase letters, digits and hyphens. A server with any other name is skipped with the log line `mcpapps: server not mirrored`, and because runs only receive mirrored applications, **it reaches no run at all**. Register it again under a valid name:

```bash
claude mcp remove --scope user My_Mail
claude mcp add --scope user my-mail -- npx -y imap-mcp-server
```

### Which runs get a server

A run receives the dashboard's own servers, every application marked **Attach to every run**, and the applications of the routine that created its task — nothing else from `~/.claude.json`.

- The first time the dashboard mirrors servers, every server that already exists is marked **Attach to every run**, so existing setups keep working. Servers added later start unattached. Change it per application in Settings → Applications.
- To attach an application to a routine, tick it under **Applications** in the routine form (**Build → Schedules**), or send `applications` — a list of application resource ids — with `POST` or `PATCH /api/schedules`. An unknown id is rejected with `400`.
- The scheduler copies the routine's list onto every task it creates. The task API does not accept `applications`; a task gets applications only from its routine.

### Secrets

In Settings → Applications, add the environment variable names the server reads its credentials from with **Add variable** (names match `^[A-Z_][A-Z0-9_]*$`), then enter a value for each. A server that takes per-account passwords from the environment might, for example, need `IMAP_MCP_ACCOUNT_OVH_IMAP_PASSWORD` and `IMAP_MCP_ACCOUNT_OVH_SMTP_PASSWORD`.

- Values are encrypted with AES-256-GCM using the dashboard's secret key and never returned by any API; the panel only shows whether a value is set and when it last changed. Without a configured key, storing a value fails with `503`.
- At spawn, the values are written into the `env` of that application's entry in the run's temporary MCP config. No other server in the run sees them.
- If an attached application is missing a required value, or its server is no longer in `~/.claude.json`, the stage run **fails before the agent starts**, with a reason beginning `MCP applications:`. Add the value and retry the task.
- If `~/.claude.json` cannot be read, the run gets no applications but still starts.

The same over HTTP: `PATCH /api/applications/{resourceId}` with `{"requiredEnv": [...]}` or `{"attachAll": true}`; `PUT /api/applications/{resourceId}/secrets/{NAME}` with `{"value": "..."}`; `DELETE` on the same path.

### Tools and grants

**Refresh tool list** (`POST /api/applications/{resourceId}/refresh`) starts the server with its secrets and reads its tool list. Only stdio servers — entries with a `command` — are supported; `http` and `sse` servers are refused with an error. A failed refresh keeps the previous list and shows the error.

Each tool becomes a capability named the way Claude Code names it, `mcp__<server>__<tool>`, with class `tool`. A later refresh never changes the class of a capability that already exists. The server's `readOnlyHint` and `destructiveHint` are shown for orientation only; the MCP specification says clients must not trust them.

Refresh before granting — the grants API rejects capability names it does not know. Then:

```bash
agent-dashboard grants add mcp__mail__search_emails --scope routine:<schedule id> --mode allow
agent-dashboard grants add mcp__mail__send_email --scope global --mode deny
```

Grants resolve from the most specific context: task, routine, project (the task's working directory), global. An allow lands in the run's `--allowedTools`, a deny in `--disallowedTools`.

**No grant is not an allow.** Pipeline runs are headless, so an ungranted tool is refused; the agent can ask through a permission request, and a human's approval is honoured for that task. Allow-all autonomy (`spec_gated`, `full`) does not allow application tools.

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
