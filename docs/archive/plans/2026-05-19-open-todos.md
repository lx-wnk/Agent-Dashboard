# Open TODOs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Resolve the three open actionable TODOs: API key masking (UX-46), plugin guide update, and tygo codegen integration (ARCH-05).

**Architecture:** Three independent changes — a small Vue/TS frontend fix, a docs-only update, and a codegen pipeline addition. Can be done in any order; each task produces its own commit.

**Tech Stack:** Vue 3 + TypeScript (Vitest for tests), tygo (Go→TS codegen), Taskfile.yml

---

## Task 1: UX-46 — API Key Token Masking

**Files:**
- Modify: `src/utils/format.ts` — add `maskToken()`
- Modify: `src/utils/format.test.ts` — unit tests for `maskToken()`
- Modify: `src/components/ApiKeySettings.vue` — wire masking into reveal dialog

### Step 1: Write the failing test

Add to `src/utils/format.test.ts` (after existing imports):

```typescript
import { maskToken, formatCost, formatTokens, formatUptime, shortModel, totalTokenCount } from './format'
```

Add a new `describe` block at the end of the file:

```typescript
describe('maskToken', () => {
  it('masks middle of token keeping first 8 and last 4 chars', () => {
    const token = 'mcp_abcdefghij1234'
    // length=18: first 8 = 'mcp_abcd', last 4 = '1234', middle = 6 bullets
    expect(maskToken(token)).toBe('mcp_abcd••••••1234')
  })

  it('uses at least 8 bullets for short tokens', () => {
    const token = 'mcp_1234'
    // length=8: first 8 already covers all, pad 8 bullets minimum
    expect(maskToken(token)).toBe('mcp_1234••••••••1234')
  })

  it('handles a realistic 40-char MCP token', () => {
    const token = 'mcp_' + 'a'.repeat(36)
    // length=40: first 8 = 'mcp_aaaa', last 4 = 'aaaa', middle = 28 bullets
    expect(maskToken(token)).toBe('mcp_aaaa' + '•'.repeat(28) + 'aaaa')
  })

  it('never reveals more than first 8 + last 4 chars', () => {
    const token = 'mcp_' + 'x'.repeat(100)
    const masked = maskToken(token)
    expect(masked.startsWith('mcp_')).toBe(true)
    expect(masked.endsWith('xxxx')).toBe(true)
    expect(masked).toContain('•')
    // Total visible chars (excluding bullets): 8 + 4 = 12
    const visible = masked.replace(/•/g, '')
    expect(visible).toBe(token.slice(0, 8) + token.slice(-4))
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

```bash
pnpm test -- --reporter=verbose src/utils/format.test.ts
```

Expected: `maskToken is not a function` (or similar import error)

- [ ] **Step 3: Implement `maskToken` in `src/utils/format.ts`**

Add after the existing exports:

```typescript
/** Returns a masked version of a secret token: first 8 chars + bullets + last 4 chars. */
export function maskToken(token: string): string {
  const head = token.slice(0, 8)
  const tail = token.slice(-4)
  const bulletCount = Math.max(8, token.length - 12)
  return head + '•'.repeat(bulletCount) + tail
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
pnpm test -- --reporter=verbose src/utils/format.test.ts
```

Expected: all `maskToken` tests PASS

- [ ] **Step 5: Wire masking into `ApiKeySettings.vue`**

Add `maskToken` to the import at the top of `<script setup lang="ts">`:

```typescript
import { maskToken } from '@/utils/format'
```

After the existing `revealedToken` ref (line ~37), add:

```typescript
const tokenVisible = ref(false)
```

In the `dismissReveal` function, reset visibility:

```typescript
function dismissReveal() {
  revealedToken.value = null
  copyHint.value = null
  tokenVisible.value = false
}
```

Replace the token display block in the reveal dialog (`<div class="font-mono text-xs bg-green-50...">`) with:

```vue
<div class="relative font-mono text-xs bg-green-50 dark:bg-green-950/30 text-green-600 dark:text-green-400 p-3 pr-10 rounded border border-green-200 dark:border-green-800/50 break-all mb-3">
  {{ tokenVisible ? revealedToken : maskToken(revealedToken!) }}
  <button
    type="button"
    class="absolute right-2 top-1/2 -translate-y-1/2 p-1 rounded hover:bg-green-100 dark:hover:bg-green-900/40 text-green-500 dark:text-green-500 transition-colors"
    :aria-label="tokenVisible ? 'Hide token' : 'Show token'"
    @click="tokenVisible = !tokenVisible"
  >
    <svg v-if="tokenVisible" xmlns="http://www.w3.org/2000/svg" class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/>
      <line x1="1" y1="1" x2="23" y2="23"/>
    </svg>
    <svg v-else xmlns="http://www.w3.org/2000/svg" class="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
      <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/>
      <circle cx="12" cy="12" r="3"/>
    </svg>
  </button>
</div>
```

- [ ] **Step 6: Typecheck**

```bash
pnpm typecheck
```

Expected: no errors

- [ ] **Step 7: Commit**

```bash
git add src/utils/format.ts src/utils/format.test.ts src/components/ApiKeySettings.vue
git commit -m "fix(ux): mask API key token in reveal dialog with show/hide toggle (UX-46)"
```

---

## Task 2: Plugin Guide Update

**Files:**
- Modify: `docs/plugin-guide.md`

No unit tests — docs only. Verify correctness by reading `plugins/github-oauth/main.go`.

- [ ] **Step 1: Rewrite `docs/plugin-guide.md`**

Replace the entire file content with:

```markdown
# Plugin Developer Guide

Agent-dashboard supports a lightweight sidecar plugin system. Each plugin is an independent HTTP server that the dashboard discovers at startup, health-checks, and optionally spawns as a child process.

---

## What is a plugin?

A plugin is a standalone HTTP server that:

1. Listens on a loopback address (`127.0.0.1:<port>`).
2. Ships a `plugin.json` descriptor in its own directory.
3. Serves `GET /health` returning `{"ok":true}`.
4. Implements one or more **capability** contract(s).

The dashboard registry (`server/internal/plugin/`) reads `plugin.json`, starts the process (if `command` is set), waits up to 5 seconds for `/health` to return 200, then makes the plugin's capabilities available to the rest of the server.

---

## The `plugin.json` descriptor

```json
{
  "id":           "my-plugin",
  "version":      "1.0.0",
  "capabilities": ["auth_provider"],
  "addr":         "127.0.0.1:19001",
  "command":      ["./my-plugin"],
  "env":          ["MY_CLIENT_ID", "MY_CLIENT_SECRET"]
}
```

| Field          | Required | Description |
|----------------|----------|-------------|
| `id`           | yes      | Unique plugin identifier (slug, no spaces). |
| `version`      | yes      | Semver string. |
| `capabilities` | yes      | Array of capability strings the plugin implements (see below). |
| `addr`         | yes      | `127.0.0.1:<port>` the plugin HTTP server binds to. |
| `command`      | no       | Executable + args to launch the plugin. Omit if the process is already running. |
| `env`          | no       | Env var names to forward from the dashboard's environment. Only a fixed base set (PATH, HOME, TMPDIR, TEMP, USER, LANG, LC_ALL) plus any names listed here are forwarded. Any secret a plugin needs must be named in this array. |

---

## Capabilities

### `auth_provider`

Replaces the built-in bypass-auth with a real OAuth provider. Only the first discovered `auth_provider` plugin is used.

The plugin implements a **standalone OAuth dance** and creates dashboard sessions by calling core's `POST /api/auth/session`. Core has zero provider-specific knowledge — it only issues JWT session cookies when a trusted plugin presents a verified user profile.

#### Standalone flow (primary)

```
Browser          Core                     Plugin              Provider
  │                │                        │                    │
  │ GET /auth/...  │                        │                    │
  │───────────────►│                        │                    │
  │ 302 → /login   │                        │                    │
  │◄───────────────│                        │                    │
  │ GET /login     │                        │                    │
  │───────────────────────────────────────►│                    │
  │ 302 → provider │                        │                    │
  │◄───────────────────────────────────────│                    │
  │                                         Provider OAuth dance │
  │◄────────────────────────────────────────────────────────────│
  │ GET /callback?code=…                   │                    │
  │───────────────────────────────────────►│                    │
  │                │  POST /api/auth/session│                    │
  │                │◄───────────────────────│                    │
  │                │  Set-Cookie: auth_token│                    │
  │                │───────────────────────►│                    │
  │ 302 → /        │                        │                    │
  │◄───────────────────────────────────────│                    │
```

The plugin must implement:

**`GET /health`** — required by registry health-check.

Response: `{"ok":true}`

**`GET /login?nonce=<jwt>`** — entry point; core forwards here with a one-time nonce.

Redirects to the OAuth provider's authorization URL. Must embed the nonce in the OAuth state parameter so it survives the round-trip.

**`GET /callback?code=<code>&state=<state>`** — OAuth callback.

1. Validate CSRF state.
2. Extract nonce from state.
3. Exchange code for access token.
4. Fetch user profile from provider.
5. Call `POST /api/auth/session` (see below).
6. Forward the `auth_token` cookie to the browser.
7. Redirect to `DASHBOARD_URL/`.

**`POST /api/auth/session`** on core — called by the plugin, not the browser.

Request headers:
```
Authorization: Bearer <DASHBOARD_AUTH_PLUGIN_SECRET>
Content-Type: application/json
```

Request body:
```json
{
  "github_id":    "12345",
  "login":        "username",
  "display_name": "Full Name",
  "avatar_url":   "https://...",
  "nonce":        "<jwt-nonce-from-GET-/login>"
}
```

Response: `200 OK` with `Set-Cookie: auth_token=<jwt>`.

#### Required environment variables

| Variable | Description |
|----------|-------------|
| `DASHBOARD_URL` | Base URL of the dashboard (e.g. `http://127.0.0.1:13120`) |
| `DASHBOARD_AUTH_PLUGIN_SECRET` | Shared secret ≥32 chars for `POST /api/auth/session` |
| Provider credentials | Named in `plugin.json` `env` array |

#### Legacy capability routes (deprecated)

The following routes were used by the old in-core proxy flow and are retained for backwards compatibility. New plugins should implement the standalone flow above instead.

| Method | Path | Description |
|--------|------|-------------|
| `GET`  | `/capabilities/auth/authorize-url?state=&redirect_uri=` | Returns provider authorization URL |
| `POST` | `/capabilities/auth/exchange` | Exchanges OAuth code for access token |
| `GET`  | `/capabilities/auth/user` | Returns user profile for Bearer token |

---

### `route_extension` (future)

Reserved for plugins that mount additional HTTP routes into the dashboard router. Not yet implemented.

---

## How to register a plugin

1. Build your plugin binary.
2. Create a directory under your plugin dir:
   ```
   /path/to/plugins/
   └── my-plugin/
       ├── plugin.json
       └── my-plugin   ← compiled binary
   ```
3. Set the env var (or config key) before starting the dashboard:
   ```bash
   export PLUGIN_DIR=/path/to/plugins
   ./agent-dashboard serve
   ```
   Or add `"plugin_dir": "/path/to/plugins"` to your JSON config file.

The registry scans every subdirectory of `plugin_dir` for `plugin.json` at startup. Missing or malformed descriptors are skipped with a warning; a failed health check kills the process and skips the plugin.

---

## Security

- Plugins **must** bind to `127.0.0.1` only — never a public address.
- The dashboard kills any plugin process it started on shutdown (`Registry.Shutdown()`).
- Only a fixed base set of env vars (PATH, HOME, TMPDIR, TEMP, USER, LANG, LC_ALL) plus any var names listed in the `env` array in `plugin.json` are forwarded to the plugin process.
- `DASHBOARD_AUTH_PLUGIN_SECRET` must be at least 32 characters. Store it in a `.env` file, never commit it.
- Health check timeout is **5 seconds**. Plugins that do not respond in time are skipped.

---

## Reference: github-oauth plugin

`plugins/github-oauth/` is the canonical reference implementation of the `auth_provider` capability.

### Files

| File          | Purpose |
|---------------|---------|
| `plugin.json` | Descriptor — capability `auth_provider`, addr `127.0.0.1:19001`, command `./github-oauth` |
| `go.mod`      | Standalone Go module (`github.com/lx-wnk/agent-dashboard-plugin-github-oauth`) |
| `main.go`     | HTTP server implementing standalone OAuth flow + legacy capability routes + `/health` |

### Setup

```bash
# 1. Build the plugin binary
cd plugins/github-oauth
GOWORK=off go build -o github-oauth .

# 2. Export credentials
export GITHUB_CLIENT_ID=your_client_id
export GITHUB_CLIENT_SECRET=your_client_secret
export DASHBOARD_URL=http://127.0.0.1:13120
export DASHBOARD_AUTH_PLUGIN_SECRET=$(openssl rand -hex 32)

# 3. Point the dashboard at the plugin dir and start
export PLUGIN_DIR=/path/to/plugins   # directory containing github-oauth/
./agent-dashboard serve
```

The dashboard logs `plugin: loaded id=github-oauth capabilities=[auth_provider]` on success.

### Endpoint summary

| Method | Path | Description |
|--------|------|-------------|
| `GET`  | `/health` | Health check — returns `{"ok":true}` |
| `GET`  | `/login?nonce=<jwt>` | Start OAuth dance (primary entry point) |
| `GET`  | `/callback?code=&state=` | OAuth callback — creates session, redirects to dashboard |
| `GET`  | `/capabilities/auth/authorize-url` | Legacy: returns GitHub authorization URL |
| `POST` | `/capabilities/auth/exchange` | Legacy: exchanges OAuth code for access token |
| `GET`  | `/capabilities/auth/user` | Legacy: returns user profile for Bearer token |
```

- [ ] **Step 2: Commit**

```bash
git add docs/plugin-guide.md
git commit -m "docs: update plugin guide — document standalone auth flow, fix env var names"
```

---

## Task 3: ARCH-05 — tygo Codegen Integration

**Files:**
- Create: `tygo.yaml`
- Modify: `sdk/types.go` — add `//go:generate` comment
- Modify: `Taskfile.yml` — prepend tygo to generate task
- Create: `src/sdk.generated.ts` — codegen output (committed)
- Modify: `src/types.ts` — import from generated, remove duplicates

### Step 1: Create `tygo.yaml` at repo root

```yaml
packages:
  - path: github.com/lx-wnk/agent-dashboard/sdk
    output_path: src/sdk.generated.ts
    type_mappings:
      # AgentStatus is a named string type with const declarations;
      # tygo generates the union automatically from the const block — no override needed.
      # Add overrides here if tygo output diverges from the canonical TS definition.
```

Note: `output_path` is relative to the `tygo.yaml` file location (repo root). `path` is the full Go import path — tygo resolves it via the active go.work workspace.

- [ ] **Step 2: Run tygo to preview output**

From the repo root:
```bash
$(go env GOPATH)/bin/tygo generate
```

Expected: `src/sdk.generated.ts` is created. Check it contains `TokenUsage`, `SessionMeta`, `SubAgent`, `TaskInfo`, `AgentStatus` interfaces/types.

If `AgentStatus` is generated as `string` (not a union), add to `tygo.yaml` type_mappings:
```yaml
      AgentStatus: "\"active\" | \"waiting\" | \"idle\""
```

Re-run and confirm the union appears correctly.

- [ ] **Step 3: Verify `src/sdk.generated.ts` output**

Open `src/sdk.generated.ts` and confirm:
- `TokenUsage` has `inputTokens`, `outputTokens`, `cacheCreationTokens`, `cacheReadTokens` (all `number`)
- `SessionMeta` has `inputTokens`, `outputTokens`, `linesAdded`, etc.
- `AgentStatus` is a union `"active" | "waiting" | "idle"` (not plain `string`)
- `SubAgent` and `TaskInfo` are present

Do NOT manually edit `src/sdk.generated.ts` — it is owned by tygo.

- [ ] **Step 4: Add `//go:generate` comment to `sdk/types.go`**

At the top of `sdk/types.go`, after the `package sdk` declaration, add:

```go
//go:generate tygo generate --config ../tygo.yaml
```

The full top of the file should look like:

```go
// Package sdk provides shared types for the agent-dashboard modules.
package sdk

//go:generate tygo generate --config ../tygo.yaml
```

- [ ] **Step 5: Update `Taskfile.yml` generate task**

The current generate task in `Taskfile.yml`:
```yaml
  generate:
    desc: Run go generate (ent schemas)
    dir: server
    cmd: go generate ./...
```

Replace with:
```yaml
  generate:
    desc: Run go generate (ent schemas) and tygo TS codegen
    cmds:
      - cmd: $(go env GOPATH)/bin/tygo generate --config tygo.yaml
        dir: '{{.ROOT_DIR}}'
      - cmd: go generate ./...
        dir: '{{.ROOT_DIR}}/server'
```

- [ ] **Step 6: Run `task generate` to verify the full pipeline**

```bash
task generate
```

Expected: tygo runs first (no errors), then `go generate ./...` runs (ent schemas). `src/sdk.generated.ts` is up to date.

- [ ] **Step 7: Migrate `src/types.ts` to import from generated**

Open `src/sdk.generated.ts` and note the exact exported names (they should be `TokenUsage`, `SessionMeta`, `SubAgent`, `TaskInfo`, `AgentStatus`).

In `src/types.ts`:

**a) Remove** the following interface definitions (they are now generated):
- `export interface TokenUsage { ... }` (lines ~1-6)
- `export interface SessionMeta { ... }` (lines ~8-19)
- `export interface SubAgent { ... }` (lines ~67-73)
- `export interface TaskInfo { ... }` (lines ~94-99)
- `export const AGENT_STATUSES = ['active', 'waiting', 'idle'] as const` — **keep this line** (needed for runtime use in `src/utils/agentSort.ts`)
- `export type AgentStatus = typeof AGENT_STATUSES[number]` — **remove this line** (will import from generated)

