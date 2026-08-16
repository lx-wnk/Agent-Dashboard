# Multi-User Server Deployment Design

**Date:** 2026-04-26  
**Status:** Approved  
**Scope:** GitHub OAuth authentication, user-scoped task visibility, local session integration, and targeted bug fixes in process scanning and session matching.

---

## Context & Goals

The dashboard currently runs as a single-user local tool. This design makes it deployable on a shared team server with user isolation, while preserving full backwards compatibility with standalone local usage.

**Primary goals:**
- GitHub OAuth login restricted to a configurable GitHub org
- Each user sees only their own pipeline tasks and their own local sessions
- Users can register their local dashboard instance to have local Claude Code sessions aggregated into the central view
- Admins have cross-user visibility for pipeline tasks only — never for other users' registered remotes
- Zero breaking changes for standalone (no-auth) local usage

---

## Standalone Compatibility

If `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` are not set in the environment, the auth layer is completely bypassed. The dashboard behaves identically to the current version: no login required, no user scoping, no `users` table queries. All new code paths are gated behind an `isAuthEnabled()` check derived from these env vars.

---

## Section 1: Auth Layer (GitHub OAuth)

### Flow

```
Browser → GET /login
  → Redirect: github.com/login/oauth/authorize?scope=read:org
  → GitHub callback: GET /auth/callback?code=xxx
  → Server exchanges code for access_token (GitHub OAuth API)
  → Server checks org membership: GET /orgs/{GITHUB_ORG}/members/{username}
  → Not a member → 403 page
  → Member → upsert user in DB → sign JWT → set HttpOnly cookie
  → Redirect to /
```

### Session Management

Signed JWT stored as an `HttpOnly` cookie (not localStorage — XSS-safe). Payload: `{ sub: githubUserId, login: "alex", isAdmin: false, exp: 8h }`. Signed with `JWT_SECRET` env var. On expiry: redirect to `/auth/refresh` which re-validates the GitHub token silently; on GitHub token expiry, re-login.

Rationale for JWT over server-side sessions: SQLite as a session store blocks under SSE load. JWT is stateless and requires no additional infrastructure.

### Org Membership Check

`GET /orgs/{GITHUB_ORG}/members/{username}` using the user's OAuth access token. Note: the `read:org` scope is required — org membership may be private and not visible with the default `user` scope alone.

Two supported modes, selected via env var:
- **`GITHUB_ORG_MEMBERSHIP_PUBLIC=true`** (default): checks membership using the user's OAuth token. Requires org admins to set member visibility to "Public" in org settings.
- **`GITHUB_ORG_MEMBERSHIP_PUBLIC=false`**: the server uses a dedicated `GITHUB_SERVER_TOKEN` (a GitHub PAT or App installation token with `read:org` scope) to check membership server-side. Required for orgs with private member visibility.

```
GITHUB_SERVER_TOKEN=ghp_...   # only needed when GITHUB_ORG_MEMBERSHIP_PUBLIC=false
```

### Middleware

A single `requireAuth` Express middleware is added to all `/api/*` routes. Public routes exempt: `/auth/login`, `/auth/callback`, `/auth/logout`. The existing MCP Bearer-token mechanism (`/api/mcp`) is unchanged — it handles its own auth independently.

### New Environment Variables

```
GITHUB_CLIENT_ID=...
GITHUB_CLIENT_SECRET=...
GITHUB_ORG=your-org-name
GITHUB_ORG_MEMBERSHIP_PUBLIC=false   # set true to skip server-side org token
JWT_SECRET=...                        # min 32 random bytes
```

---

## Section 2: Database Schema Extensions

### New Table: `users`

```sql
CREATE TABLE IF NOT EXISTS users (
  id            TEXT PRIMARY KEY,   -- GitHub numeric user ID (stable across login renames)
  github_login  TEXT NOT NULL,      -- display only; can change
  display_name  TEXT,
  avatar_url    TEXT,
  is_admin      INTEGER NOT NULL DEFAULT 0,
  created_at    TEXT NOT NULL,
  last_login_at TEXT
);
```

The GitHub numeric user ID (not the login string) is used as the primary key — it is stable even if the user renames their GitHub account.

### New Table: `remote_registrations`

```sql
CREATE TABLE IF NOT EXISTS remote_registrations (
  id          TEXT PRIMARY KEY,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  url         TEXT NOT NULL,
  name        TEXT,              -- e.g. "MacBook Alex"
  bearer_key  TEXT,              -- token sent to the local dashboard for auth
  created_at  TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_remote_reg_user_url
  ON remote_registrations(user_id, url);
```

**Privacy rule (enforced in all repo layers, not just routes):** every query on `remote_registrations` must include `WHERE user_id = ?` bound to the authenticated user's ID. There is no admin override for this table — not in routes, not in MCP tools, not in the orchestrator.

### Additive Column Migrations

```sql
ALTER TABLE tasks    ADD COLUMN user_id TEXT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE api_keys ADD COLUMN user_id TEXT REFERENCES users(id) ON DELETE SET NULL;
```

Both columns default to `NULL`. Existing rows remain intact. Existing tasks with `user_id = NULL` are treated as system/legacy tasks and are only visible to admins.

### Task Visibility Rules

| Viewer | Sees |
|---|---|
| Authenticated user | Tasks WHERE `user_id = their ID` |
| Admin (`is_admin = 1`) | All tasks (incl. `user_id = NULL` system tasks) |
| Orchestrator-spawned tasks (no user session) | `user_id = NULL` → admin-only |

### Migration & Transition

A config flag `SHOW_LEGACY_TASKS=true` can make `user_id = NULL` tasks visible to all authenticated users during a transition period. Default: `false`. Legacy tasks should be manually assigned to an admin user after rollout.

