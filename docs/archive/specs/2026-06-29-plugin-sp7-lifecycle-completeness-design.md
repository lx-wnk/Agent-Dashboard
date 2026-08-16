# Plugin SP7 — Lifecycle Completeness (Health Visibility + Update Verb) — Design Spec

> Date: 2026-06-29 · Status: Draft for review · Branch: `feat/plugin-sp7-lifecycle-completeness` (off `feat/plugin-followups` / `upcoming`)
> Follow-up to SP1–SP5. Closes two lifecycle gaps from the post-integration audit.

## Why

1. **Health is invisible.** `/api/plugins` derives `state` purely from DB `active`/`installed_at`; the registry's runtime health (`Entry.healthy`) is never surfaced. `OnUnhealthy` already persists `active=false` on *exhausted* restarts, but during the crash→restart-backoff window a plugin shows `state:"active"` while not serving, and the DTO has no field to express "active intent, not currently running/healthy".
2. **`update` is dead.** `Engine.Update` + the `update` lifecycle hook exist, and discovery computes `updateAvailable` (surfaced in the DTO), but there is no API verb to trigger an update — the UI can show "update available" with no action.

## Scope

In: a `healthy` (runtime) field on `PluginView` sourced from the registry at list time; a read seam from the lifecycle controller to the registry; the `update` lifecycle verb (`POST /api/plugins/{id}/update`) wired through the controller to `Engine.Update`; a minimal frontend affordance (show health + an "Update" button when `updateAvailable`).

Out: a persistent health DB column (rejected — runtime truth lives in the registry); auto-update/scheduling; the pluginsctl/`/api/settings/plugins` consolidation (parked — depends on this + SP6).

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | `PluginView` gains **`healthy bool`** (runtime), filled from the registry at list time; DB `state` stays intent | Clean intent-vs-runtime split, no ent migration. `healthy` reflects `registry.Lookup(id).healthy` (false if not running). |
| D2 | Controller gets a **`HealthProbe` seam** (`func(id string) (running bool, healthy bool)`) injected via DI over the registry | Keeps `pluginlifecyclectl` decoupled from `*plugin.Registry`; nil-safe (no DB / no registry → `healthy=false`). |
| D3 | Add **`update`** to the closed `lifecycleActions` set + a controller case → `Engine.Update` | The route `POST /api/plugins/{id}/{action}` already matches; only the action allow-list + controller dispatch are missing. |
| D4 | Frontend: show a **health dot** (running/healthy vs not) + an **Update button** when `updateAvailable` | Makes both fixes user-visible; minimal addition to the existing `PluginSettings.vue` row. |

## Architecture

### Health field (`server/internal/api/plugins/handler.go` + `pluginlifecyclectl/controller.go`)
- `PluginView` += `Healthy bool \`json:"healthy"\``.
- `controller.List` fills `Healthy` per plugin via the injected `HealthProbe`: `running, healthy := probe(id); view.Healthy = running && healthy`. (A not-running plugin → `healthy=false`.)
- DI (`di.go`): build the probe from `pluginRegistry` — `func(id string)(bool,bool){ e, ok := reg.Lookup(id); return ok, ok && e.Healthy() }`. Inject into `pluginlifecyclectl.New`. Nil probe → `Healthy=false` for all.

### Update verb (`pluginlifecyclectl/controller.go` + `handler.go`)
- `handler.go`: add `"update": true` to `lifecycleActions`.
- `controller.Transition`: add a `case "update":` that loads the descriptor and calls `engine.Update(ctx, desc)`. `Engine.Update` already: runs the `update` hook via `WithTransient`, bumps the stored version. After update, `updateAvailable` recomputes to false on the next discovery/list (version now matches manifest hash) — verify the manifest-hash/version is refreshed (Update should also refresh `manifest_hash` so `updateAvailable` clears; if `Engine.Update` only bumps version, also update the stored hash — extend if needed).
- Returns the new `PluginView`.

### Frontend (`src/components/PluginSettings.vue` + `usePluginSettings.ts`)
- `usePluginSettings`: `PluginView` type += `healthy: boolean`; add an `update(id)` method → `POST /api/plugins/{id}/update`.
- Row: the existing state dot uses `p.state === 'active'`; add a secondary indicator or title when `p.state === 'active' && !p.healthy` → "active, not running". When `p.updateAvailable`, render an "Update" button calling `update(id)` (with the same error handling as `setActive`).

## Data flow
```
GET /api/plugins → controller.List → per plugin: probe(id) → PluginView.healthy
POST /api/plugins/{id}/update → controller.Transition("update") → engine.Update
  → WithTransient(update hook) → SetVersion + refresh manifest_hash → updateAvailable clears
UI: state dot + "active not running" hint when active&&!healthy; "Update" button when updateAvailable
```

## Error handling
- Probe on a plugin absent from the registry → `running=false` → `healthy=false` (correct for inactive/crashed).
- `update` on a non-installed plugin → engine returns `ErrIllegalTransition` (already mapped to 409).
- `update` hook failure → transient stop + surfaced error (engine behavior), `updateAvailable` unchanged.

## Testing
- `controller.List` sets `Healthy` from a fake probe (running+healthy → true; not running → false).
- `update` action: accepted verb; controller dispatches to engine.Update; unknown→400, illegal→409.
- Update clears `updateAvailable` (version/hash refreshed) — engine/discovery test.
- Frontend: `update()` posts the right endpoint; health hint renders when active&&!healthy; Update button only when updateAvailable.

## Risks / notes
- No ent change.
- If `Engine.Update` doesn't currently refresh `manifest_hash`, extend it so `updateAvailable` clears post-update (otherwise the button stays). Verify against the discovery hash logic.
- `pluginlifecyclectl.New` + `apiplugins` controller interface gain the probe + the `Transition("update")` path → update fakes in tests.