**b) Add** at the top of `src/types.ts` (before any existing `export`):

```typescript
// Types generated from sdk/types.go via tygo — do not edit these directly.
// Run `task generate` to regenerate after changing sdk/types.go.
export type { TokenUsage, SessionMeta, SubAgent, TaskInfo, AgentStatus } from './sdk.generated'
```

**c) Verify** `AGENT_STATUSES` still compiles by checking `src/utils/agentSort.ts` — it imports `AGENT_STATUSES` and `AgentStatus` from `../types`, both of which are still exported (AGENT_STATUSES as value, AgentStatus re-exported from generated).

- [ ] **Step 8: Typecheck**

```bash
pnpm typecheck
```

Expected: no errors. If you see "Property X does not exist on type Y" for SessionMeta fields like `firstPrompt`, check if tygo generated `firstPrompt: string` while `src/types.ts` previously had `firstPrompt: string | null`. The generated type is `string` (matching Go); update any null-checks in the codebase to use `!agent.meta?.firstPrompt` (falsy check) instead of `=== null`.

- [ ] **Step 9: Run unit tests**

```bash
pnpm test
```

Expected: all tests pass. The `format.test.ts` still imports `TokenUsage` from `../types` which now re-exports from generated — should be seamless.

- [ ] **Step 10: Commit**

