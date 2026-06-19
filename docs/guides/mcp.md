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

## Local integration

Copy the shipped example and export a token — any Claude Code session opened in this repo then auto-connects to the dashboard MCP:

```bash
cp .mcp.json.example .mcp.json
export DASHBOARD_MCP_TOKEN=mcp_<your-token>
```

`.mcp.json` is gitignored to prevent accidental token commits.
