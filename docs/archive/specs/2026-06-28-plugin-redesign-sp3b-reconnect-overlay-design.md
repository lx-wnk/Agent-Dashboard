# Plugin Redesign SP3b — Frontend Reconnect Overlay — Design Spec

> Date: 2026-06-28 · Status: Draft for review · Branch: `feat/plugin-sp3b-reconnect` (off `feat/plugin-sp2-live-dispatch` / #232)
> Parent: `docs/superpowers/specs/2026-06-28-plugin-system-redesign-design.md` (SP3 row). Pairs with SP3a (restart endpoint). Independent of SP3a's code (frontend only) but completes the UX.

## Why

SP3a makes the server restart on request (re-exec or supervised). During the few seconds the backend is down, the SPA's SSE stream and API calls fail. Without UX the app looks broken. SP3b adds a reconnect overlay that detects the outage, polls `/health`, and auto-recovers — plus a "restart required" hint when the user changes a boot-wired plugin (`auth_provider`/bind-port) that SP2's live dispatch cannot apply.

## Scope

In: a `useServerReconnect` composable (detect down → poll `/health` → reconnect/reload); a blocking "server restarting…" overlay component; a "restart required" indicator surfaced when a boot-wired plugin is toggled; a restart-trigger affordance (button) that calls `POST /api/admin/restart` and shows the overlay.

Out: the restart endpoint itself (SP3a); the CLI (SP3c). If SP3a is not yet merged, the restart button degrades gracefully (the endpoint 404s → show an error toast) — SP3b does not depend on SP3a being present to build/test.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | Poll the existing public `GET /api/system/health` | Already unauthenticated + cheap (`system.HealthHandler`); no new endpoint. |
| D2 | On reconnect, **full page reload** | Simplest correct recovery — re-runs auth, re-opens SSE, reloads plugin/UI state. A surgical re-subscribe is more work for no real gain on a local app. |
| D3 | Overlay is **blocking + non-dismissable** while down | The app is genuinely unusable during restart; a blocking overlay prevents confusing half-failed interactions. |
| D4 | "Restart required" is **derived from plugin capabilities**, not new server state | When a toggled plugin has `auth_provider` (or a bind-port capability), the UI knows it's boot-wired and shows the hint — no extra backend flag needed. |

## Architecture

### `useServerReconnect` composable (`src/composables/useServerReconnect.ts`)
- State: `isReconnecting: Ref<boolean>`.
- `triggerRestart()`: `POST /api/admin/restart` (same-origin, Origin header); on 202 set `isReconnecting=true` and start polling; on non-2xx surface an error (toast/throw).
- `beginReconnect()`: set `isReconnecting=true` and start polling (also callable when the SSE layer detects a drop).
- Polling: `GET /api/system/health` every `RECONNECT_POLL_MS` (~1500ms, a shared const in `src/utils/`), with a short fetch timeout; on first 200 after being down → `window.location.reload()`. Stop polling on unmount.
- Reuse existing SSE retry semantics (`src/utils/sse.ts` `SSE_RETRY_DELAY_MS`) for consistency; the composable is the single owner of the "is the server down" signal.

### Overlay component (`src/components/ServerReconnectOverlay.vue`)
- Full-screen blocking overlay shown when `isReconnecting`. Copy: "Server is restarting…", a spinner, and a subtle "reconnecting automatically" line. Mounted once near the app root (e.g. `App.vue`).

### Restart affordance + "restart required" hint
- A restart control (button) in the Plugins/Settings area calls `triggerRestart()`. After a boot-wired plugin (`auth_provider` or bind-port capability) is activated/deactivated, show a "Restart required to apply" badge next to that plugin + surface the restart button. Capability check uses the plugin list DTO already served by `/api/plugins` (`capabilities` field).

## Data flow
```
user toggles auth_provider plugin → /api/plugins/{id}/activate (SP1/SP2)
  → UI sees capability auth_provider → shows "Restart required" badge + restart button
user clicks restart → useServerReconnect.triggerRestart()
  → POST /api/admin/restart → 202 → isReconnecting=true → overlay shown
  → poll GET /api/system/health … (server down → fails) … → 200 → window.location.reload()
```

## Error handling
- Restart endpoint 404 (SP3a not deployed) or 409 (validation rejected) → do NOT show the blocking overlay; show an error toast with the server message (e.g. "restart would lock out auth").
- Poll never succeeding (server didn't come back) → keep the overlay but after N attempts show a "still waiting — check the server" message; never silently give up.
- Health endpoint returns non-200 transiently during boot → treated as "still down", keep polling.

## Testing
- Vitest: `useServerReconnect` — `triggerRestart` posts + flips state on 202; polling calls `/health` and reloads on 200 (mock `window.location.reload`); error responses keep overlay hidden + surface error; polling stops on unmount.
- Component: overlay renders only when `isReconnecting`; "restart required" badge shows only for boot-wired capabilities.
- No e2e required; covered by unit + component tests (consistent with the project's Vitest-first approach).

## Risks / notes
- Frontend-only; no backend or ent change. Builds/tests independently of SP3a.
- `window.location.reload()` is hard to assert — mock it; keep the reload call isolated in one place for testability.
- Keep the poll interval + timeout as shared consts (SSOT) under `src/utils/`.
