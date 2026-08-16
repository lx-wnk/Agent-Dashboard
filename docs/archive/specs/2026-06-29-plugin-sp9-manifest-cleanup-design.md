# Plugin SP9 — Manifest Field Cleanup — Design Spec

> Date: 2026-06-29 · Status: Draft for review · Branch: `feat/plugin-sp9-manifest-cleanup` (off `feat/plugin-followups` / `upcoming`)
> Follow-up to SP1–SP5. Removes declared-but-ignored manifest surface flagged by the post-integration architecture audit.

## Why

Two `plugin.json` manifest fields are parsed and shipped (in the v2 schema, the Go `Descriptor`, and tests) but have **zero consumers**, and one exported registry method is unused:
- `permissions` (`Descriptor.Permissions`, `plugin.schema.json`) — looks like a sandboxing/security control but is enforced nowhere. Leaving a security-shaped field inert is a trap (an author may rely on it). Real enforcement is a separate large feature; decision: **remove**.
- `slots[]` / `SlotBinding` (`Descriptor.Slots`) — the live slot/priority/mode for UI extensions comes solely from the plugin-served `ui-manifest.json` (SP4a D5, the SSOT). The `plugin.json` `slots[]` copy is read by no Go consumer and never reaches the frontend — a divergeable duplicate. Decision: **remove**.
- `plugin.Registry.AllWithCapability` — zero references (incl. tests). Dead code.

## Scope

In: remove `permissions` from `plugin.schema.json` + `Descriptor` + docs; remove `slots[]`/`SlotBinding` from `plugin.schema.json` + `Descriptor` + docs (note `ui-manifest.json` is the SSOT for slots); remove the unused `Registry.AllWithCapability`.

Out: real `permissions` enforcement (separate feature; the field can be re-added with enforcement later); the `pluginsctl`/`/api/settings/plugins` consolidation (parked — depends on SP6/SP7); the `url`/settings validation (SP6).

## Decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | **Remove** `permissions` (schema + `Descriptor.Permissions` + guide mention) | No enforcement; security-theatre risk. Manifest v2 ignores unknown fields (forward-compat), so an old manifest still loads; re-introduce with enforcement when built. |
| D2 | **Remove** `slots[]`/`SlotBinding` from `plugin.json` (schema + `Descriptor.Slots` + type) | `ui-manifest.json` is the single source for slot bindings; the `plugin.json` copy is unused + divergeable. Guide updated to state ui-manifest is authoritative. |
| D3 | **Remove** `Registry.AllWithCapability` | Dead exported code; `FindByCapability`/`Lookup`/`Infos` cover all live needs. |

## Architecture / changes

### `server/internal/plugin/types.go`
- Remove the `Slots []SlotBinding` and `Permissions []string` fields from `Descriptor`.
- Remove the `SlotBinding` struct (no other consumer). Keep `SettingField`, `LifecycleHooks`, capability consts.
- Forward-compat preserved: `json.Unmarshal` ignores unknown JSON fields, so a `plugin.json` that still has `slots`/`permissions` loads fine (fields just dropped).

### `plugin-sdk/plugin.schema.json`
- Remove the `slots` and `permissions` property definitions. `additionalProperties: true` stays, so manifests still validate if they carry the old fields (editor just won't autocomplete them).

### `server/internal/plugin/registry.go`
- Delete `AllWithCapability` (and any now-unused helper it alone used — verify none).

### Docs
- `docs/plugin-guide.md`: remove the `permissions` row/section; in the manifest reference note that slot bindings live in `ui-manifest.json` (not `plugin.json`), removing the `plugin.json slots[]` mention. `plugin-sdk/README.md`: same — drop `permissions`/`slots` from the field table, point slots at `ui-manifest.json`.
- `CHANGELOG.md`: a `### Changed`/`### Removed` note that the inert `permissions` and `plugin.json slots[]` manifest fields were removed (slot bindings are declared in `ui-manifest.json`).

## Error handling / compat
- Old manifests carrying `slots`/`permissions` still load (unknown-field-tolerant parse + `additionalProperties:true`) — no breakage, the fields are simply ignored. Example plugins (the 5 in `plugins/`) don't use either field — verify (`grep -l '"slots"\|"permissions"' plugins/*/plugin.json` → expect none).

## Testing
- `go build ./...` clean after the type removals (catches any stray reference to `SlotBinding`/`Slots`/`Permissions`/`AllWithCapability`).
- The SP5 manifest drift-guard test (`src/plugin-sdk.test.ts`) still passes (examples validate against the trimmed schema).
- A grep/assert that no Go code references the removed symbols.
- Manifest-parse test: a `plugin.json` containing legacy `slots`/`permissions` still unmarshals without error (forward-compat).

## Risks / notes
- No ent change; backend + schema + docs.
- Verify no test references `SlotBinding`/`Descriptor.Slots`/`Descriptor.Permissions` before removing — update/remove those test assertions.
- Pure subtraction + docs; lowest-risk of the four follow-up specs → good first/standalone slice.
