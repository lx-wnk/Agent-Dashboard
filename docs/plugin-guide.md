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
  │ GET /api/auth/github │                   │                    │
  │───────────────►│                        │                    │
  │ 302 → <plugin>/login │                  │                    │
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

Redirects to the OAuth provider's authorization URL. Must embed the nonce in the CSRF state value as `<csrfState>.<nonce>` — pass this combined value both as the state cookie and as the OAuth `state` parameter. The nonce is recovered in the callback by splitting the cookie value on the first `.` (the nonce is a JWT that may contain dots, so split only on the first one).

**`GET /callback?code=<code>&state=<state>`** — OAuth callback.

1. Compare the `state` query parameter against the `github_oauth_state` cookie value — reject if they differ.
2. Extract nonce: split the cookie value on the first `.` only — everything after is the nonce (a JWT that may itself contain dots).
3. Exchange code for access token.
4. Fetch user profile from provider.
5. Call `POST /api/auth/session` (see below).
6. Forward the `auth_token` cookie to the browser.
7. Redirect to `DASHBOARD_URL/`.

**Calling core to create a session: `POST /api/auth/session`**

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
  "nonce":        "<value received as ?nonce= on GET /login — pass verbatim>"
}
```

Response: `200 OK` with `Set-Cookie: auth_token=<jwt>`.

#### Required environment variables

| Variable | Description |
|----------|-------------|
| `DASHBOARD_URL` | Base URL of the dashboard (e.g. `http://127.0.0.1:13120`) |
| `DASHBOARD_AUTH_PLUGIN_SECRET` | Shared secret ≥32 chars for `POST /api/auth/session` |
| `GITHUB_CLIENT_ID` | GitHub OAuth app client ID (github-oauth specific; name in `plugin.json` `env` array) |
| `GITHUB_CLIENT_SECRET` | GitHub OAuth app client secret (github-oauth specific; name in `plugin.json` `env` array) |

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

### `ui_extension`

Lets a plugin contribute frontend UI into named slots of the dashboard SPA. The plugin
serves a **UI manifest** plus one JS module per slot through its own reverse-proxied
file space; the dashboard imports a module only when a host renders that slot.

**1. Declare the capability** in `plugin.json`:

```json
{ "capabilities": ["ui_extension"] }
```

**2. Serve a UI manifest** at `/ui-manifest.json` within your plugin's HTTP server. The
dashboard fetches it via the reverse proxy at `/api/plugins/{id}/proxy/ui-manifest.json`.

Each slot entry has one required field (`slot`, `module`) and two optional ones:

| Field | Type | Description |
|-------|------|-------------|
| `slot` | string | Slot name to target (see contract table below). |
| `module` | string | Relative path to the ES module served by your plugin (e.g. `task-footer.js`). Must not start with `/` and must not traverse `..`. |
| `priority` | number | Render order within a slot. Higher priority renders outer/first. Omit for default (0). |
| `mode` | `"override"` \| `"extend"` | Composition mode (see below). Omit to register as an independent sibling. |

```json
{
  "slots": [
    { "slot": "task-modal-footer", "module": "task-footer.js" },
    { "slot": "task-modal-footer", "module": "task-footer-wrapper.js", "priority": 10, "mode": "extend" },
    { "slot": "kanban-card-badge", "module": "badge.js", "priority": 100, "mode": "override" }
  ]
}
```

**3. Serve each module** (e.g. `task-footer.js`) as an ES module whose `default` export is
a `SlotAddon`. The dashboard imports it from `/api/plugins/{id}/proxy/{module}`.

```js
export default {
  mount(el, ctx) {
    // ctx shape depends on the slot (see the contract table below)
    el.textContent = ctx.task.slug
    return () => { /* teardown */ }
  },
}
```

**Composition modes** -- when multiple addons target the same slot, the dashboard resolves
them as follows:

- **No mode (sibling, default):** each addon is mounted into its own independent host
  element, in load order. All siblings coexist.
- **`override`:** the highest-priority `override` addon owns the slot exclusively. Every
  addon in the priority-sorted chain below it -- including other `extend` entries and all
  siblings -- is suppressed and never mounted.
- **`extend`:** addons with `mode: "extend"` are sorted by `priority` (descending) into a
  composition chain. The outermost (highest priority) is mounted first; its `mount`
  function receives a `parent` handle it may use to mount the next addon in the chain:

```js
export default {
  mount(el, ctx, parent) {
    // render this addon's own content
    const label = document.createElement('span')
    label.textContent = 'wrapper'
    el.appendChild(label)
    // compose the lower-priority chain below
    if (parent) {
      const child = document.createElement('div')
      el.appendChild(child)
      parent.mount(child)
    }
    return () => { /* teardown */ }
  },
}
```

