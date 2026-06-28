# Plugin Redesign SP4a — Slot Loader Fix + Priority/Override/Extend Chain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Repair the SP2-broken slot loader (proxy path) and add priority-ordered composition with `override` (exclusive replace) and `extend` (wrap-parent) modes, keeping legacy mode-less addons as siblings.

**Architecture:** Extend the addon contract (`pluginSlot.ts`) with `priority`/`mode`/`parent`; the manifest loader (`usePluginSlots.ts`) fetches from `/api/plugins/{id}/proxy/*` and propagates `priority`/`mode`; `PluginSlot.vue` partitions addons into a priority-sorted chain (override cuts, extend composes a parent handle) plus legacy siblings.

**Tech Stack:** Vue 3 `<script setup>`, TypeScript, Vitest + @vue/test-utils.

---

## File Structure

| File | Responsibility | Action |
|---|---|---|
| `src/utils/pluginSlot.ts` | addon contract: `SlotParent`, `priority`/`mode`/`parent` | Modify |
| `src/composables/usePluginSlots.ts` | proxy-path URLs; `UiManifest` w/ priority+mode; propagate onto addons | Modify |
| `src/composables/usePluginSlots.test.ts` | path + priority/mode propagation tests | Modify |
| `src/components/PluginSlot.vue` | partition → sort → override-cut → extend-compose → mount | Modify |
| `src/components/PluginSlot.test.ts` | order/override/extend/sibling tests | Modify |

**Commands:** `pnpm test <file>`, `pnpm typecheck`, `pnpm lint` (antfu, must be 0). Worktree needs `pnpm i`. Commits `--no-gpg-sign`, English, no phase labels, trailers:
```
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6
```

---

### Task 1: Extend the addon contract

**Files:** Modify `src/utils/pluginSlot.ts`

- [ ] **Step 1: Add the parent handle + fields**

In `src/utils/pluginSlot.ts`, add `SlotParent` and extend `LoadedAddon` + `SlotAddon`:

```ts
export type UnmountFn = () => void

/** A composed lower-priority chain an `extend` addon may mount/wrap. */
export interface SlotParent {
  mount: (el: HTMLElement) => UnmountFn
}
```

Extend `SlotAddon<S>` (add the optional fields + parent param):

```ts
export interface SlotAddon<S extends SlotName = SlotName> {
  slot?: S
  /** Higher renders outer/first. Default 0. */
  priority?: number
  /** 'override' = own the slot exclusively; 'extend' = wrap the parent chain. Undefined = sibling. */
  mode?: 'override' | 'extend'
  mount: (el: HTMLElement, ctx: SlotContracts[S], parent?: SlotParent | null) => UnmountFn
}
```

Extend `LoadedAddon` (type-erased host boundary):

```ts
export interface LoadedAddon {
  slot?: string
  priority?: number
  mode?: 'override' | 'extend'
  mount: (el: HTMLElement, ctx: any, parent?: SlotParent | null) => UnmountFn
}
```

- [ ] **Step 2: Typecheck**

Run: `pnpm typecheck`
Expected: PASS (additive optional fields; existing addons/usages still compile — the new `parent` param is optional, existing `mount(el,ctx)` impls remain assignable).

- [ ] **Step 3: Commit**

