# MCP Connect Command in API-Key Reveal Dialog

**Date:** 2026-06-12
**Status:** Approved (design), pending implementation plan

## Problem

A user wants any open Claude Code session (in any folder, not spawned by the
dashboard) to refine and create pipeline tasks. The dashboard already exposes a
full task-management MCP server at `POST /api/mcp` (tools: `create_task`,
`update_task`, `manage_task`, `add_dependency`, `list_tasks`, `get_task`, …),
authenticated by a bearer API key. Connecting a session to it requires
registering the server in Claude's MCP config — a multi-part command that is
tedious to assemble by hand and easy to get wrong (URL, transport, header).

The raw API token is shown exactly once, in the token-reveal dialog (after
create or regenerate); the database only stores its SHA-256 hash. That dialog is
therefore the only place a ready-to-use, token-embedded connect command can be
rendered.

## Goal

In the token-reveal dialog, offer two copy-ready artifacts that register the
dashboard task MCP server for the just-created/regenerated key:

1. A `claude mcp add` CLI one-liner (for direct shell execution).
2. An equivalent `mcpServers` JSON block (for manual paste into a Claude config
   file).

Both embed the real token and a dynamic dashboard URL.

## Non-Goals

- No connect command in the key list rows (existing keys; token no longer
  available). Reveal dialog only.
- No backend changes, no new endpoints.
- No per-project / per-spawner key scoping (separate concern; see Security
  Notes).

## Design

### Scope of the command

The CLI command uses `--scope user`, registering the server in the user's global
Claude config (`~/.claude.json`) so every session in every folder gets the
tools — which is the stated goal.

### Artifacts

For dashboard origin `O` (`window.location.origin`) and token `T`:

**CLI:**
```
claude mcp add --scope user --transport http dashboard-tasks O/api/mcp --header "Authorization: Bearer T"
```

**JSON:**
```json
{
  "mcpServers": {
    "dashboard-tasks": {
      "type": "http",
      "url": "O/api/mcp",
      "headers": { "Authorization": "Bearer T" }
    }
  }
}
```

Server name is the fixed string `dashboard-tasks` (matches the MCP server's own
`serverInfo.name`).

### Dynamic URL

`O` is taken from `window.location.origin` at render time, so the command points
at whatever host/port/scheme the dashboard is actually served from (loopback,
LAN host, tunnel) — never a hardcoded `127.0.0.1:13120`.

### Components

**New: `src/utils/mcpCommand.ts`** (SSOT for command assembly, unit-tested)

- `buildMcpAddCommand(origin: string, token: string): string` — returns the CLI
  one-liner. Trailing slash on `origin` is stripped before appending `/api/mcp`.
- `buildMcpJsonConfig(origin: string, token: string): string` — returns the
  pretty-printed JSON block (2-space indent) as a string.

**Modified: `src/components/ApiKeySettings.vue`**

- The reveal dialog gains two new blocks below the existing raw-token block,
  each visually separate with its own copy button, reusing the existing
  `copyHint` feedback pattern (`Copied!` / `Copy failed`, 2s reset). To keep
  three independent copy targets, the single `copyHint` string is replaced by a
  small per-target hint state (e.g. a `copiedTarget` ref holding
  `'token' | 'cli' | 'json' | '__error__' | null`).
- A scope hint: the reveal dialog must know the just-revealed key's scopes. Both
  `handleCreate` and `regenerateKey` already receive `data.key.scopes`; store
  the relevant scopes alongside `revealedToken` (e.g. a `revealedScopes` ref).
  When the key lacks `tasks:write`, show a short note: "read-only key — creating
  or refining tasks needs the Developer or Admin role."
- `dismissReveal()` clears the new refs too.

### Data flow

```
create / regenerate
  → POST /api/settings/api-keys (or /:id/regenerate)
  → { key, token }
  → revealedToken = token ; revealedScopes = key.scopes
  → dialog renders: raw token | buildMcpAddCommand(origin, token) | buildMcpJsonConfig(origin, token)
  → per-block copy buttons → navigator.clipboard.writeText(...)
```

### Error handling

- Clipboard write failure → reuse existing `__error__` hint path ("Copy
  failed").
- No network calls added; nothing else to fail.

### Testing

Unit tests (Vitest) for `src/utils/mcpCommand.ts`:

- `buildMcpAddCommand` embeds origin and token correctly.
- Trailing slash on origin is normalized (no `//api/mcp`).
- `buildMcpJsonConfig` produces valid JSON; `JSON.parse` round-trips and
  contains the expected `url` and `Authorization` header.
- Token charset is `mcp_[a-z0-9]+` (no shell metacharacters), so the CLI string
  needs no extra quoting beyond the static `"…"` around the header value — a
  test documents this assumption by asserting the token contains no
  whitespace/quotes.

Component-level behavior (copy buttons, scope hint) is covered by the existing
manual/visual QA flow; no new E2E required.

## Security Notes

- The connect command embeds a live bearer token. It is only rendered in the
  one-time reveal dialog, consistent with the existing raw-token reveal — no new
  exposure surface.
- `/api/mcp` already sits outside the same-origin CSRF group and authenticates
  purely by bearer key, so the generated command needs no `Origin` header.
- Out of scope but noted: any `tasks:write` key can author tasks in any project
  (no per-project/per-spawner binding in the current auth model). This design
  does not change that; if tighter scoping is wanted later, it is a separate
  backend change to `api_keys` + `McpAuthMiddleware`.

## Affected Files

- `src/utils/mcpCommand.ts` (new)
- `src/utils/mcpCommand.test.ts` (new)
- `src/components/ApiKeySettings.vue` (modified)