```bash
git add tygo.yaml sdk/types.go Taskfile.yml src/sdk.generated.ts src/types.ts
git commit -m "feat(arch): add tygo codegen for Go→TS type sync (ARCH-05)"
```

---

## Task 4: TODOS Cleanup

**Files:**
- Modify: `docs/local/TODOS.md`

- [ ] **Step 1: Mark resolved items**

In `docs/local/TODOS.md`:

1. Mark gzip bug as resolved:
```
* ~~Open follow-up from the quality review: gzipResponseWriter missing http.Flusher forwarding + Vary: Accept-Encoding header — pre-existing issue, not in this PR's scope but worth a dedicated fix.~~ ✅ Already fixed in router.go — verified by TestGzipResponseWriter_ImplementsFlusher and TestGzipMiddleware_SetsVaryHeader.
```

2. Mark plugin guide as resolved:
```
* ~~Introduce a "plugin guide" — document how to build plugins, using github-oauth as reference~~ ✅ Updated in docs/plugin-guide.md — standalone OAuth flow documented as primary.
```

3. Mark UX-46 as resolved:
```
* ~~**UX-46 [Security/UX]** `ApiKeySettings.vue` — API key shown in full after creation; not masked.~~ ✅ Fixed — maskToken() utility + show/hide toggle added.
```

4. Mark ARCH-05 as resolved (partial):
```
* ~~**ARCH-05 [Arch]** `sdk/types.go` ↔ `src/types.ts` — dual type definitions require manual sync.~~ ✅ Partial fix — tygo generates TokenUsage, SessionMeta, SubAgent, TaskInfo, AgentStatus from Go. Agent stays manual (TS-only fields + type refinements). Follow-up: migrate Agent once Go types have proper string-enum const declarations.
```

- [ ] **Step 2: Commit**

```bash
git add docs/local/TODOS.md
git commit -m "docs: mark resolved TODOs (UX-46, plugin guide, ARCH-05, gzip bug)"
```