```bash
git add src/utils/pluginSlot.ts
git commit --no-gpg-sign -m "feat: add priority, mode, and parent to slot addon contract

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 2: Loader — proxy path + priority/mode propagation

**Files:** Modify `src/composables/usePluginSlots.ts`, `src/composables/usePluginSlots.test.ts`

- [ ] **Step 1: Update the failing test first**

In `usePluginSlots.test.ts`, the existing tests assert the old `/api/settings/plugins/{id}/...` URLs. Update them to the proxy path and add a priority/mode propagation case. Read the current test file, then:
- Change every expected manifest URL to `/api/plugins/{id}/proxy/ui-manifest.json` and every module URL to `/api/plugins/{id}/proxy/{module}`.
- Add a test: a manifest entry `{ slot: 'task-modal-footer', module: 'a.js', priority: 50, mode: 'extend' }` → the returned `LoadedAddon` has `priority === 50` and `mode === 'extend'`.

Concrete addition (adapt the mock-deps shape to the existing tests' `loadSlotAddons(slot, { fetchPlugins, fetchManifest, importAddon })`):

```ts
it('propagates priority and mode from the manifest entry', async () => {
  resetSlotCaches()
  const addons = await loadSlotAddons('task-modal-footer', {
    fetchPlugins: async () => [{ id: 'p1', capabilities: ['ui_extension'] }],
    fetchManifest: async () => ({ slots: [{ slot: 'task-modal-footer', module: 'a.js', priority: 50, mode: 'extend' }] }),
    importAddon: async () => ({ default: { mount: () => () => {} } }),
  })
  expect(addons).toHaveLength(1)
  expect(addons[0].priority).toBe(50)
  expect(addons[0].mode).toBe('extend')
})
```

- [ ] **Step 2: Run tests to verify failures**

Run: `pnpm test src/composables/usePluginSlots.test.ts`
Expected: FAIL — old URL assertions now mismatch / `priority`/`mode` undefined.

- [ ] **Step 3: Implement**

In `usePluginSlots.ts`:
1. Extend `UiManifest`:
```ts
export interface UiManifest {
  slots: { slot: string, module: string, priority?: number, mode?: 'override' | 'extend' }[]
}
```
2. Change `defaultFetchManifest` URL to `/api/plugins/${pluginId}/proxy/ui-manifest.json`.
3. In `loadSlotAddons`, change both import URLs to the proxy namespace:
   - manifest module: `` `/api/plugins/${p.id}/proxy/${entry.module}` ``
   - legacy fallback: `` `/api/plugins/${p.id}/proxy/addon.js` ``
4. When pushing a manifest-mapped addon, carry `priority`/`mode`:
```ts
if (mod.default)
  addons.push({ ...mod.default, slot, priority: entry.priority, mode: entry.mode })
```
(Leave the legacy `addon.js` branch as-is — those stay mode-less siblings.)

> `fetchPluginList` in `src/utils/plugins.ts` stays on `/api/settings/plugins` (that read-only list survived SP2) — do NOT change it.

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm test src/composables/usePluginSlots.test.ts && pnpm typecheck`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add src/composables/usePluginSlots.ts src/composables/usePluginSlots.test.ts
git commit --no-gpg-sign -m "fix: load plugin slot assets from the proxy path with priority and mode

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 3: PluginSlot resolution — sort, override-cut, extend-compose, siblings

**Files:** Modify `src/components/PluginSlot.vue`, `src/components/PluginSlot.test.ts`

- [ ] **Step 1: Write the failing tests**

In `PluginSlot.test.ts`, add (using the injectable `loader` prop the component already supports):

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import PluginSlot from './PluginSlot.vue'

function addon(opts: any) {
  return { mount: opts.mount, priority: opts.priority, mode: opts.mode }
}

