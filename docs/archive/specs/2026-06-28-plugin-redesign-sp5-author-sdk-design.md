# Plugin Redesign SP5 — Plugin Author SDK + Guide — Design Spec

> Date: 2026-06-28 · Status: Draft for review · Branch: `feat/plugin-sp5` (off `feat/plugin-sp4` ← feat/plugin-sp3 ← #232 ← #231)
> Parent: `docs/superpowers/specs/2026-06-28-plugin-system-redesign-design.md` (SP5 row). Final slice of the redesign. Single PR.

## Why

SP1–SP4 built the plugin runtime (lifecycle, live dispatch, restart, UI slots, settings). Nothing yet helps a third party *author* a plugin: there is a prose `docs/plugin-guide.md` but no machine-readable manifest schema, no shared addon types, and the guide predates the SP2 path change + SP4 slot composition. SP5 ships the author-facing artifacts (JSON schema + TS addon types) and a consolidated, accurate guide.

## Scope

In: a `plugin-sdk/` folder with `plugin.schema.json` (manifest v2), `addon.d.ts` (UI-addon author types), and a `README.md` quickstart; `$schema` references wired into the 5 example manifests; a consolidated `docs/plugin-guide.md`; a drift-guard test (example manifests validate against the schema) + an addon-types usage test.

Out: a publishable npm package (rejected — local single-user tool); a Go author-SDK helper (rejected — example-driven, plugins are language-agnostic); any Vue-externalize build config (rejected — the addon contract is vanilla imperative `mount`, not Vue components, so there is no host-Vue singleton to share).

## Decisions (resolved in brainstorming)

| # | Decision | Rationale |
|---|---|---|
| D1 | **Drop "Vue-externalize"; document vanilla ESM** | Addons are `export default { slot, mount(el, ctx, parent) }` ESM modules mounted into an isolated DOM element. No host framework singleton; an author bundles whatever framework they want into the file. |
| D2 | Ship author artifacts in **`plugin-sdk/`** (repo folder, no npm publish) | Local single-user project; authors reference/copy the schema + `.d.ts`. |
| D3 | `addon.d.ts` is a **hand-maintained copy** of the slot contract; `src/utils/pluginSlot.ts` stays the SSOT | Cross-boundary parity by hand (same pattern as the client/server slug rule). A comment + a parity test guard drift. |
| D4 | Manifest validation test uses a **minimal structural validator** (no new dependency) | `ajv` is not a dependency; adding one for a docs deliverable is overkill. A focused check mirrors the schema's key rules (id pattern, capability enum, required fields). |
| D5 | **One slice / one PR** | SP5 is small + docs-heavy; a coherent guide is better written whole. |

## Architecture / Components

### `plugin-sdk/plugin.schema.json`
JSON Schema (draft-07) for `plugin.json` v2:
- `id`: string, pattern `^[a-z0-9][a-z0-9-]*$` (required).
- `name`, `version`: string.
- `capabilities`: array of enum `auth_provider | route_extension | ui_extension`.
- `addr`: string (loopback host:port); `command`: array of string; `env`: array of string.
- `slots`: array of `{ slot: string, priority?: integer, mode?: 'override' | 'extend' }`.
- `settings`: array of `{ key: string, type: 'string'|'url'|'int'|'bool'|'enum', label: string, secret?: boolean, enum?: string[] }`.
- `lifecycle`: object `{ install?, postInstall?, activate?, deactivate?, update?, uninstall? }` (string paths).
- `permissions`: array of string.
- `additionalProperties: true` at the top level (forward-compat, mirrors the server's "unknown fields ignored").

### `plugin-sdk/addon.d.ts`
Author-facing TS types, copied from `src/utils/pluginSlot.ts`: `SlotName`, `SlotContracts` (the 5 slots + contexts), `SlotAddon<S>`, `SlotParent`, `UnmountFn`, `RefinementInputContext`/`TaskSlotContext`/`AgentSlotContext`/`SettingsSlotContext`. Header comment: "Mirror of `src/utils/pluginSlot.ts` (the SSOT). Keep in parity by hand."

### `plugin-sdk/README.md`
Quickstart: (1) write `plugin.json` with `"$schema": "<path to plugin.schema.json>"`; (2) backend = any subprocess serving `GET /health` (+ optional lifecycle hook paths) — point at the example plugins; (3) UI addon = a vanilla ESM module `export default { slot, mount(el, ctx, parent) }`, served from the plugin's proxy namespace; reference `addon.d.ts` for typed contexts.

### `docs/plugin-guide.md` (consolidated)
Bring the existing guide current: manifest v2 (incl. `slots` priority/mode, `settings`, `lifecycle`); capabilities (auth_provider — needs restart, SP3; route_extension — live, SP2; ui_extension — live + refresh-to-unload, SP4); the proxy path `/api/plugins/{id}/proxy/*` (replacing all `/api/settings/plugins/{id}` references); enable/disable via the lifecycle endpoints + the settings UI; slot composition (sibling/override/extend with the `parent` handle); a first-plugin walkthrough. Remove the deprecated/legacy-route and Vue-externalize mentions.

### Example manifests
Add `"$schema": "../../plugin-sdk/plugin.schema.json"` to each `plugins/*/plugin.json` (relative path from the plugin dir to the repo's `plugin-sdk/`).

## Testing
- **Manifest drift guard** (`plugin-sdk/plugin-schema.test.ts` or under `src/` test scope): load each `plugins/*/plugin.json`, assert it satisfies the schema's key rules via a minimal structural validator (id matches the pattern; every capability is in the enum; settings/slots entries have required keys with valid `type`/`mode`). Fails if an example manifest drifts.
- **Addon types usage** (`plugin-sdk/addon.test-d.ts` or a `.test.ts` that imports the `.d.ts`): construct a `SlotAddon<'agent-modal-footer'>` and assert its `mount(el, ctx, parent)` shape compiles + `ctx` is the right context type — guards the `.d.ts` stays usable and in parity with `pluginSlot.ts`.
- Docs verified by review (no automated test).

## Error handling / edge cases
- Schema `additionalProperties: true` so a newer manifest field doesn't fail validation (forward-compat with the server's tolerant parser).
- The `$schema` relative path must resolve from each plugin dir; verify it points at the repo `plugin-sdk/plugin.schema.json` for all 5 examples.

## Risks / notes
- Frontend/docs-only; no Go/ent change. The test runs under vitest (TS).
- `addon.d.ts` parity is manual — the usage test + a clear header comment mitigate drift; a future change to `pluginSlot.ts` must update the `.d.ts` (note in the SSOT table of `layer2-project-core.md` if appropriate).
- Keep the schema's enums in sync with the server SSOT (`plugin/types.go` capability consts, `SettingField.type`, `SlotBinding.mode`) — by hand; document.
