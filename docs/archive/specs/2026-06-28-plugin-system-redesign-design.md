# Plugin System Redesign — Design Spec

> Date: 2026-06-28 · Status: Draft for review · Branch: `feat/plugin-system-redesign`
> Soft prerequisite: PR #230 (DB-backed settings) — reuses its settings infra; the new `plugin` table supersedes #230's interim flat `plugins.enabled` list.

## Why

The current plugin system (boot-mounted reverse proxies, `plugin.json` discovery, a crash-restart watcher, no enable/disable concept) cannot deliver UI-controlled enable/disable. PR #230's interim made enablement restart-to-apply to kill an orphan-restart bug. This redesign builds a real lifecycle: enable/disable from the UI, **live where achievable**, restart only for the genuinely boot-bound minority, plus per-plugin settings and composable UI slots.

## Research grounding (2026-06-28)

Four parallel research sweeps (full sources in the session log) established:

- **Go (R1):** Native `plugin` (.so) is unsuitable (no unload, CGO, exact-version coupling). Stay on **subprocess + reverse-proxy** (language-agnostic, crash-isolated) — the most capable model for runtime enable/disable; Vault/Grafana are the references. chi cannot mutate routes after serving (issue #480) → use a **catch-all dispatcher** resolving an `atomic`/`RWMutex` registry per request. Process hygiene: `Setpgid` groups, supervised goroutine, group-kill, **suppress auto-restart on intentional stop**.
- **Restart (R2):** Web-triggered **supervised restart** is legit best-practice (Traefik-Manager, Gitea): `202` → graceful `Shutdown` → `os.Exit` → supervisor restarts; frontend reconnect-overlay polls `/health`; **validate-before-restart** avoids lockout. `tableflip`/socket-activation = overkill for single-user local.
- **Vue (R3):** **Manifest-driven dynamic ESM import** (`defineAsyncComponent(() => import(url))`) is best-fit for a Vite host with a controlled SDK. Enable/disable live via `v-if`; only *full code unload* needs a page reload (browser ES-module registry is permanent — show a "refresh to fully unload" notice, like Grafana/Backstage). Plugin SDK must externalize Vue (singleton). Module Federation = overkill; iframe = only if untrusted/isolation required. `router.addRoute()` is live.
- **PHP (R4):** Shopware's "problem-free" activation is a side-effect of request-per-process (compiled container = serialized state; activate = DB flag + `cache:clear` → next request reboots). Does NOT transfer free to a long-running daemon. **Borrow the patterns**: DB activation state read at boot, Install/Activate/Deactivate/Uninstall lifecycle hooks, per-plugin versioning/upgrade, manifest discovery, install-vs-activate split, content-hash → reload trigger.

## Core decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | Plugins stay **external subprocesses + reverse-proxy** | Language-agnostic, crash-isolated; best for runtime enable/disable (R1). No `.so`, no Wasm (Wasm noted as a future option). |
| D2 | **Ambition = "live where cheap, restart where needed"** | `route_extension` live (catch-all dispatcher), `ui_extension` live (dynamic ESM), `auth_provider`+bind-port → supervised restart. Honest: those two are boot-wired and cannot be live. |
| D3 | **Full lifecycle** (install/activate/deactivate/uninstall + update) with HTTP hooks | User choice; supports setup/validation/teardown/upgrades. Plugins own their storage — the dashboard never runs a plugin's migrations. |
| D4 | **Per-plugin settings**; secrets **encrypted at rest** | API keys must be UI-settable without plaintext in the readable DB. AES-GCM with a master key bootstrapped like the existing hooks-secret. |
| D5 | **Composable slots**: manifest declares `slot` + `priority` + `mode (override\|extend)`; extend can call the parent | Multiple plugins can target one slot, ordered, and wrap rather than replace. |
| D6 | Decompose into **SP1–SP5**, each its own spec→plan→build | Multi-subsystem; SP1 is the backbone. |

## Decomposition

| SP | Scope | Depends |
|---|---|---|
| **SP1** | **Lifecycle foundation** (this spec): `plugin` table, manifest v2, lifecycle state machine + HTTP hooks, per-plugin settings + encrypted secrets, discovery, lifecycle API, migration from #230. | — |
| SP2 | **Live backend dispatch + process mgmt**: catch-all `/plugins/{id}/*` + atomic registry; `Setpgid` groups, supervised goroutine, group-kill, suppress-restart-on-stop; transient-start for lifecycle hooks. → `route_extension` live. | SP1 |
| SP3 | **Web-triggered supervised restart**: `/api/admin/restart` (202→validate→graceful→exit), reconnect overlay, supervisor setup docs. → `auth_provider` + bind-port. | SP1 |
| SP4 | **Frontend dynamic UI extensions**: `/api/ui-extensions` manifest, `defineAsyncComponent` slot renderer with **priority ordering + override/extend (parent) chain**, per-plugin settings UI section, "refresh-to-unload" notice. → `ui_extension` live. | SP1 |
| SP5 | **Plugin SDK + author docs**: manifest schema, lifecycle contract, Vue-externalize build config, capability + slot + settings reference. | SP1–4 |

---

# SP1 — Lifecycle Foundation (detailed)

## SP1.1 Manifest `plugin.json` v2 (backward-compatible)

Existing fields (`id`, `capabilities`, `addr`, `command`, `env`) keep working unchanged; new fields are all optional:

```jsonc
{
  "id": "voice-whisper", "name": "Voice (Whisper)", "version": "1.2.0",
  "capabilities": ["route_extension"],
  "addr": "127.0.0.1:19010", "command": ["./voice-whisper"], "env": ["WHISPER_KEY"],
  "slots":    [{ "slot": "agent-toolbar", "priority": 100, "mode": "extend" }],
  "settings": [{ "key": "endpoint", "type": "url",    "label": "Endpoint" },
               { "key": "apiKey",   "type": "string", "label": "API Key", "secret": true }],
  "lifecycle":{ "install": "/lifecycle/install", "postInstall": "...", "activate": "/lifecycle/activate",
                "deactivate": "...", "update": "...", "uninstall": "..." },
  "permissions": ["net"]
}
```

- `settings[].type`: `string | url | int | bool | enum` (+ `enum` values), `secret: bool`.
- `slots[].mode`: `override` (replace) or `extend` (wrap; receives parent — rendered by SP4). `priority` orders multiple plugins on one slot (higher first).
- `lifecycle.*`: optional HTTP paths on the plugin's `addr`. Absent hook = no-op transition.
- Validation: `id` matches `^[a-z0-9][a-z0-9-]*$`; unknown manifest fields ignored (forward-compat); a parse error skips the plugin with a warning (existing behavior).

## SP1.2 DB model

**`plugin`** (ent schema):
- `id` string unique (the manifest id) · `name` · `version` · `installed_at` time nullable · `active` bool default false · `path` · `manifest_hash` · `created_at` · `updated_at`.
- Derived state: `installed_at == nil` → **discovered**; set + `active=false` → **inactive**; `active=true` → **active**. `manifest_hash` differs from stored → **update-available**.

**`plugin_setting`** (ent schema):
- `id` (uuid) · `plugin_id` (FK-ish string) · `key` · `value` (text) · `secret` bool · `nonce` (text, empty for non-secret) · timestamps. Unique on (`plugin_id`,`key`).
- Non-secret rows: `value` is plaintext. Secret rows: `value` is base64 AES-GCM ciphertext, `nonce` set.

## SP1.3 Secrets crypto (`internal/secretbox` or similar)

- Master key resolution (mirrors `LoadOrGenerateHooksSecret`): `DASHBOARD_SECRET_KEY` (env, 32-byte hex) if set; else generate 32 random bytes, persist to `~/.claude/dashboard-secret.key` (0600), reuse on next boot. Bootstrap/secret → **stays out of the DB** (consistent with the #230 env-only-secrets rule).
- `Encrypt(plaintext) -> (ciphertextB64, nonceB64)` / `Decrypt(...)` via AES-GCM (`crypto/aes` + `crypto/cipher`).
- Secret setting values are decrypted ONLY at the point of injecting them into a plugin's process env at start (SP2 consumes; SP1 provides the decrypt). API/UI never return secret plaintext — a masked sentinel (e.g. `"********"`) indicates "set"; PUT with the sentinel leaves it unchanged.

## SP1.4 Lifecycle engine (`internal/pluginlifecycle`)

A service over the `plugin` repo + a hook HTTP-caller. Transitions persist state, then POST the declared hook (if present) to `addr+path` with a small JSON body (`{id, version, fromVersion?}`); non-2xx aborts the transition with the hook's error surfaced.

- `Discover(ctx)`: scan `cfg.PluginDir`, upsert `plugin` rows from manifests (state=discovered if new), compute `manifest_hash`, flag update-available on change. Unknown/removed dirs reconciled.
- `Install(id)`: requires discovered; calls `install` then `postInstall` hooks; sets `installed_at`. (Process orchestration for hook delivery — incl. transient start of a stopped plugin — is **SP2**; SP1 calls the hook caller, which assumes reachability and errors cleanly if not.)
- `Activate(id)`: requires installed; calls `activate` hook; sets `active=true`. (Actual serve/start = SP2.)
- `Deactivate(id)`: calls `deactivate` hook; sets `active=false`. (Stop = SP2.)
- `Update(id)`: on version change; calls `update` hook (from→to); bumps `version`.
- `Uninstall(id)`: deactivate if active; calls `uninstall` hook; clears `installed_at`; deletes the plugin's `plugin_setting` rows (incl. secrets).

**SP1/SP2 boundary (explicit):** SP1 = state model, persistence, settings/secrets, discovery, hook HTTP-caller. SP2 = process lifecycle (start/stop, process groups, supervision, transient-start-for-hook, catch-all dispatch). So in SP1 alone, hooks only succeed against an already-running plugin; SP1 ships its logic + tests with a faked/stub hook-caller, and SP2 wires real process orchestration.

## SP1.5 API (`internal/api/plugins`, extended)

- `GET /api/plugins` — list: `{id, name, version, state, updateAvailable, capabilities, hasSettings}`. No secret values, no `BaseURL`/`Env` (preserve the existing narrow-DTO security guard).
- `POST /api/plugins/{id}/install|activate|deactivate|uninstall` — invoke the lifecycle engine; return new state or a 4xx/5xx with the hook error. (Distinguish unknown id → 400, hook/persist failure → 500, per the #230 error-class pattern.)
- `GET /api/plugins/{id}/settings` — manifest settings schema + current values (secrets masked).
- `PUT /api/plugins/{id}/settings` — validate against the schema; encrypt secret fields; persist; masked-sentinel leaves a secret unchanged.
- Routing mounts via `RouterDeps` in `router.go` (the established pattern), NOT the proxy-shadowed `/api/settings/plugins/{id}` path.

## SP1.6 Migration from #230

- One-time: seed `plugin` rows from the interim `plugins.enabled` app_setting list — each id → `active=true, installed_at=now()` (treat already-enabled as installed+active).
- The boot enablement predicate (`di.go`) reads `plugin.active` from the table instead of the `plugins.enabled` list.
- Deprecate the `plugins.enabled` registry key (remove from the settings registry; the new `plugin` table is the source). Keep #230's boot-skip behavior (disabled/unbuilt `auth_provider` doesn't trip the guard) sourced from the new table.

## SP1.7 Testing

- Manifest v2 parse + backward-compat (old plugin.json still loads; unknown fields ignored; new fields populate).
- DB repo: plugin upsert/get/list/state-derivation; plugin_setting upsert/get/list/delete + unique constraint.
- Secrets: encrypt→decrypt round-trip; wrong-key fails; masked-sentinel-on-PUT leaves value unchanged; master-key generate-and-persist + reuse.
- Lifecycle engine: each transition's state change + hook call (faked hook-caller asserts URL/body; non-2xx aborts + surfaces error); illegal transitions rejected (e.g. activate before install).
- Discovery: upsert new, detect version/hash change → update-available, reconcile removed.
- API: list shape (no secrets/BaseURL), lifecycle endpoints state changes + error classes, settings GET masking + PUT encrypt + sentinel-unchanged.
- Migration: `plugins.enabled` → `plugin` rows; boot predicate reads the table.

## Open dependencies / risks

- **ent regen** for two new schemas (`plugin`, `plugin_setting`) — run the project generate flow; revert non-schema `runtime.go`/`go.sum` drift; note: `go test ./...` regenerates and can corrupt the ent tree → restore the whole `server/internal/db/ent/` after full test runs.
- **#230 must merge first** (or SP1 branches off it) — SP1 reuses the settings infra + supersedes `plugins.enabled`.
- **SP1/SP2 boundary**: SP1's lifecycle hooks need a running plugin; full hook delivery (transient start) lands in SP2. SP1 is testable in isolation via a faked hook-caller.
- **Secret-key loss** = unreadable secret settings (acceptable; user re-enters keys). Document.

## Out of scope (later SPs)
Live dispatch/process mgmt (SP2), web restart (SP3), frontend slot rendering + per-plugin settings UI + override/extend chain (SP4), plugin SDK + docs (SP5).
