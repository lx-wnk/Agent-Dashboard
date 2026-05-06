# AGENTS.md — Project Bootstrap

> All agents MUST read and follow this file.

## Identity

**claude-agent-overview** | Vue 3 + Express 5 + TypeScript | No Docker (native Node)

## Context Architecture

| Layer | File                                      | Content                         |
| ----- | ----------------------------------------- | ------------------------------- |
| 0     | `.agent-context/layer0-agent-workflow.md`   | Agent Workflow (shared)         |
| 1     | `.agent-context/layer1-bootstrap.md`        | Project identity, tech stack    |
| 2     | `.agent-context/layer2-project-core.md`     | Dev principles + critical rules |
| 3     | `.agent-context/layer3-guidebook.md`        | Task routing, skills, memory    |

@.agent-context/agent-startup.md
@.agent-context/layer0-agent-workflow.md
@.agent-context/layer1-bootstrap.md
@.agent-context/layer2-project-core.md
@.agent-context/layer3-guidebook.md

## Quick Rules (Always Apply)

> Details in `.agent-context/layer2-project-core.md`

- Server binds to `127.0.0.1` only (reads sensitive session data)
- macOS and Linux — `ps`/`lsof` on macOS, `/proc` on Linux for process scanning
- No database — all data from filesystem + running processes
- Single dev command: `pnpm dev` (Express + Vite on port 13120)

## Compaction Preservation

When compacting context, always preserve:

- List of modified/created files in this session
- Active test/lint commands and their last results
- Unfinished tasks and next steps

## Pipeline Permissions

Default tool permissions for pipeline stage agents running in this project.

```json
{
  "allow": ["Read", "Write", "Edit", "MultiEdit", "Glob", "Grep", "LS", "Bash", "WebFetch"]
}
```

## MCP Self-Service Permissions

If you (a spawned agent) hit a tool that is denied, do NOT write prose asking
the user. Use the dashboard-channel `request_permission` MCP tool — and
prefer the BULK form.

**1. As your first action in any non-trivial task**, scan ahead, build the
full list of tools you anticipate needing, then call once:

```jsonc
// request_permission MCP call
{
  "permissions": [
    { "tool": "Bash", "pattern": "pnpm test*", "reason": "run vitest" },
    { "tool": "Bash", "pattern": "pnpm lint*", "reason": "lint" },
    { "tool": "Bash", "pattern": "pnpm typecheck*", "reason": "typecheck" },
    { "tool": "WebFetch", "reason": "fetch library docs" }
  ]
}
```

The dashboard auto-resolves any entries already pre-granted (silent, no UI
prompt). Only uncovered entries surface as ON HOLD; the user grants them
as one batch decision.

**2. Spawning sub-tasks via `create_task`?** Pass the child's permissions
inline so it never needs its own request_permission round-trip:

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

**3. `manage_task` for retroactive edits.** Grant or revoke permissions on
a running task at any time:

```jsonc
// manage_task action="grant_permissions"
{ "task_id": "...", "action": "grant_permissions", "template": "feature_implementation" }
```

**Templates available:** `feature_implementation`, `research_only`,
`test_only`, `review_only`.

**Git push policy:** by default `git push` is hard-blocked even when
granted. Set env `DASHBOARD_ALLOW_GIT_PUSH=true` (global) or task
`metadata.allowGitPush=true` (per-task) to opt out.
