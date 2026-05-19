# Open TODOs Design Spec

**Date:** 2026-05-19  
**Status:** Approved

---

## Scope

Three actionable items from `docs/local/TODOS.md` (all others resolved or no-action):

| Item | Priority | Effort |
|------|----------|--------|
| UX-46: API key token masking | P4 | S |
| Plugin guide update | overall | M |
| ARCH-05: tygo codegen | P4 | L |
| gzip bug | — | already fixed |

---

## UX-46 — API Key Token Masking

**Problem:** `ApiKeySettings.vue` reveal dialog displays the full API token unmasked immediately after creation. Screen sharing / shoulder-surfing leaks secrets.

**Solution:**
- Extract `maskToken(token: string): string` pure utility function into `src/utils/format.ts`
- Shows first 8 chars + `•` padding + last 4 chars
- Add `tokenVisible` ref (default `false`) to reveal dialog
- Eye-icon toggle button in the dialog
- Copy button always copies the full token regardless of visibility
- `tokenVisible` resets to `false` on dialog close

**Files:**
- `src/utils/format.ts` — add `maskToken()`
- `src/utils/format.test.ts` — unit tests for `maskToken()`
- `src/components/ApiKeySettings.vue` — wire masking into reveal dialog

---

## Plugin Guide Update

**Problem:** `docs/plugin-guide.md` documents only the legacy capability-based flow (`/capabilities/auth/*`). The github-oauth plugin's new standalone OAuth flow (`/login` → `/callback` → `POST /api/auth/session`) is the primary path and is undocumented.

**Solution:** Update `docs/plugin-guide.md` to:
- Document standalone OAuth flow as primary
- Mark legacy capability routes as deprecated/backwards-compat
- Correct `PLUGIN_DIR` references to match koanf config key `plugin_dir`
- Document `DASHBOARD_AUTH_PLUGIN_SECRET` env var
- Update github-oauth endpoint table to include `/login` and `/callback`

**Files:**
- `docs/plugin-guide.md` — rewrite auth_provider section

---

## ARCH-05 — tygo Codegen (Partial)

**Problem:** `sdk/types.go` and `src/types.ts` define `TokenUsage`, `SessionMeta`, `SubAgent`, `TaskInfo`, `AgentStatus` independently. Manual sync is error-prone.

**Scope decision:** Migrate only the types that map cleanly without nullability gymnastics (`TokenUsage`, `SessionMeta`, `SubAgent`, `TaskInfo`, `AgentStatus`). `Agent` stays manually defined — it has TS-only fields (`lastBtw`, `machine`, `pipelineTaskTitle`) and string-to-union refinements that tygo can't model without Go type changes.

**Solution:**
1. `tygo.yaml` at repo root — targets `sdk/types.go`, outputs `src/sdk.generated.ts`
2. `sdk/types.go` — add `//go:generate` comment
3. `Taskfile.yml generate` — prepend tygo step
4. `src/sdk.generated.ts` — committed generated output (never edited manually; header comment warns)
5. `src/types.ts` — remove `TokenUsage`, `SessionMeta`, `SubAgent`, `TaskInfo`; import from generated; `AgentStatus` imported but `AGENT_STATUSES` runtime array stays manual (tygo can't gen runtime arrays)

**Invariant:** `src/sdk.generated.ts` is always up to date with `sdk/types.go`. Checked by running `task generate` and verifying no diff before PR.

**Files:**
- `tygo.yaml` — create
- `sdk/types.go` — add go:generate comment
- `Taskfile.yml` — update generate task
- `src/sdk.generated.ts` — created by codegen
- `src/types.ts` — import from generated, remove duplicates

---

## Out of Scope

- Full `Agent` struct migration to tygo (requires Go type changes for string-enum fields)
- CI drift-check for generated file (follow-up)
- `ARCH-05` part 2: change `Entrypoint`/`ErrorState` to typed Go enums (follow-up)
