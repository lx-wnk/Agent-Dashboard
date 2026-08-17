# Plugin Redesign SP5 — Plugin Author SDK + Guide Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship author-facing plugin artifacts — `plugin-sdk/` with a `plugin.json` JSON schema, TS addon types, and a README — plus a consolidated `docs/plugin-guide.md`, with a drift-guard test validating the example manifests.

**Architecture:** A repo `plugin-sdk/` folder holds `plugin.schema.json` (draft-07), `addon.d.ts` (hand-mirrored from `src/utils/pluginSlot.ts`), and `README.md`. The 5 example manifests gain a `$schema` ref. A vitest test under `src/` (vitest only globs `src/**/*.test.ts`) reads the schema + example manifests via fs and structurally validates them, and imports `addon.d.ts` as a type to guard parity.

**Tech Stack:** JSON Schema (draft-07), TypeScript, Vitest, Markdown.

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `plugin-sdk/plugin.schema.json` | manifest v2 JSON schema (validation + `$schema` autocomplete) | Create |
| `plugin-sdk/addon.d.ts` | UI-addon author types (mirror of `src/utils/pluginSlot.ts`) | Create |
| `plugin-sdk/README.md` | author quickstart | Create |
| `src/plugin-sdk.test.ts` | drift guard: example manifests vs schema rules + addon-types parity import | Create |
| `plugins/*/plugin.json` (5) | add `"$schema"` ref | Modify |
| `docs/plugin-guide.md` | consolidate to current runtime (SP1–SP4) | Modify |

**Commands:** `pnpm test src/plugin-sdk.test.ts`, `pnpm typecheck`, `pnpm lint` (0). Worktree `pnpm i`. Commits `--no-gpg-sign`, English, no phase labels, trailers:
```
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6
```

The schema mirrors the server SSOT (`server/internal/plugin/types.go`): capability enum `auth_provider|route_extension|ui_extension`; `SettingField.type` ∈ `string|url|int|bool|enum`; `SlotBinding.mode` ∈ `override|extend`. Example manifests today are minimal (e.g. `voice-webspeech`: `{id,version,capabilities,addr,command,env}`), so only `id` is `required`.

---

### Task 1: Manifest schema + `$schema` refs + drift guard

**Files:** Create `plugin-sdk/plugin.schema.json`, `src/plugin-sdk.test.ts`; Modify the 5 `plugins/*/plugin.json`.

- [ ] **Step 1: Write the failing test**

Create `src/plugin-sdk.test.ts`:

```ts
import { readdirSync, readFileSync, existsSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

const ROOT = process.cwd()
const SCHEMA_PATH = join(ROOT, 'plugin-sdk', 'plugin.schema.json')
const PLUGINS_DIR = join(ROOT, 'plugins')

const ID_RE = /^[a-z0-9][a-z0-9-]*$/
const CAPS = new Set(['auth_provider', 'route_extension', 'ui_extension'])
const SETTING_TYPES = new Set(['string', 'url', 'int', 'bool', 'enum'])
const SLOT_MODES = new Set(['override', 'extend'])

// Minimal structural validator mirroring plugin.schema.json's key rules.
// (No ajv dependency — see the SP5 spec, decision D4.)
function validateManifest(m: any): string[] {
  const errs: string[] = []
  if (typeof m.id !== 'string' || !ID_RE.test(m.id))
    errs.push(`id "${m.id}" must match ${ID_RE}`)
  if (m.capabilities !== undefined) {
    if (!Array.isArray(m.capabilities))
      errs.push('capabilities must be an array')
    else for (const c of m.capabilities) if (!CAPS.has(c)) errs.push(`unknown capability "${c}"`)
  }
  for (const s of m.settings ?? []) {
    if (typeof s.key !== 'string') errs.push('setting.key required')
    if (!SETTING_TYPES.has(s.type)) errs.push(`setting "${s.key}" bad type "${s.type}"`)
  }
  for (const sl of m.slots ?? []) {
    if (typeof sl.slot !== 'string') errs.push('slot.slot required')
    if (sl.mode !== undefined && !SLOT_MODES.has(sl.mode)) errs.push(`slot "${sl.slot}" bad mode "${sl.mode}"`)
  }
  return errs
}

describe('plugin-sdk', () => {
  it('ships a valid JSON schema for plugin.json', () => {
    expect(existsSync(SCHEMA_PATH)).toBe(true)
    const schema = JSON.parse(readFileSync(SCHEMA_PATH, 'utf8'))
    expect(schema.$schema).toContain('json-schema.org')
    expect(schema.required).toContain('id')
  })

  it('every example manifest satisfies the schema rules', () => {
    const dirs = readdirSync(PLUGINS_DIR, { withFileTypes: true }).filter(d => d.isDirectory())
    expect(dirs.length).toBeGreaterThan(0)
    for (const d of dirs) {
      const p = join(PLUGINS_DIR, d.name, 'plugin.json')
      if (!existsSync(p))
        continue
      const m = JSON.parse(readFileSync(p, 'utf8'))
      expect(validateManifest(m), `${d.name}/plugin.json`).toEqual([])
      expect(m.$schema, `${d.name} should reference the schema`).toBeTruthy()
    }
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test src/plugin-sdk.test.ts`
Expected: FAIL — schema file missing; manifests lack `$schema`.

