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

The plugin must implement:

#### `GET /capabilities/auth/authorize-url`

Query parameters: `state`, `redirect_uri`

Response `200 application/json`:
```json
{ "url": "https://provider.example.com/oauth/authorize?..." }
```

#### `POST /capabilities/auth/exchange`

Request body `application/json`:
```json
{ "code": "<oauth-code>", "redirect_uri": "<redirect-uri>" }
```

Response `200 application/json`:
```json
{ "token": "<access-token>" }
```

#### `GET /capabilities/auth/user`

Request header: `Authorization: Bearer <access-token>`

Response `200 application/json` matching `auth.OAuthUserProfile`:
```json
{
  "ID":          "12345",
  "Login":       "username",
  "DisplayName": "Full Name",
  "AvatarURL":   "https://..."
}
```

---

### `route_extension` (future)

Reserved for plugins that mount additional HTTP routes into the dashboard router. Not yet implemented.

---

## How to register a plugin

1. Build your plugin binary.
2. Create a directory inside `PLUGIN_DIR` (default: unset — no plugins loaded):
   ```
   $PLUGIN_DIR/
   └── my-plugin/
       ├── plugin.json
       └── my-plugin   ← compiled binary
   ```
3. Set the env var before starting the dashboard:
   ```bash
   export PLUGIN_DIR=/path/to/plugins
   ./agent-dashboard serve
   ```
   Or add `"plugin_dir": "/path/to/plugins"` to your JSON config file.

The registry scans every subdirectory of `PLUGIN_DIR` for `plugin.json` at startup. Missing or malformed descriptors are skipped with a warning; a failed health check kills the process and skips the plugin.

---

## Security

- Plugins **must** bind to `127.0.0.1` only — never a public address.
- The dashboard kills any plugin process it started on shutdown (`Registry.Shutdown()`).
- Only a fixed base set of env vars (PATH, HOME, TMPDIR, TEMP, USER, LANG, LC_ALL) plus any var names listed in the `env` array in `plugin.json` are forwarded to the plugin process. Any secret a plugin needs (e.g. `GITHUB_CLIENT_SECRET`) must be named in `env`.
- Health check timeout is **5 seconds**. Plugins that do not respond in time are considered failed and are not registered.

---

## Reference: github-oauth plugin

`plugins/github-oauth/` is the canonical reference implementation of the `auth_provider` capability.

### Files

| File          | Purpose |
|---------------|---------|
| `plugin.json` | Descriptor — capability `auth_provider`, addr `127.0.0.1:19001`, command `./github-oauth` |
| `go.mod`      | Standalone Go module (`github.com/lx-wnk/agent-dashboard-plugin-github-oauth`) |
| `main.go`     | HTTP server implementing all three `auth_provider` endpoints + `/health` |

### Setup

```bash
# 1. Build the plugin binary
cd plugins/github-oauth
GOWORK=off go build -o github-oauth .

# 2. Export credentials
export GITHUB_CLIENT_ID=your_client_id
export GITHUB_CLIENT_SECRET=your_client_secret

# 3. Point the dashboard at the plugin dir and start
export PLUGIN_DIR=/path/to/plugins   # directory containing github-oauth/
./agent-dashboard serve
```

The dashboard logs `plugin: loaded id=github-oauth capabilities=[auth_provider]` on success.

### Endpoint summary

| Method | Path | Description |
|--------|------|-------------|
| `GET`  | `/health` | Health check — returns `{"ok":true}` |
| `GET`  | `/capabilities/auth/authorize-url?state=&redirect_uri=` | Returns GitHub authorization URL |
| `POST` | `/capabilities/auth/exchange` | Exchanges OAuth code for access token |
| `GET`  | `/capabilities/auth/user` | Returns user profile for Bearer token |
