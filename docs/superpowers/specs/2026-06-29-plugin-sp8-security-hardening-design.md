# Plugin SP8 — Security Hardening — Design Spec

> Date: 2026-06-29 · Status: Draft for review · Branch: `feat/plugin-sp8-security-hardening` (off `feat/plugin-followups` / `upcoming`)
> Follow-up to SP1–SP5. Addresses the security findings from the post-integration audit (one P2 authz gap + three P3 defense-in-depth) plus two spec-mandated test gaps.

## Why

Post-integration security audit found: (1) `POST /api/admin/restart` is the only privileged plugin/admin endpoint NOT wrapped in `RequireAdminOrBypass` — a non-admin authenticated user can trigger repeated server re-exec (DoS) in a real `auth_provider` deployment; (2) the lifecycle handler and discovery don't validate plugin `{id}` against `pluginIDRe` (the dispatcher/registry do — inconsistent path-safety invariant); (3) `buildPluginEnv` lets a plugin's own `desc.Env` name `DASHBOARD_*` secrets, so a *benign* plugin could accidentally inherit the dashboard's master/JWT secret. Plus two test gaps the SP specs called for: `secretbox` key generate+persist+reuse happy path, and the CLI `enable <unknown-id>` error.

## Scope

In: admin-gate `/api/admin/restart`; apply `pluginIDRe` in the lifecycle handler + discovery; blocklist `DASHBOARD_*` (and other dashboard secrets) in `buildPluginEnv`; add the two missing tests.

Out: real `permissions`-based sandboxing (SP9 removes the inert field; enforcement is a separate large feature); the offline `dashboard plugins` CLI auth-bypass (intended by design — it's the lockout hatch).

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | Wrap `AdminHandler.Mount` in `RequireAdminOrBypass(BypassAuth)` | Match its risk-peers (spawners, system-prompts, plugin-lifecycle). Restart is at least as privileged. Bypass-auth (loopback default) still passes, so single-user local is unaffected. |
| D2 | Validate `{id}` with the shared `pluginIDRe` in lifecycle handler + discovery | One uniform path-safety invariant, not router-dependent. Reject malformed ids → 400 (handler) / skip-with-warning (discovery), matching the dispatcher/registry. |
| D3 | `buildPluginEnv` **blocklists** dashboard secret env names | A plugin can still read the DB/key file as the same user (accept), but a benign plugin must never *inadvertently* inherit `DASHBOARD_SECRET_KEY`/`DASHBOARD_JWT_SECRET`/`DASHBOARD_AUTH_PLUGIN_SECRET`/`DASHBOARD_MCP_TOKEN`/`DASHBOARD_HOOKS_SECRET` via its allow-list. Blocklist wins over the allow-list. |

## Architecture

### Admin-gate restart (`server/internal/api/router.go`)
- The `deps.AdminHandler.Mount(r)` call currently sits in the protected group without an admin wrapper. Wrap it like the siblings:
  ```go
  r.Group(func(r chi.Router) {
      r.Use(authpkg.RequireAdminOrBypass(deps.Config.BypassAuth))
      deps.AdminHandler.Mount(r)
  })
  ```
  (Match the exact pattern/args used for `SystemPromptsHandler`/`spawnersHandler`.)

### ID validation (`server/internal/api/plugins/handler.go` + `pluginlifecycle/discovery.go`)
- Handler: in `transition`, `getSettings`, `putSettings`, after the `id == ""` check, reject `!pluginIDRe.MatchString(id)` → 400. `pluginIDRe` lives in `internal/plugin` (unexported). Export a validator: add `plugin.ValidID(id string) bool` (wrapping the existing regex) and call it from the handler + discovery (DRY, single regex SSOT in `internal/plugin`).
- Discovery (`discovery.go`): when scanning manifests, skip any whose `desc.ID` fails `plugin.ValidID` (log a warning), mirroring `registry.Load`.

### Env blocklist (`server/internal/plugin/registry.go` `buildPluginEnv`)
- Add a `dashboardSecretEnv` set (`DASHBOARD_SECRET_KEY`, `DASHBOARD_JWT_SECRET`, `DASHBOARD_AUTH_PLUGIN_SECRET`, `DASHBOARD_MCP_TOKEN`, `DASHBOARD_HOOKS_SECRET`). In the loop that copies allow-listed env, skip any key in the blocklist even if listed in `desc.Env`. (The base set PATH/HOME/etc is unaffected.)

### Tests (gap closure)
- `secretbox_test.go`: happy path — with a temp dir as the key location (override the path via env/param), `LoadOrGenerateMasterKey` with no existing file generates a key, persists it `0600`, and a second call returns the SAME key (reuse). 
- `cmd_plugins_test.go`: `enable <unknown-id>` → error + non-zero (mirror the existing `disable`-unknown test).

## Data flow / behavior
```
POST /api/admin/restart → [RequireAdminOrBypass] → AdminHandler.restart   # non-admin in auth-mode → 403
GET/POST /api/plugins/{id}/... → plugin.ValidID(id)? no → 400
discovery scan → plugin.ValidID(desc.ID)? no → skip + warn
spawn plugin → buildPluginEnv: forward allow-list MINUS dashboardSecretEnv
```

## Error handling
- Malformed id → 400 (handler) / skipped (discovery), never reaches `filepath.Join`.
- Blocklisted secret in `desc.Env` → silently not forwarded (optionally slog.Debug); plugin start proceeds.
- Non-admin restart in auth mode → 403 (RequireAdmin); bypass mode → allowed (unchanged).

## Testing
- Admin-gate: restart route returns 403 for a non-admin token; allowed under bypass (router/handler test if a harness exists, else assert the middleware is applied).
- ID validation: lifecycle handler 400 on `../x`/uppercase/empty; discovery skips a manifest with a bad id (doesn't persist it).
- Env blocklist: `buildPluginEnv` with `desc.Env=["DASHBOARD_SECRET_KEY","MY_KEY"]` + those set in os env → output contains `MY_KEY`, NOT `DASHBOARD_SECRET_KEY`.
- secretbox happy-path + CLI enable-unknown (the two gap tests).

## Risks / notes
- No ent change. Backend-only (+ tests).
- `plugin.ValidID` is a new tiny exported helper wrapping the existing `pluginIDRe` — keep the regex as the single SSOT in `internal/plugin`.
- `RequireAdminOrBypass` exact signature must match existing usages (grep before writing).
