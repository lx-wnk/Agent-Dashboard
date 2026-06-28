# Plugin Redesign SP4a — Slot Loader Fix + Priority/Override/Extend Chain — Design Spec

> Date: 2026-06-28 · Status: Draft for review · Branch: `feat/plugin-sp4a-slot-chain` (off `feat/plugin-sp4`, which stacks on `feat/plugin-sp3` ← #232 ← #231)
> Parent: `docs/superpowers/specs/2026-06-28-plugin-system-redesign-design.md` (SP4 row). Sibling slices: SP4b (per-plugin settings UI), SP4c (refresh-to-unload notice).

## Why

A frontend plugin-slot system already exists (`usePluginSlots.ts` `loadSlotAddons`, `PluginSlot.vue`, `pluginSlot.ts`; consumed in TaskCard/AgentModal/RefinementChat). SP2 changed the plugin proxy contract from `/api/settings/plugins/{id}/*` to `/api/plugins/{id}/proxy/*` and removed the old per-plugin mount — so the slot loader's manifest + module fetches now 404 (the slot system is silently broken). SP4a repairs that and adds the spec'd composition: priority ordering plus `override` (replace) and `extend` (wrap-parent) modes.

## Scope

In: fix the loader URLs to the SP2 proxy path; extend the UI-manifest + addon contract with `priority` + `mode`; implement priority-sorted resolution with `override` (exclusive) and `extend` (parent-wrapping) in `PluginSlot.vue`, preserving legacy mode-less addons as siblings.

Out: per-plugin settings UI (SP4b); refresh-to-unload notice (SP4c); a `/api/ui-extensions` aggregation endpoint (rejected — per-plugin manifest already works, YAGNI); the plugin SDK / Vue-externalize build (SP5).

## Decisions (resolved in brainstorming)

| # | Decision | Rationale |
|---|---|---|
| D1 | Loader fetches manifest + modules from **`/api/plugins/{id}/proxy/...`** | SP2's contract; the old path was removed. `fetchPluginList` stays on `/api/settings/plugins` (the read-only list SP2 kept). |
| D2 | **Full `extend(parent)` chain now** (not a stacked approximation) | User choice: the complete solution. The mount contract gains a `parent` handle the extend-addon can mount/wrap. |
| D3 | **Mode-less addons render as siblings** (today's behaviour); only addons with explicit `mode` join the priority/override/extend chain | Back-compat: existing voice addons use `mount(el,ctx)` and must keep working; they ignore the new 3rd arg. |
| D4 | An **`override`** addon owns the slot exclusively — suppresses all lower-priority chain addons AND sibling addons | Clear precedence; "override" means replace, not coexist. |
| D5 | `priority`/`mode` come from the **plugin-served `ui-manifest.json`** entry, not a backend change | No new server code; the manifest already carries per-slot module mapping. |

## Architecture

### Mount contract (`src/utils/pluginSlot.ts`)
```ts
export interface SlotParent {
  // Mounts the composed parent chain (all lower-priority addons) into el.
  mount: (el: HTMLElement) => UnmountFn
}
export interface LoadedAddon {
  slot?: string
  priority?: number                 // higher renders outer/first; default 0
  mode?: 'override' | 'extend'      // undefined ⇒ sibling (legacy stacking)
  mount: (el: HTMLElement, ctx: any, parent?: SlotParent | null) => UnmountFn
}
```
`SlotAddon<S>` (author-facing) gains the same optional `priority`/`mode` + the optional `parent` param. Existing addons that omit them are siblings and ignore `parent`.

### UI manifest (`usePluginSlots.ts`)
```ts
export interface UiManifest {
  slots: { slot: string, module: string, priority?: number, mode?: 'override' | 'extend' }[]
}
```
`loadSlotAddons` attaches `priority` + `mode` from the manifest entry onto each `LoadedAddon`. URLs change to `/api/plugins/{id}/proxy/ui-manifest.json` and `/api/plugins/{id}/proxy/{module}`. The legacy `addon.js` fallback path also moves to the proxy namespace. `isSafeModulePath` unchanged (still rejects `..`/absolute/scheme).

### Resolution (`PluginSlot.vue`)
Given the loaded addons for a slot:
1. **Partition**: `chain` = addons with explicit `mode`; `siblings` = addons without `mode`.
2. **Sort** `chain` by `priority` desc (stable; ties keep load order).
3. **Override cut**: find the highest-priority `override`; if present, drop every chain addon below it AND drop all siblings (override owns the slot). The override addon becomes the chain root with `parent = null`.
4. **Compose extend chain** (recursive, top → bottom):
   ```
   compose(i):
     if i >= chain.length: return null
     a = chain[i]
     if a.mode === 'override': return { mount: el => a.mount(el, ctx, null) }
     // extend
     parent = compose(i+1)
     return { mount: el => a.mount(el, ctx, parent) }
   ```
   The root (`compose(0)`) mounts into one host div.
5. **Mount siblings** (if not suppressed by an override) each into its own host div, as today.
6. Teardown: collect every returned `UnmountFn` (chain root + siblings) and call them on unmount, as the current component already does.

### Files
| File | Change |
|---|---|
| `src/utils/pluginSlot.ts` | add `SlotParent`, `priority`/`mode`/`parent` to `LoadedAddon`/`SlotAddon` |
| `src/utils/plugins.ts` | (unchanged — `fetchPluginList` stays on `/api/settings/plugins`) |
| `src/composables/usePluginSlots.ts` | proxy-path URLs; `UiManifest` + `priority`/`mode` propagation |
| `src/composables/usePluginSlots.test.ts` | path assertions + priority/mode propagation |
| `src/components/PluginSlot.vue` | partition + sort + override-cut + extend-compose + sibling mount |
| `src/components/PluginSlot.test.ts` | priority order, override suppression, extend parent mounted, legacy sibling coexistence |

## Data flow
```
<PluginSlot name="agent-modal-footer" :ctx>
 → loadSlotAddons('agent-modal-footer')
     → GET /api/settings/plugins  (candidates with ui_extension|route_extension)
     → per candidate: GET /api/plugins/{id}/proxy/ui-manifest.json
     → import('/api/plugins/{id}/proxy/{module}')  → LoadedAddon{priority,mode,mount}
 → partition → sort desc → override-cut → compose extend chain → mount root + siblings
```

## Error handling
- Manifest 404 / missing → plugin skipped (cached null), others continue (existing behaviour).
- Module import failure → that addon skipped, cache entry dropped for retry (existing).
- A `mount` throwing → that host div removed, other addons unaffected (existing PluginSlot guard); for the chain, a throwing extend addon means its parent (already composed) may be unmounted — wrap each chain mount in try/catch and fall back to mounting the parent directly if the wrapper fails.
- No override + all extend + innermost has `parent=null` → renders the full nest. Empty slot (no addons) → nothing rendered.

## Testing
- Loader: manifest+module URLs use `/api/plugins/{id}/proxy/`; `priority`/`mode` from manifest land on the addon; legacy addon.js still resolves (proxy path).
- PluginSlot: two extend addons → outer wraps inner (assert parent mounted inside outer); an override at top → lower chain + siblings absent; mixed mode + mode-less → mode-less still render as siblings (unless override); priority desc order; teardown calls all unmounts.

## Risks / notes
- Frontend-only; no backend/ent change.
- Back-compat hinges on D3 (mode-less = sibling). Verify the existing voice addons still mount under the new proxy path (their modules move from `/api/settings/plugins/{id}/` to `/api/plugins/{id}/proxy/`).
- `import(/* @vite-ignore */ url)` stays; only the base path changes.
