# Permissions Model

Stage agents run with a `permissions.allow` list derived from `task_permissions` rows. The dashboard funnels every grant path through `bulkGrantPermissions()` in `server/services/approvalUtils.ts`, which validates each entry against `ALLOWED_TOOLS` and the `DANGEROUS_BASH_RE` block-list (curl/wget/eval/shell-substitution/etc.).

## Default Pipeline Tool Allow-List

Default tool permissions for pipeline stage agents running in this project:

```json
{
  "allow": ["Read", "Write", "Edit", "MultiEdit", "Glob", "Grep", "LS", "Bash", "WebFetch"]
}
```

## Grant Sources, by Precedence at Create Time

1. **`template`** — predefined sets in `server/services/permissionTemplates.ts` (`feature_implementation`, `research_only`, `test_only`, `review_only`).
2. **`permissions[]`** — explicit `[{tool, pattern?, expiresAt?}]` array. Time-bound grants expire at `expiresAt` (ISO timestamp).
3. **`inheritPermissions: true`** — when set with `parentTaskId`, copy effective (granted, non-expired) parent permissions onto the child. Only kicks in when no explicit `permissions[]` provided.

## Retroactive Management via MCP `manage_task` Tool

- `action: "grant_permissions"` — bulk-add by template/list (same shape as `create_task`).
- `action: "revoke_permission"` — remove by `permission_id`.
- `action: "list_permissions"` — inspect current grants (`effective_only: true` filters expired/denied).
- `action: "inherit_from_parent"` — pull parent's grants onto this task.
- `action: "set_metadata"` / `set_priority` / `set_budget` — non-permission edits.

REST mirror: `POST /api/tasks/:id/permissions/bulk` accepts the same `{template?, permissions?[]}` body.

## Channel-Bridge Bulk Request

Spawned agents must call `request_permission` with `permissions: [...]` as their first action. The server's `POST /api/permission-requests/bulk` auto-resolves any entries already covered by granted task_permissions (silent, no UI prompt) and only surfaces uncovered entries as ON HOLD. Single-tool legacy form still works.

## MCP Self-Service Permissions (Spawned Agents)

If you (a spawned agent) hit a tool that is denied, do NOT write prose asking the user. Use the dashboard-channel `request_permission` MCP tool — and prefer the BULK form.

**1. As your first action in any non-trivial task**, scan ahead, build the full list of tools you anticipate needing, then call once:

```jsonc
// request_permission MCP call
{
  "permissions": [
    { "tool": "Bash", "pattern": "pnpm test*", "reason": "run vitest" },
    { "tool": "Bash", "pattern": "pnpm lint*", "reason": "lint" },
    { "tool": "Bash", "pattern": "pnpm typecheck*", "reason": "typecheck" },
    { "tool": "WebFetch", "pattern": "docs.example.com", "reason": "fetch library docs" }
  ]
}
```

> **Note (post-tightening):** WebFetch now requires a non-empty domain pattern. Bare WebFetch grants (no `pattern`) are rejected at grant time and purged from the DB on startup.

The dashboard auto-resolves any entries already pre-granted (silent, no UI prompt). Only uncovered entries surface as ON HOLD; the user grants them as one batch decision.

**2. Spawning sub-tasks via `create_task`?** Pass the child's permissions inline so it never needs its own request_permission round-trip:

```jsonc
// create_task MCP call
{
  "slug": "child-task-slug",
  "title": "...",
  "cwd": "/abs/path",
  "parentTaskId": "<your task id from DASHBOARD_TASK_ID env>",
  "template": "feature_implementation",
  "permissions": [{ "tool": "Bash", "pattern": "git push *" }],
  // OR — if your task already has all needed perms:
  "inheritPermissions": true
}
```

**3. `manage_task` for retroactive edits.** Grant or revoke permissions on a running task at any time:

```jsonc
// manage_task action="grant_permissions"
{ "task_id": "...", "action": "grant_permissions", "template": "feature_implementation" }
```

**Templates available:** `feature_implementation`, `research_only`, `test_only`, `review_only`.

**Git push policy:** by default `git push` is hard-blocked even when granted. Set env `DASHBOARD_ALLOW_GIT_PUSH=true` (global) or task `metadata.allowGitPush=true` (per-task) to opt out.
