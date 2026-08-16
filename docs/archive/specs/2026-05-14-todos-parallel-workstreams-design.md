# Design: Parallel Workstreams — TODOS Implementation

**Date:** 2026-05-14
**Base branch:** `upcoming`
**Strategy:** Two-wave parallel worktrees, each producing one PR back to `upcoming`

---

## Scope

Implement all items from `docs/local/TODOS.md` except "Specific company ideas" (SSO, Memory/Xaver adapters). Competitor analysis deferred to a later cycle.

---

## Execution Strategy

### Wave 1 — Full parallel (all independent)

| PR | Branch | Size | Description |
|----|--------|------|-------------|
| PR-A | `fix/gzip-flusher` | XS | Gzip bug fix |
| PR-B | `feat/test-suite` | M | Test suite improvements |
| PR-C | `chore/server-arch-review` | M | Server architecture review |
| PR-D | `feat/plugin-system` | L | Plugin system + GitHub auth extraction |
| PR-F | `feat/custom-system-prompts` | M | Custom system prompts per stage |
| PR-G | `feat/cli-app` | L | CLI application |

### Wave 1 addition (independent)

| PR | Branch | Description |
|----|--------|-------------|
| PR-E | `feat/llm-adapters` | LLM adapter system (built-in adapters only; plugin-based adapters can be added later via PR-D's route_extension) |

---

## PR-A: Gzip Bug Fix

### Problem
`gzipResponseWriter` in `server/internal/api/router.go:228` wraps `http.ResponseWriter` but does not implement `http.Flusher`. SSE handlers assert `w.(http.Flusher)` — when the gzip middleware wraps the response writer, this assertion yields `ok = false`, silently disabling SSE flushes. Additionally, the `Vary: Accept-Encoding` header is not set, which causes incorrect caching behavior.

### Fix
1. Add `Flush()` method to `gzipResponseWriter`:
   - Call `gz.Flush()` to flush the gzip buffer
   - Forward to inner writer's `Flush()` if it implements `http.Flusher`
2. Set `Vary: Accept-Encoding` in `gzipMiddleware` before serving the next handler
3. Add a unit test verifying that `gzipResponseWriter` satisfies `http.Flusher`

### Files
- `server/internal/api/router.go` — patch `gzipResponseWriter`, add `Vary` header

---

## PR-B: Test Suite Improvements

### Goals
- Increase API handler coverage with HTTP-level integration tests
- Cover auth middleware bypass logic
- Add frontend composable unit tests

### Go Backend Tests

**API integration tests** (`server/internal/api/*_integration_test.go`):
- Use `httptest.NewServer` per handler group
- Cover: agents list/stream, tasks CRUD, auth callbacks, MCP endpoint, API key management
- Use table-driven tests with recorded fixtures for JSONL parsing

**Auth middleware test** (`server/internal/api/middleware_test.go`):
- Verify `BypassAuth=true` behavior when loopback + no OAuth configured
- Verify JWT validation passes/fails correctly

**Missing coverage areas** (identify via `go test -coverprofile`):
- `server/internal/api/sessions/`
- `server/internal/api/history/`
- `server/internal/api/search/`
- `server/internal/api/analytics/`
- `server/internal/merger/` (integration path)

### Frontend Tests (Vitest)
- `src/composables/useAgents.test.ts` — SSE connection + polling fallback
- `src/composables/useTasks.test.ts` — task list state management
- `src/composables/useRole.test.ts` — RBAC composable

### Tooling
- Add `go test -coverprofile=coverage.out ./...` to `Taskfile.yml`
- Add coverage badge target

---

## PR-C: Server Architecture Review

### Deliverables
1. `docs/architecture/server-review.md` — findings document
2. Inline fixes committed alongside the doc (no separate PR)

### Review Scope
- **Layer compliance audit**: verify `db/*` → `pipeline/*` → `services/*` → `routes/*` import direction. Flag any upward imports.
- **Handler responsibility audit**: each handler in `server/internal/api/*/` should do HTTP decode → service call → HTTP encode. Flag handlers with business logic.
- **Middleware chain review**: security headers, gzip, auth, recovery — order and completeness.
- **Error handling patterns**: verify all handlers use `apierr` package consistently.
- **Context propagation**: ensure request contexts flow through to DB calls and spawner.

### Document Structure
```
# Server Architecture Review — 2026-05-14
## Layer Compliance
## Handler Responsibility
## Middleware Chain
## Error Handling
## Findings & Recommendations
## Action Items (P0/P1/P2)
```

---

## PR-D: Plugin System + GitHub Auth Extraction

### Plugin Contract (Sidecar HTTP)

Each plugin is an independent process that the server discovers on startup.

**Discovery:**
- Server reads all subdirectories of `plugin_dir` (config key `DASHBOARD_PLUGIN_DIR`)
- Each plugin must have a `plugin.json` at the root of its directory:

```json
{
  "id": "github-oauth",
  "version": "1.0.0",
  "capabilities": ["auth_provider"],
  "addr": "127.0.0.1:13200",
  "command": ["./github-oauth-plugin"],
  "env": ["GITHUB_CLIENT_ID", "GITHUB_CLIENT_SECRET"]
}
```

**Startup sequence:**
1. Server scans `plugin_dir`, reads `plugin.json` files
2. For each plugin with a `command` field: spawn the process
3. Poll `http://{addr}/health` until 200 or timeout (5s)
4. Register capabilities with the plugin registry

**Capability contracts:**

`auth_provider`:
- `GET /capabilities/auth/authorize-url?redirect_uri=...` → `{"url": "..."}`
- `POST /capabilities/auth/exchange` body: `{"code": "..."}` → `{"user": {...}}`
- `GET /capabilities/auth/provider-name` → `{"name": "github"}`

`route_extension`:
- Plugin exposes arbitrary routes; server proxies `GET|POST /api/plugins/{id}/*` to plugin

### Plugin Registry
New package `server/internal/plugin/`:
- `registry.go` — `Registry` struct: discover, start, health-check, stop all plugins
- `types.go` — `PluginDescriptor`, `Capability` types
- `proxy.go` — HTTP reverse proxy for `route_extension` capability

### GitHub Auth Extraction
1. Move `server/internal/auth/github/` → standalone plugin at `plugins/github-oauth/`
2. Plugin implements the `auth_provider` HTTP contract above
3. Core server: `OAuthProvider` interface already exists — wire it from plugin registry if `auth_provider` capability found
4. Server config: remove `GitHubClientID` / `GitHubClientSecret` (they move to the plugin's env)
5. `plugins/github-oauth/plugin.json` + `plugins/github-oauth/main.go` included in repo as reference implementation

### Plugin Guide
`docs/plugin-guide.md`:
- Plugin contract reference (all capability types)
- Step-by-step: build a plugin from scratch (language-agnostic)
- Worked example: GitHub OAuth plugin
- Security model: plugins only bind loopback, server → plugin calls are local-only

---

## PR-E: LLM Adapter System (Wave 1 — independent of PR-D)

### Interface

New interface in `server/internal/pipeline/types.go`:

```go
type LLMSpawner interface {
    // Spawn starts the LLM agent and returns a handle for monitoring.
    Spawn(ctx context.Context, args SpawnArgs) (*SpawnResult, error)
    // Name returns the adapter identifier (e.g. "claude", "openai", "ollama").
    Name() string
}

type SpawnArgs struct {
    TaskID      string
    StageRunID  string
    SystemPrompt string
    UserPrompt   string
    Model        string
    WorkDir      string
    AllowedTools []string
    Env          []string
}

type SpawnResult struct {
    PID       int    // 0 if not subprocess-based
    SessionID string // Claude session ID or equivalent
}
```

### Implementations

**`ClaudeSpawner`** (default, wraps existing `spawner.go`):
- Calls `claude` CLI subprocess
- No behavior change from current implementation

**`OllamaSpawner`**:
- Calls `POST http://{ollama_host}/api/chat` with model and messages
- Non-subprocess — manages response via HTTP streaming
- Completion detection: response is written synchronously; `SpawnResult.SessionID` is a synthetic ID
- Adapter writes the final JSON output block to a temporary session file matching `completionDetector`'s expected format: a single JSONL line with `{"role":"assistant","content":[{"type":"text","text":"...json block..."}]}`
- `PID` is 0; orchestrator treats result as immediately complete (no alive-PID poll loop)

**`OpenAISpawner`**:
- Calls OpenAI-compatible chat completions API
- Configurable base URL (supports any OpenAI-compatible endpoint, including local models)
- Same synchronous completion + synthetic session file pattern as `OllamaSpawner`

**`CustomCommandSpawner`**:
- Env: `DASHBOARD_SPAWN_COMMAND=/path/to/my-agent`
- Passes `SpawnArgs` as JSON via stdin
- Reads `SpawnResult` JSON from stdout

### Configuration

In `settings.json` / env vars:
```json
{
  "adapters": {
    "default": "claude",
    "stages": {
      "implementation": "ollama",
      "self_review": "claude"
    }
  },
  "ollama": {
    "host": "http://localhost:11434",
    "default_model": "llama3"
  },
  "openai": {
    "base_url": "https://api.openai.com/v1",
    "api_key_env": "OPENAI_API_KEY",
    "default_model": "gpt-4o"
  }
}
```

### API
- `GET /api/settings/adapters` — list configured adapters + available implementations
- `PUT /api/settings/adapters` — update adapter configuration

---

## PR-F: Custom System Prompts

### DB Schema

New ent schema `SystemPrompt`:
```
id          uuid (PK)
scope       enum: global | task
stage       nullable string (nil = all stages)
content     text
priority    int (higher = wins)
created_by  string
created_at  time
updated_at  time
```

### API
- `GET /api/settings/system-prompts` — list all prompts (admin only)
- `POST /api/settings/system-prompts` — create prompt
- `PUT /api/settings/system-prompts/{id}` — update prompt
- `DELETE /api/settings/system-prompts/{id}` — delete prompt

### Pipeline Integration

In `server/internal/pipeline/stage_prompts.go`:
1. Before building stage prompt, query `SystemPrompt` table: `scope=global`, ordered by `priority DESC`
2. Filter by `stage` (nil rows match all)
3. Winning prompt prepended to the built-in system prompt with a separator
4. Custom prompt is injected as the first section; built-in remains below

Resolution order (highest priority wins): task-scoped + stage-specific → task-scoped global → global stage-specific → global

### UI
- Settings panel new section: "System Prompts"
- List view with stage filter chip
- Textarea editor for content

---

## PR-G: CLI Application

### Package
New binary: `server/cmd/cli/main.go`

Uses Cobra (already a dependency), cobra-based subcommands.

### Commands

```
dashboard agents
  list              List all running agents (table format)
  inspect <id>      Show full agent details

dashboard tasks
  list              List tasks (filter by --status, --stage)
  create            Create new task (interactive or --file=spec.json)
  cancel <id>       Cancel a task
  logs <id>         Stream task stage output (SSE)

dashboard pipeline
  status            Show pipeline config + runner slots
  config get <key>  Read pipeline config value
  config set <key> <value>  Update pipeline config

dashboard config
  set host <url>    Set dashboard server URL
  set token <key>   Set API bearer token
  show              Print current config
```

### Config File
`~/.config/dashboard/config.json`:
```json
{
  "host": "http://127.0.0.1:13120",
  "token": "mcp_..."
}
```

### Auth
Reuses existing MCP API tokens (scope `tasks:read` minimum, `tasks:write` for mutations, `pipeline:control` for pipeline commands). User creates a token via the web UI settings panel and adds it to CLI config.

### Output Formats
- Default: human-readable table (using `text/tabwriter`)
- `--json` flag: raw JSON (for scripting)
- `--quiet` flag: IDs only

### Error Handling
- Non-2xx responses: print error body + exit code 1
- Connection refused: clear message "Is the dashboard server running at {host}?"

---

## Dependencies & Merge Order

All 7 PRs are independent and can be developed and merged in any order. No wave structure required.

```
PR-A ──────────────────────────────────────────────► merge (independent)
PR-B ──────────────────────────────────────────────► merge (independent)
PR-C ──────────────────────────────────────────────► merge (independent)
PR-D ──────────────────────────────────────────────► merge (independent)
PR-E ──────────────────────────────────────────────► merge (independent)
PR-F ──────────────────────────────────────────────► merge (independent)
PR-G ──────────────────────────────────────────────► merge (independent)
```

Note: Plugin-based LLM adapters (future work) would need PR-D merged first, but the built-in adapters in PR-E are self-contained.

---

## Out of Scope

- Competitor analysis (deferred)
- SSO via Office365 (deferred)
- Memory/Xaver adapter (deferred)
- Agent-driven MCP for status updates (already implemented via existing MCP endpoint + channel bridge)