- [ ] **Step 3: Create the schema**

Create `plugin-sdk/plugin.schema.json`:

```json
{
  "$schema": "https://json-schema.org/draft-07/schema",
  "$id": "https://github.com/lx-wnk/agent-dashboard/plugin-sdk/plugin.schema.json",
  "title": "agent-dashboard plugin manifest (plugin.json v2)",
  "type": "object",
  "required": ["id"],
  "additionalProperties": true,
  "properties": {
    "$schema": { "type": "string" },
    "id": { "type": "string", "pattern": "^[a-z0-9][a-z0-9-]*$" },
    "name": { "type": "string" },
    "version": { "type": "string" },
    "capabilities": {
      "type": "array",
      "items": { "enum": ["auth_provider", "route_extension", "ui_extension"] }
    },
    "addr": { "type": "string", "description": "loopback host:port the plugin listens on" },
    "command": { "type": "array", "items": { "type": "string" } },
    "env": { "type": "array", "items": { "type": "string" } },
    "slots": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["slot"],
        "properties": {
          "slot": { "type": "string" },
          "priority": { "type": "integer" },
          "mode": { "enum": ["override", "extend"] }
        }
      }
    },
    "settings": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["key", "type", "label"],
        "properties": {
          "key": { "type": "string" },
          "type": { "enum": ["string", "url", "int", "bool", "enum"] },
          "label": { "type": "string" },
          "secret": { "type": "boolean" },
          "enum": { "type": "array", "items": { "type": "string" } }
        }
      }
    },
    "lifecycle": {
      "type": "object",
      "properties": {
        "install": { "type": "string" },
        "postInstall": { "type": "string" },
        "activate": { "type": "string" },
        "deactivate": { "type": "string" },
        "update": { "type": "string" },
        "uninstall": { "type": "string" }
      }
    },
    "permissions": { "type": "array", "items": { "type": "string" } }
  }
}
```

- [ ] **Step 4: Add `$schema` to each example manifest**

For each of `plugins/github-oauth`, `office365-oauth`, `voice-whisper`, `voice-webspeech`, `anthropic-spawner`: add `"$schema": "../../plugin-sdk/plugin.schema.json"` as the first key of `plugin.json` (relative path from the plugin dir to the repo `plugin-sdk/`). Example for `voice-webspeech`:

```json
{
  "$schema": "../../plugin-sdk/plugin.schema.json",
  "id": "voice-webspeech",
  "version": "0.1.0",
  "capabilities": ["route_extension"],
  "addr": "127.0.0.1:19011",
  "command": ["./voice-webspeech"],
  "env": []
}
```

> Verify each plugin dir's actual depth — all 5 live at `plugins/<name>/plugin.json`, so `../../plugin-sdk/...` is correct for every one. Do not reformat the rest of each file.

- [ ] **Step 5: Run test to verify it passes**

Run: `pnpm test src/plugin-sdk.test.ts`
Expected: PASS (both tests).

- [ ] **Step 6: Commit**

