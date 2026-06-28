# Plugin Redesign SP4b — Per-Plugin Settings UI — Design Spec

> Date: 2026-06-28 · Status: Draft for review · Branch: `feat/plugin-sp4b-settings-ui` (off `feat/plugin-sp4`)
> Parent: `docs/superpowers/specs/2026-06-28-plugin-system-redesign-design.md` (SP4 row). Sibling slices: SP4a (slot chain), SP4c (refresh notice).

## Why

SP1 added per-plugin settings (`GET/PUT /api/plugins/{id}/settings`, secrets encrypted + masked) but no UI consumes them. SP4b renders a schema-driven settings form in the plugin panel. While reworking that panel it also fixes a SP2-introduced regression: `usePluginSettings.toggle()` still calls the **removed** `PATCH /api/settings/plugins-enabled/{id}` — enable/disable from the UI is currently broken; it must move to the live lifecycle endpoints.

## Scope

In: a schema-driven per-plugin settings form (string/url/int/bool/enum, secret masking, sentinel-unchanged on save) driven by `GET/PUT /api/plugins/{id}/settings`; migrate the plugin enable/disable toggle from the removed PATCH to `POST /api/plugins/{id}/activate|deactivate`; consume the lifecycle list (`/api/plugins`, which carries `hasSettings` + `state`).

Out: slot rendering (SP4a); refresh-to-unload notice (SP4c — though the toggle migration here surfaces the auth_provider "restart required" case already wired in SP3b); secret crypto (SP1, done).

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | The settings form consumes `GET /api/plugins/{id}/settings` (schema + masked values) and saves via `PUT` | The SP1 contract; schema drives the fields, so no per-plugin UI code. |
| D2 | A secret field shows a masked sentinel; **PUT only sends a changed secret** — unchanged leaves the sentinel, which the server treats as "keep" | Matches SP1's masked-sentinel rule; never round-trips plaintext. |
| D3 | **Migrate the toggle to `activate`/`deactivate`** (live for route/ui; the SP3b "restart required" badge already covers auth_provider) | The interim PATCH was removed in SP2; the UI must use the real lifecycle verbs. Fixes the broken toggle. |
| D4 | Drive the panel from the **lifecycle list `/api/plugins`** (`hasSettings`, `state`, `capabilities`) | `hasSettings` gates the form; `state` (discovered/inactive/active) drives the toggle. The old `/api/settings/plugins` list lacks `hasSettings`. |

## Architecture

### Composable (`src/composables/usePluginSettings.ts`, reworked)
- List: `GET /api/plugins` → `{id,name,version,state,updateAvailable,capabilities,hasSettings}` (the lifecycle DTO). Replaces the current `/api/settings/plugins` fetch.
- `setActive(id, active)`: `POST /api/plugins/{id}/activate` or `/deactivate`; on success update local `state`. Replaces the broken `toggle()` PATCH. Returns whether a restart is required (true when the plugin declares `auth_provider`) so the caller shows the SP3b badge.
- `getSettings(id)`: `GET /api/plugins/{id}/settings` → `{schema, values}`.
- `putSettings(id, values)`: `PUT` with only changed fields (secret sentinel omitted-or-kept per D2).
- A separate small composable already named `usePluginSettings.ts` exists for the list — extend it (or split list vs per-plugin settings into `usePluginSettings` + `usePluginSettingsForm`) keeping files focused.

### Component (`src/components/PluginSettings.vue` + a `PluginSettingsForm.vue`)
- Each plugin row gains (when `hasSettings`) an expandable settings section rendering `PluginSettingsForm` for that id.
- `PluginSettingsForm.vue`: fetches schema+values, renders one control per field type — `string`/`url` → text input, `int` → number, `bool` → checkbox, `enum` → select. Secret fields → password-style input pre-filled with the masked sentinel; editing replaces it. Save button → `putSettings` (only fields the user changed; an untouched secret keeps the sentinel).
- The enable/disable control calls `setActive`; the SP3b "restart required" badge/button stays for auth_provider.

### Files
| File | Change |
|---|---|
| `src/composables/usePluginSettings.ts` | list from `/api/plugins`; `setActive` (activate/deactivate); add `getSettings`/`putSettings` (or a sibling composable) | 
| `src/components/PluginSettings.vue` | use the lifecycle list + `setActive`; expandable settings section per plugin with `hasSettings` |
| `src/components/PluginSettingsForm.vue` (new) | schema-driven form (field types, secret mask, sentinel-unchanged save) |
| `*.test.ts` for the composable + form | list/setActive; field rendering per type; secret masking; PUT payload omits unchanged secret |

## Data flow
```
PluginSettings → usePluginSettings: GET /api/plugins (hasSettings, state, caps)
  toggle → setActive(id) → POST /api/plugins/{id}/activate|deactivate → update state
  expand settings → PluginSettingsForm → GET /api/plugins/{id}/settings (schema+masked values)
  save → PUT /api/plugins/{id}/settings { values: changed-only } (secret sentinel = keep)
```

## Error handling
- List/settings fetch failure → existing error ref + message (errorMessage util).
- `setActive` failure (e.g. activate hook error 4xx/5xx) → surface the server error; state unchanged.
- PUT validation error (unknown key / bad type → 400) → show the field/server error, keep the form open.
- Secret sentinel: never display or send plaintext for an unchanged secret.

## Testing
- Composable: list maps the lifecycle DTO; `setActive` posts the right verb + updates state; `getSettings`/`putSettings` shapes.
- Form: renders correct control per `type`; secret shows sentinel not plaintext; saving an untouched secret omits it from the PUT (server keeps it); changed fields sent.
- Regression: the toggle no longer calls `/api/settings/plugins-enabled/` (assert the new endpoint).

## Risks / notes
- Frontend-only; backend endpoints already exist (SP1). No ent change.
- Two list endpoints exist (`/api/settings/plugins` read-only + `/api/plugins` lifecycle). SP4b moves the settings panel to `/api/plugins`; the slot loader (SP4a) keeps using `/api/settings/plugins` (it only needs id+capabilities). Acceptable; a later cleanup could unify.
- Coordinate with SP4c (refresh notice) — both touch `PluginSettings.vue`; they merge into `feat/plugin-sp4` so resolve any overlap there.