If an `override` appears in the chain, `parent` is `null` for it -- it does not delegate
further. Siblings (no mode) are always mounted after the chain, regardless of priority,
unless an `override` is present (which suppresses them entirely).

**Available slots and their `ctx` contract** (SSOT: `src/utils/pluginSlot.ts`):

| Slot name                | Host             | `ctx` (props in)                                  |
| ------------------------ | ---------------- | ------------------------------------------------- |
| `refinement-input-addon` | RefinementChat   | `{ insertText(text), setBusy(bool) }` (callbacks) |
| `task-modal-footer`      | TaskModal footer | `{ task }`                                         |
| `agent-modal-footer`     | AgentModal footer| `{ agent }`                                        |
| `kanban-card-badge`      | TaskCard         | `{ task }`                                         |
| `settings-panel`         | PluginSettings   | `{}` (no entity context)                          |

The addon's only callback *out* is the `UnmountFn` it returns from `mount`, invoked when
the host unmounts.

**Lifecycle (v1):** `discover` (boot-static registry) → `health-check` → `load-manifest`
→ `import module` → `mount(el, ctx)` → `unmount` when the host component unmounts. Imports
are memoized per page load (plugin list + each manifest + each module fetched once).

**Legacy fallback:** a `route_extension` plugin that ships **no** manifest but serves an
`addon.js` whose `default.slot` matches the requested slot still works — this keeps the
original voice-input plugins functioning unchanged.

**Non-goal (v1):** hot plugin add/remove without a page reload. The plugin registry is
boot-static; adding or removing a plugin requires a server restart and a browser reload.

**Security:** modules are imported **only** from `/api/plugins/{id}/proxy/*`, served by
the registry-discovered, health-checked, SSRF-guarded plugin proxy — never an arbitrary
URL. A plugin must be discovered and healthy before any of its UI can load.

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
   export DASHBOARD_PLUGIN_DIR=/path/to/plugins
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
export DASHBOARD_PLUGIN_DIR=/path/to/plugins   # directory containing github-oauth/
./agent-dashboard serve
```

The dashboard logs `plugin: loaded id=github-oauth capabilities=[auth_provider]` on success.

### Endpoint summary

| Method | Path | Description |
|--------|------|-------------|
| `GET`  | `/health` | Health check — returns `{"ok":true}` |
| `GET`  | `/login?nonce=<jwt>` | Start OAuth dance (primary entry point) |
| `GET`  | `/callback?code=<code>&state=<state>` | OAuth callback — creates session, redirects to dashboard |
| `GET`  | `/capabilities/auth/authorize-url` | Legacy: returns GitHub authorization URL |
| `POST` | `/capabilities/auth/exchange` | Legacy: exchanges OAuth code for access token |
| `GET`  | `/capabilities/auth/user` | Legacy: returns user profile for Bearer token |

---

## Reference: office365-oauth plugin

`plugins/office365-oauth/` implements the `auth_provider` capability for Microsoft single-tenant Azure AD.

### Files

| File | Purpose |
|------|---------|
| `plugin.json` | Descriptor — capability `auth_provider`, addr `127.0.0.1:19002`, command `./office365-oauth` |
| `go.mod` | Standalone module (`github.com/lx-wnk/agent-dashboard-plugin-office365-oauth`) |
| `main.go` | HTTP server implementing standalone OAuth2 flow |

### Azure App Registration

1. Go to [Azure portal](https://portal.azure.com) → **Azure Active Directory** → **App registrations** → **New registration**.
2. Set redirect URI to `http://127.0.0.1:19002/callback` (type: Web).
3. Under **Certificates & secrets**, create a new client secret.
4. Under **API permissions**, add `User.Read` (delegated). If using group restriction, also add `GroupMember.Read.All` (delegated). Grant admin consent.

### Setup

```bash
# 1. Build the plugin binary
cd plugins/office365-oauth
GOWORK=off go build -o office365-oauth .

# 2. Export credentials
export AZURE_CLIENT_ID=your_application_client_id
export AZURE_CLIENT_SECRET=your_client_secret
export AZURE_TENANT_ID=your_tenant_directory_id
export DASHBOARD_URL=http://127.0.0.1:13120
export DASHBOARD_AUTH_PLUGIN_SECRET=$(openssl rand -hex 32)

# Optional: restrict to a specific Azure AD group
export OFFICE365_ALLOWED_GROUP_ID=your_group_object_id

# 3. Point the dashboard at the plugin dir and start
export PLUGIN_DIR=/path/to/plugins   # directory containing office365-oauth/
./agent-dashboard serve
```

### Endpoint summary

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Health check — returns `{"ok":true}` |
| `GET` | `/login?nonce=<jwt>` | Start OAuth dance (primary entry point) |
| `GET` | `/callback?code=&state=` | OAuth callback — creates session, redirects to dashboard |