```bash
git add plugin-sdk/plugin.schema.json src/plugin-sdk.test.ts plugins/*/plugin.json
git commit --no-gpg-sign -m "feat: add plugin.json schema and validate example manifests

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 2: Addon author types + parity guard

**Files:** Create `plugin-sdk/addon.d.ts`; Modify `src/plugin-sdk.test.ts`

- [ ] **Step 1: Add the failing parity test**

Append to `src/plugin-sdk.test.ts` (top: add a type-only import; body: a new test). The type import makes `pnpm typecheck`/vitest typecheck resolve `addon.d.ts` and fail if it drifts from being usable:

```ts
import type { SlotAddon } from '../plugin-sdk/addon'

it('addon types are usable for authoring an addon', () => {
  const sample: SlotAddon<'agent-modal-footer'> = {
    slot: 'agent-modal-footer',
    priority: 10,
    mode: 'extend',
    mount: (el, ctx, _parent) => {
      // ctx is the agent context; touching it proves the typing resolves.
      void ctx.agent
      void el
      return () => {}
    },
  }
  expect(typeof sample.mount).toBe('function')
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm test src/plugin-sdk.test.ts`
Expected: FAIL — `../plugin-sdk/addon` cannot be resolved (file missing) → test/typecheck error.

- [ ] **Step 3: Create `addon.d.ts`**

Create `plugin-sdk/addon.d.ts` mirroring `src/utils/pluginSlot.ts` (read it first; copy the slot contracts + addon shape exactly). Content:

```ts
// agent-dashboard plugin UI-addon types.
// MIRROR of src/utils/pluginSlot.ts (the source of truth). Keep in parity by hand:
// when the host's slot contract changes, update this file too.

export interface RefinementInputContext {
  insertText: (text: string) => void
  setBusy: (busy: boolean) => void
}
export interface TaskSlotContext { task: unknown }
export interface AgentSlotContext { agent: unknown }
export type SettingsSlotContext = Record<string, never>

export interface SlotContracts {
  'refinement-input-addon': RefinementInputContext
  'task-modal-footer': TaskSlotContext
  'agent-modal-footer': AgentSlotContext
  'kanban-card-badge': TaskSlotContext
  'settings-panel': SettingsSlotContext
}

export type SlotName = keyof SlotContracts
export type UnmountFn = () => void

export interface SlotParent {
  mount: (el: HTMLElement) => UnmountFn
}

export interface SlotAddon<S extends SlotName = SlotName> {
  slot?: S
  /** Higher renders outer/first. Default 0. */
  priority?: number
  /** 'override' = own the slot exclusively; 'extend' = wrap the parent chain; omit = sibling. */
  mode?: 'override' | 'extend'
  mount: (el: HTMLElement, ctx: SlotContracts[S], parent?: SlotParent | null) => UnmountFn
}
```

> `TaskSlotContext.task`/`AgentSlotContext.agent` are typed `unknown` in the SDK (authors don't have the host's `PipelineTask`/`Agent` types). The host's `pluginSlot.ts` keeps the precise types — this is the deliberate author-facing simplification, noted in the header.

- [ ] **Step 4: Run to verify it passes**

Run: `pnpm test src/plugin-sdk.test.ts && pnpm typecheck`
Expected: PASS (typecheck resolves `addon.d.ts`; the sample addon compiles).

- [ ] **Step 5: Commit**

```bash
git add plugin-sdk/addon.d.ts src/plugin-sdk.test.ts
git commit --no-gpg-sign -m "feat: add plugin UI-addon author types

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 3: SDK README

**Files:** Create `plugin-sdk/README.md`

- [ ] **Step 1: Write the README**

Create `plugin-sdk/README.md` with: (1) what a plugin is (a subprocess + `plugin.json` + optional UI addon); (2) `plugin.json` quickstart with the `"$schema": "../../plugin-sdk/plugin.schema.json"` line for editor autocomplete + the field summary (link the schema); (3) backend contract — serve `GET /health` (200 when ready) and optional `lifecycle.*` hook paths (POST, 2xx = ok); point at `plugins/github-oauth` + `plugins/voice-whisper` as references; (4) UI addon — a vanilla ESM module:
```js
// addon.js — vanilla ESM, served from the plugin's proxy path
export default {
  slot: 'agent-modal-footer',
  priority: 0,
  // mode: 'override' | 'extend' (omit = sibling)
  mount(el, ctx, parent) {
    // build DOM into el; ctx is the slot context; parent (extend mode) can be mounted
    return () => { /* teardown */ }
  },
}
```
Note: addons are framework-agnostic — bring/bundle your own framework into the module; reference `addon.d.ts` for typed `ctx`. (5) capabilities + their liveness: `route_extension`/`ui_extension` apply live; `auth_provider` needs a server restart (the UI surfaces a restart button). Keep it concise (~1 screen).

- [ ] **Step 2: Verify + commit**

Run: `pnpm lint` (antfu lints markdown — fix any issues).
```bash
git add plugin-sdk/README.md
git commit --no-gpg-sign -m "docs: add plugin SDK quickstart README

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 4: Consolidate the plugin guide

**Files:** Modify `docs/plugin-guide.md`

- [ ] **Step 1: Read + update the guide**

Read `docs/plugin-guide.md` in full, then bring it current:
- **Manifest** section: document the full v2 (`slots[]{slot,priority,mode}`, `settings[]{key,type,label,secret,enum}`, `lifecycle{...}`, `permissions`) and reference `plugin-sdk/plugin.schema.json`.
- **Proxy path**: replace every `/api/settings/plugins/{id}` with `/api/plugins/{id}/proxy/*` (the SP2 contract). Grep the file for the old path to catch all.
- **Capabilities**: `auth_provider` — boot-wired, needs restart (SP3, restart button in UI); `route_extension` — live (SP2); `ui_extension` — live, with a refresh-to-unload prompt on disable (SP4). Remove the "(future)" marker on `route_extension` and any "Legacy capability routes (deprecated)" section that no longer applies.
- **UI extension / slots**: document the slot manifest entries (`priority`/`mode`), the sibling/override/extend composition + the `parent` handle, and that addons are vanilla ESM (remove any Vue-externalize mention).
- **Settings**: document the per-plugin settings UI + the encrypted-secret masking.
- **Enable/disable**: via the lifecycle endpoints + the offline `dashboard plugins disable` hatch (SP3c).
- Add a brief **"Build your first plugin"** walkthrough linking `plugin-sdk/README.md`.

- [ ] **Step 2: Verify no stale references**

Run: `grep -n '/api/settings/plugins/' docs/plugin-guide.md` → should return nothing (all moved to the proxy path). Run `pnpm lint`.

- [ ] **Step 3: Commit**

```bash
git add docs/plugin-guide.md
git commit --no-gpg-sign -m "docs: consolidate plugin guide for the current plugin runtime

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 5: Full verify + CHANGELOG

**Files:** Modify `CHANGELOG.md`

- [ ] **Step 1: Full suite**

Run: `pnpm test && pnpm typecheck && pnpm lint`
Expected: all pass, lint 0 (the new `src/plugin-sdk.test.ts` passes; the addon-type import typechecks).

- [ ] **Step 2: CHANGELOG + commit**

`CHANGELOG.md` Unreleased `### Added`: a plugin author SDK (`plugin-sdk/`: `plugin.json` JSON schema + UI-addon TS types + quickstart) and a consolidated plugin developer guide.
```bash
git add CHANGELOG.md
git commit --no-gpg-sign -m "docs: note plugin author SDK in changelog

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

## Self-Review

**Spec coverage:** `plugin.schema.json` (T1) ✓; `$schema` in 5 manifests (T1) ✓; drift-guard test (T1) ✓; `addon.d.ts` + parity test (T2) ✓; `plugin-sdk/README.md` (T3) ✓; consolidated guide + drop Vue-externalize + proxy path (T4) ✓; CHANGELOG (T5) ✓. No npm publish / Go helper (per decisions) ✓. Minimal validator, no ajv (D4) ✓.
**Placeholder scan:** schema + test + `addon.d.ts` are complete code. T3/T4 specify exact content/edits (README sections, guide updates with the grep check). No vague TODOs.
**Type consistency:** `SlotAddon<S>`, `SlotContracts`, `SlotName`, `SlotParent`, `UnmountFn` match `src/utils/pluginSlot.ts`; capability enum + `mode`/`type` literals consistent between schema, test validator, and `addon.d.ts`; `$schema` relative path `../../plugin-sdk/plugin.schema.json` consistent across T1 + T4.