---

## Section 3: Local Session Integration

### Concept

The existing global `DASHBOARD_REMOTES` env-var mechanism becomes user-scoped. Instead of a single global list, each user registers their local dashboard URL in the Settings UI. The server loads per-user remotes from `remote_registrations` at SSE-broadcast time.

### Local Dashboard Security

Local dashboard instances gain a new env var:

```
DASHBOARD_API_TOKEN=<random-hex>
```

When set, a new middleware protects `GET /api/agents` and `GET /api/agents/stream` with `Authorization: Bearer {token}`. Without this env var, the endpoint remains unprotected (backwards-compatible local-only behaviour).

### Registration Flow (Settings UI)

User provides:
- **URL**: e.g. `http://192.168.1.5:13120`
- **Bearer Token**: the `DASHBOARD_API_TOKEN` of their local instance
- **Name**: e.g. "MacBook Alex"

On save, the server immediately tests the connection (`GET {url}/api/agents` with the token) and shows a success/error indicator. The bearer key is stored in `remote_registrations.bearer_key`.

### Server-Side Aggregation

`remoteAggregator.ts` signature change:

```ts
// Before
aggregateAgents(local: Agent[], urls: string[]): Promise<Agent[]>

// After
aggregateAgents(local: Agent[], remotes: RemoteRegistration[]): Promise<Agent[]>
// RemoteRegistration = { url: string; bearerKey?: string; name: string }
```

The SSE handler loads the authenticated user's remotes from DB and passes them to `aggregateAgents`. The global `DASHBOARD_REMOTES` env var continues to work as a fallback for standalone deployments where no DB user exists.

### Visibility Model

| Agent source | Visible to |
|---|---|
| Server-local pipeline agents | All authenticated users |
| User's own registered remotes | Only that user |
| Other users' remotes | Nobody (no admin override) |

### Network Requirement

This uses a pull model: the central server calls `GET /api/agents` on the user's local machine. The local machine must be reachable from the server (same network, VPN, or port forwarding). NAT/CGNAT environments would require a push model (future consideration, out of scope here).

---

## Section 4: Frontend Changes

### `useUser` Composable

```ts
// src/composables/useUser.ts
// Returns: { user: User | null, isAdmin: boolean, isAuthenticated: boolean }
// In standalone mode: isAuthenticated = true, user = null, isAdmin = true
```

All other composables (`useAgents`, `useTasks`) remain unchanged — they consume user context via `useUser` only where needed for scoping.

### Login Page

Shown only when auth is enabled and no valid JWT is present. Minimal: app logo + "Login with GitHub" button + org name for context. No other UI elements.

### Settings Panel Extension

New "Meine Remotes" tab (hidden in standalone mode):
- List of registered remotes with connection status (green/red indicator)
- Form: URL + Bearer Token + Name fields
- Delete button per entry
- "Test connection" action

### Task List

Automatically scoped to `user_id` of the logged-in user via updated API responses. Admin toggle "Alle Tasks anzeigen" shows system/other tasks — applies only to pipeline tasks, not to Remotes (which are always private).

---

## Section 5: Bug Fixes Included in This Scope

These correctness issues are in files that will be touched during the multi-user work and are fixed in the same branch.

### 1. `decodeProjectDir` is lossy (display only)

**File:** `server/sessionScanner.ts`  
**Problem:** The display-decode replaces all `-` with `/`, breaking project names that contain hyphens (e.g. `my-project` becomes `my/project`). This is a cosmetic bug in the Sessions API response — the matching logic in `jsonlParser.ts` already works correctly via `encodePath()` and is unaffected.  
**Fix:** Keep the encoded directory name as the canonical `projectPath` and only decode it for `projectName` display. For paths where the encoded form starts with `-Users-`, reconstruct the path by restoring the leading `/` and replacing only the first segment separator — not all hyphens.

### 2. Multiple agents in the same CWD

**File:** `server/agentMerger.ts`  
**Problem:** When two agents share the same working directory (e.g. a parent + subagent), the JSONL matching can misassign session data.  
**Fix:** Read the open JSONL file directly via `lsof -p {pid}` to find the exact file descriptor the process has open, rather than searching by CWD + uptime heuristic.

### 3. Cost-trend ring buffer lost on restart

**File:** `server/index.ts` (in-memory ring buffer)  
**Problem:** 1 hour of cost-trend data is lost on every server restart — more impactful on a shared server than on a local machine.  
**Fix:** Persist the ring buffer as a compact JSON blob in `pipeline_config` (key: `cost_trend_history`), written every 60 seconds and read on startup. TTL: 24 hours.

### 4. N×`lsof` calls per SSE tick

**File:** `server/processScanner.ts`  
**Problem:** One `lsof` subprocess call per PID, every 3 seconds. With 20 agents: 20 processes × 20 ticks/minute = 400 shell forks/minute.  
**Fix:** Batch into a single call: `lsof -a -d cwd -p pid1,pid2,...,pidN -Fn` — one subprocess returning all CWDs.

---

## Out of Scope

- Push-model for local sessions (NAT/CGNAT support)
- Role-based access beyond `user` / `admin` (e.g. per-project roles)
- Audit log scoping by user (existing audit log remains global, admin-readable)
- SSO providers other than GitHub

---

## Implementation Notes

- All new DB tables are created in `server/db/schema.sql` and applied via the existing runtime migration in `server/db/client.ts`
- Auth middleware lives in `server/auth/` (new directory): `githubOAuth.ts`, `jwtMiddleware.ts`, `requireAuth.ts`
- `isAuthEnabled()` utility reads `process.env.GITHUB_CLIENT_ID` — used to gate all auth-specific code paths at startup
- No changes to the MCP layer, pipeline orchestrator, or notification system