describe('PluginSlot composition', () => {
  it('mounts siblings (mode-less) in load order', async () => {
    const calls: string[] = []
    const loader = async () => [
      addon({ mount: () => { calls.push('a'); return () => {} } }),
      addon({ mount: () => { calls.push('b'); return () => {} } }),
    ]
    mount(PluginSlot, { props: { name: 'task-modal-footer', ctx: { task: {} }, loader } })
    await new Promise(r => setTimeout(r))
    expect(calls).toEqual(['a', 'b'])
  })

  it('override (highest priority) suppresses lower chain addons and siblings', async () => {
    const calls: string[] = []
    const loader = async () => [
      addon({ mode: 'override', priority: 100, mount: () => { calls.push('override'); return () => {} } }),
      addon({ mode: 'extend', priority: 50, mount: () => { calls.push('lower'); return () => {} } }),
      addon({ mount: () => { calls.push('sibling'); return () => {} } }),
    ]
    mount(PluginSlot, { props: { name: 'task-modal-footer', ctx: { task: {} }, loader } })
    await new Promise(r => setTimeout(r))
    expect(calls).toEqual(['override'])
  })

  it('extend receives a parent it can mount', async () => {
    const events: string[] = []
    const loader = async () => [
      addon({ mode: 'extend', priority: 100, mount: (_el: HTMLElement, _ctx: any, parent: any) => {
        events.push('outer')
        if (parent) {
          const child = document.createElement('div')
          parent.mount(child)
        }
        return () => {}
      } }),
      addon({ mode: 'extend', priority: 10, mount: () => { events.push('inner'); return () => {} } }),
    ]
    mount(PluginSlot, { props: { name: 'task-modal-footer', ctx: { task: {} }, loader } })
    await new Promise(r => setTimeout(r))
    expect(events).toEqual(['outer', 'inner']) // outer mounts, then mounts its parent (inner)
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pnpm test src/components/PluginSlot.test.ts`
Expected: FAIL — current component mounts everything flat (override/extend not honoured; override test sees 3 calls).

- [ ] **Step 3: Implement**

Replace the `onMounted` body in `PluginSlot.vue` with partition + chain resolution. The full `<script setup>` mount logic:

```ts
onMounted(async () => {
  const addons = await props.loader(props.name)
  if (cancelled)
    return
  const container = containerEl.value
  if (!container)
    return

  const chain = addons.filter(a => a.mode === 'override' || a.mode === 'extend')
    .sort((a, b) => (b.priority ?? 0) - (a.priority ?? 0))
  let siblings = addons.filter(a => a.mode !== 'override' && a.mode !== 'extend')

  // An override at the top owns the slot exclusively: drop everything below it + all siblings.
  const overrideIdx = chain.findIndex(a => a.mode === 'override')
  let activeChain = chain
  if (overrideIdx !== -1) {
    activeChain = chain.slice(0, overrideIdx + 1) // keep down to (and incl.) the override
    siblings = []
  }

  // compose(i): build the parent handle for chain[i..]. override stops the chain (parent=null).
  const ctx = toRaw(props.ctx)
  const compose = (i: number): SlotParent | null => {
    if (i >= activeChain.length)
      return null
    const a = activeChain[i]
    if (a.mode === 'override')
      return { mount: (el: HTMLElement) => a.mount(el, ctx, null) }
    const parent = compose(i + 1)
    return { mount: (el: HTMLElement) => a.mount(el, ctx, parent) }
  }

  const root = compose(0)
  if (root) {
    const host = document.createElement('div')
    host.setAttribute('data-addon-host', '')
    container.appendChild(host)
    try {
      unmounts.push(root.mount(host))
    }
    catch {
      host.remove()
    }
  }

  for (const addon of siblings) {
    const host = document.createElement('div')
    host.setAttribute('data-addon-host', '')
    container.appendChild(host)
    try {
      unmounts.push(addon.mount(host, ctx))
    }
    catch {
      host.remove()
    }
  }
})
```

Add `SlotParent` to the type import: `import type { LoadedAddon, SlotContracts, SlotName, SlotParent, UnmountFn } from '../utils/pluginSlot'`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm test src/components/PluginSlot.test.ts && pnpm typecheck && pnpm lint`
Expected: PASS, lint 0.

- [ ] **Step 5: Commit**

```bash
git add src/components/PluginSlot.vue src/components/PluginSlot.test.ts
git commit --no-gpg-sign -m "feat: compose plugin slot addons by priority with override and extend

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

### Task 4: Full verify + docs

**Files:** Modify `CHANGELOG.md` (+ `docs/plugin-guide.md` if it documents the slot manifest)

- [ ] **Step 1: Full suite**

Run: `pnpm test && pnpm typecheck && pnpm lint`
Expected: all pass, lint 0. Confirm the existing slot-consuming components (TaskCard/AgentModal/RefinementChat) still type/test green.

- [ ] **Step 2: Docs**

`docs/plugin-guide.md`: update the UI-manifest example to the proxy path (`/api/plugins/{id}/proxy/ui-manifest.json`) and document the new optional `priority` + `mode` (`override`/`extend`) per slot entry, including that an `extend` addon receives a `parent` it may mount. `CHANGELOG.md` Unreleased `### Added`/`### Fixed`: slot composition (priority/override/extend) + the proxy-path fix.

- [ ] **Step 3: Commit**

```bash
git add CHANGELOG.md docs/
git commit --no-gpg-sign -m "docs: plugin slot composition and proxy-path manifest

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01VjZCeWafzHdeVQFa2cu6f6"
```

---

## Self-Review

**Spec coverage:** contract `SlotParent`/priority/mode/parent (T1) ✓; proxy-path fix + manifest priority/mode (T2) ✓; partition/sort/override-cut/extend-compose/siblings (T3) ✓; docs (T4) ✓. No aggregation endpoint (per decision) ✓. Frontend-only ✓.
**Placeholder scan:** test code + the full `onMounted` body are concrete. The one "adapt to existing test mock shape" note (T2 Step 1) is bounded against the real file. No vague TODOs.
**Type consistency:** `SlotParent.mount(el)`, `LoadedAddon.{priority,mode,mount(el,ctx,parent)}`, `compose(i): SlotParent|null`, `UiManifest.slots[].{priority,mode}` — consistent across tasks. `mode` literal `'override'|'extend'` everywhere.
