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
