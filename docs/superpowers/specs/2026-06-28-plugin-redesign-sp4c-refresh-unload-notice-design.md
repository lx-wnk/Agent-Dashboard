# Plugin Redesign SP4c — Refresh-to-Unload Notice — Design Spec

> Date: 2026-06-28 · Status: Draft for review · Branch: `feat/plugin-sp4c-refresh-notice` (off `feat/plugin-sp4`)
> Parent: `docs/superpowers/specs/2026-06-28-plugin-system-redesign-design.md` (SP4 row). Smallest SP4 slice. Siblings: SP4a (slot chain), SP4b (settings UI).

## Why

Disabling a `ui_extension` plugin hides its slot UI (`v-if`/teardown), but the browser's ES-module registry is permanent — a dynamically imported plugin module stays in memory until a full page reload. Research (R3, Grafana/Backstage) shows the honest UX is a one-line "refresh to fully unload" notice rather than pretending the code is gone. SP4c adds that notice.

## Scope

In: when a `ui_extension` plugin is deactivated/disabled from the UI, show a non-blocking "refresh the page to fully unload this plugin" notice with a reload affordance.

Out: actually unloading the module (impossible without reload — that is the whole point); slot rendering (SP4a); settings/toggle mechanics (SP4b — SP4c only reacts to a `ui_extension` deactivation event the panel already emits).

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | Show the notice **only** when the deactivated plugin declares `ui_extension` | Other capabilities (route_extension) unload cleanly server-side; only browser-loaded ES modules persist. |
| D2 | Notice is **non-blocking** (inline banner/toast) with a "Reload now" button | The app stays usable; the user reloads when convenient. Distinct from SP3b's blocking restart overlay (that's a server restart; this is a client-only refresh). |
| D3 | Reuse the existing `PluginSettings.vue` notice mechanism (`showNotice`) | A `showNotice` helper already exists there; add a `ui_extension`-specific message + a reload button, no new infra. |

## Architecture

- In `PluginSettings.vue` (or wherever the deactivate action resolves), after a successful deactivate of a plugin whose `capabilities` include `ui_extension`, call `showNotice('warning', 'Plugin UI disabled — reload the page to fully unload its code')` and render a "Reload now" button (`window.location.reload()`) in that notice.
- If the notice component does not support an action button, add a minimal optional action (`{ label, onClick }`) to the existing notice shape — keep it small and local.

### Files
| File | Change |
|---|---|
| `src/components/PluginSettings.vue` | ui_extension deactivation → refresh notice + reload button |
| `src/components/PluginSettings.test.ts` (or the panel's test) | notice appears only for ui_extension deactivation; reload button calls reload |

## Data flow
```
user deactivates plugin → (SP4b setActive deactivate) → success
  if plugin.capabilities includes 'ui_extension':
     showNotice('warning', 'reload to fully unload') + [Reload now] → window.location.reload()
```

## Error handling
- Reload is user-initiated; no failure path beyond the browser reload.
- Notice auto-dismiss (existing 5s timer) — but keep the reload button visible long enough; consider not auto-dismissing the ui_extension notice (it carries an action). Decision: **do not auto-dismiss** the refresh notice; let the user dismiss or reload.

## Testing
- Deactivating a `ui_extension` plugin → notice with reload button shown; deactivating a non-ui_extension plugin → no such notice.
- Reload button calls `window.location.reload()` (mocked).

## Risks / notes
- Frontend-only; trivial; depends on SP4b's `setActive`/deactivate path (build order: SP4b before SP4c, or SP4c reads the capability from the existing list independently). If sequenced independently, SP4c can hook the existing deactivate handler regardless of SP4b's rework — coordinate at merge into `feat/plugin-sp4`.
- Distinct from SP3b's server-restart overlay: this is a client-only refresh, never triggers `/api/admin/restart`.
