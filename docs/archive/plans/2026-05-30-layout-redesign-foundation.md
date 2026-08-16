# Layout Redesign — Plan 1: Foundation (App Shell) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the overloaded header + 5 stacked strips with a calm app-shell — collapsible sidebar, 48px topbar, Symfony-style bottom status bar — and split the overloaded `viewMode` into `activeView` + `dashboardLayout`.

**Architecture:** A token-first cascade. Retune CSS variables in `main.css` (calm/Linear palette + new `--accent`) so every component restyles for free. Introduce composables that own shell state (`useViewState`, `useSidebar`, `useStatusBar`), build new shell components that consume them, then slim `App.vue` to compose `AppShell`. Delete the duplicate `SystemMetricsPanel`.

**Tech Stack:** Vue 3 `<script setup>`, TypeScript, Tailwind v4 (`@theme inline` CSS vars), Vitest + `@vue/test-utils` (jsdom), Playwright.

**Scope:** This is **Plan 1 of a series.** It delivers the working shell that fixes the user's stated complaint. Follow-up plans (not in this file): Plan 2 — restyle fan-out across agent/pipeline/cost/config component groups; Plan 3 — `TaskModal.vue` decomposition; Plan 4 — D3 visualization palette pass + full `blue-*`→`accent` sweep. Each delivers working software on its own.

**Spec:** `docs/superpowers/specs/2026-05-30-layout-redesign-design.md`

**Conventions:**
- Run unit tests with `pnpm test` (single run) or `pnpm test -- <file>` to scope.
- Typecheck with `pnpm typecheck`. Lint with `pnpm lint`.
- localStorage keys are `agent-*` prefixed (existing convention).
- Commit after every task. Conventional Commits (`feat:`/`refactor:`); no phase labels in messages.

---

## File Structure

**Create:**
- `src/composables/useViewState.ts` — owns `activeView` + `dashboardLayout`, persistence + migration from `agent-view-mode`.
- `src/composables/useSidebar.ts` — collapsed/pinned/hover-peek state, `Cmd/Ctrl+B`.
- `src/composables/useStatusBar.ts` — expanded-segment + collapsed-to-tab state.
- `src/components/shell/NavItem.vue` — single sidebar nav row (icon, label, badge, active).
- `src/components/shell/AppSidebar.vue` — nav (grouped) + footer host.
- `src/components/shell/SidebarFooter.vue` — quota + cost/tokens + Sessions/Settings/Theme.
- `src/components/shell/AppTopbar.vue` — title + search + per-view CTA slot + Live/Offline.
- `src/components/shell/AppStatusBar.vue` — Symfony-style system + cost segments.
- `src/components/shell/DashboardToolbar.vue` — Cards/List + Claude-only filter.
- `src/components/shell/LivePulse.vue` — SSE liveness, reduced-motion safe.
- `src/components/shell/SkeletonCard.vue` — shimmer placeholder.
- `src/components/shell/AppShell.vue` — grid composition.
- `src/utils/navConfig.ts` — single source of the nav item array.
- Tests alongside each (`*.test.ts`), plus `e2e/shell.spec.ts`.

**Modify:**
- `src/styles/main.css` — calm palette + `--accent` tokens.
- `src/composables/useAgents.ts` — remove `viewMode` (moves to `useViewState`); keep `hideNonClaude`.
- `src/App.vue` — slim to `AppShell` composition; delete strip stack.

**Delete:**
- `src/components/SystemMetricsPanel.vue` (+ its test if any) — duplicate of `ResourceBar`.

---

## Task 1: Calm design tokens + accent

**Files:**
- Modify: `src/styles/main.css:18-58` (the `@theme inline`, `:root`, `.dark` blocks)

This task has no unit test (CSS custom properties are not unit-testable in jsdom). Verification is by typecheck-clean build + visual check.

- [ ] **Step 1: Add accent token bindings to `@theme inline`**

In `src/styles/main.css`, inside the existing `@theme inline { … }` block (after the `--color-fg-faint` line at :30), add:

```css
  --color-accent: var(--accent);
  --color-accent-soft: var(--accent-soft);
```

- [ ] **Step 2: Retune `:root` (light) surfaces + add accent**

Replace the `:root { … }` block (currently main.css:32-44, the one with `--app`/`--card`/… ) with:

```css
:root {
  /* Surfaces — calm light: soft, low-contrast chrome */
  --app: var(--color-slate-50);
  --card: #ffffff;
  --raised: var(--color-slate-100);
  /* Borders — softened */
  --line: var(--color-slate-200);
  --line-strong: var(--color-slate-300);
  /* Text hierarchy (≥7:1 fg/soft/mute, ≥4.5:1 faint) */
  --fg: var(--color-slate-900);
  --fg-soft: var(--color-slate-700);
  --fg-mute: var(--color-slate-600);
  --fg-faint: var(--color-slate-500);
  /* Accent — indigo */
  --accent: var(--color-indigo-600);
  --accent-soft: var(--color-indigo-100);
}
```

- [ ] **Step 3: Retune `.dark` to deeper near-black slate + indigo accent**

Replace the `.dark { … }` block (currently main.css:46-56) with:

```css
.dark {
  /* Calm dark: deeper background so content lifts via subtle surfaces, not hard borders */
  --app: #0b0e14;
  --card: #12151c;
  --raised: #1c212b;
  --line: #1c212b;
  --line-strong: #2a313e;
  --fg: var(--color-slate-100);
  --fg-soft: var(--color-slate-300);
  --fg-mute: var(--color-slate-400);
  --fg-faint: var(--color-slate-500);
  --accent: var(--color-indigo-400);
  --accent-soft: color-mix(in oklch, var(--color-indigo-400) 14%, transparent);
}
```

- [ ] **Step 4: Verify build is clean**

Run: `pnpm build`
Expected: build completes with no CSS errors; `dist/` emitted.

- [ ] **Step 5: Commit**

```bash
git add src/styles/main.css
git commit -m "feat(ui): calm palette + indigo accent design tokens"
```

---

## Task 2: `useViewState` composable (split the overloaded viewMode)

**Files:**
- Create: `src/composables/useViewState.ts`
- Test: `src/composables/useViewState.test.ts`

`activeView` drives the sidebar; `dashboardLayout` drives Cards/List. Migrates the legacy `agent-view-mode` value on first load.

- [ ] **Step 1: Write the failing test**

`src/composables/useViewState.test.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest'

async function freshModule() {
  vi.resetModules()
  return import('./useViewState')
}

describe('useViewState', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('defaults to dashboard/cards with no stored state', async () => {
    const { useViewState } = await freshModule()
    const { activeView, dashboardLayout } = useViewState()
    expect(activeView.value).toBe('dashboard')
    expect(dashboardLayout.value).toBe('cards')
  })

  it('migrates legacy agent-view-mode="pipeline" to activeView=pipeline', async () => {
    localStorage.setItem('agent-view-mode', 'pipeline')
    const { useViewState } = await freshModule()
    expect(useViewState().activeView.value).toBe('pipeline')
  })

  it('migrates legacy "cost-analytics" to activeView=cost', async () => {
    localStorage.setItem('agent-view-mode', 'cost-analytics')
    expect((await freshModule()).useViewState().activeView.value).toBe('cost')
  })

  it('migrates legacy "list" to dashboard view + list layout', async () => {
    localStorage.setItem('agent-view-mode', 'list')
    const { useViewState } = await freshModule()
    const vs = useViewState()
    expect(vs.activeView.value).toBe('dashboard')
    expect(vs.dashboardLayout.value).toBe('list')
  })

  it('persists activeView changes to localStorage', async () => {
    const { useViewState } = await freshModule()
    useViewState().activeView.value = 'workflows'
    expect(localStorage.getItem('agent-active-view')).toBe('workflows')
  })

  it('persists dashboardLayout changes', async () => {
    const { useViewState } = await freshModule()
    useViewState().dashboardLayout.value = 'list'
    expect(localStorage.getItem('agent-dashboard-layout')).toBe('list')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- useViewState`
Expected: FAIL — `Cannot find module './useViewState'`.

- [ ] **Step 3: Write the implementation**

`src/composables/useViewState.ts`:

```ts
import { ref, watch } from 'vue'

export type ActiveView = 'dashboard' | 'workflows' | 'pipeline' | 'cost' | 'config'
export type DashboardLayout = 'cards' | 'list'

const ACTIVE_VIEWS: ActiveView[] = ['dashboard', 'workflows', 'pipeline', 'cost', 'config']

function readInitial(): { view: ActiveView, layout: DashboardLayout } {
  const ls = typeof localStorage !== 'undefined' ? localStorage : null
  const stored = ls?.getItem('agent-active-view') as ActiveView | null
  const storedLayout = ls?.getItem('agent-dashboard-layout') as DashboardLayout | null

  // Migrate legacy single `agent-view-mode` key once.
  const legacy = ls?.getItem('agent-view-mode')
  let view: ActiveView = stored && ACTIVE_VIEWS.includes(stored) ? stored : 'dashboard'
  let layout: DashboardLayout = storedLayout === 'list' ? 'list' : 'cards'

  if (!stored && legacy) {
    switch (legacy) {
      case 'pipeline': view = 'pipeline'; break
      case 'workflows': view = 'workflows'; break
      case 'config-explorer': view = 'config'; break
      case 'cost-analytics': view = 'cost'; break
      case 'list': view = 'dashboard'; layout = 'list'; break
      case 'cards': view = 'dashboard'; layout = 'cards'; break
    }
  }
  return { view, layout }
}

const initial = readInitial()
const activeView = ref<ActiveView>(initial.view)
const dashboardLayout = ref<DashboardLayout>(initial.layout)

watch(activeView, (v) => {
  if (typeof localStorage !== 'undefined')
    localStorage.setItem('agent-active-view', v)
})
watch(dashboardLayout, (l) => {
  if (typeof localStorage !== 'undefined')
    localStorage.setItem('agent-dashboard-layout', l)
})

export function useViewState() {
  return { activeView, dashboardLayout }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- useViewState`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add src/composables/useViewState.ts src/composables/useViewState.test.ts
git commit -m "feat(ui): useViewState splits viewMode into activeView + dashboardLayout"
```

---

## Task 3: Remove `viewMode` from `useAgents`

**Files:**
- Modify: `src/composables/useAgents.ts:13`, `:22-25`, `:152-156`, `:218-230`

Delete the `ViewMode` type, the `viewMode` ref, its persistence watcher, and its export. Consumers will switch to `useViewState` in later tasks (App.vue in Task 14). `hideNonClaude` stays.

- [ ] **Step 1: Remove the type and ref**

Delete line 13 (`type ViewMode = …`) and lines 22-25 (the `stored`/`viewMode` declarations).

- [ ] **Step 2: Remove the persistence watcher**

Delete lines 152-156 (the `// Persist viewMode to localStorage` watch block).

- [ ] **Step 3: Remove from the returned object**

In the `useAgents` return (lines 218-230), delete the `viewMode,` line.

- [ ] **Step 4: Verify typecheck + existing useAgents-dependent tests**

Run: `pnpm typecheck`
Expected: errors ONLY in `src/App.vue` (still references `viewMode`) — that's fixed in Task 14. No errors inside `useAgents.ts`.

Run: `pnpm test -- useAgents`
Expected: PASS (no test imports the removed `viewMode`).

> Note: App.vue typecheck errors are expected until Task 14. Do not "fix" them here.

- [ ] **Step 5: Commit**

```bash
git add src/composables/useAgents.ts
git commit -m "refactor(ui): drop viewMode from useAgents (moved to useViewState)"
```

---

## Task 4: `useSidebar` composable

**Files:**
- Create: `src/composables/useSidebar.ts`
- Test: `src/composables/useSidebar.test.ts`

State: `pinned` (persistent, localStorage) and `hovering` (transient). `expanded = pinned || hovering`. `togglePinned()` flips and persists. `handleShortcut(e)` toggles pin on `Cmd/Ctrl+B`.

- [ ] **Step 1: Write the failing test**

`src/composables/useSidebar.test.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest'

async function freshModule() {
  vi.resetModules()
  return import('./useSidebar')
}

describe('useSidebar', () => {
  beforeEach(() => localStorage.clear())

  it('defaults to collapsed (not pinned, not expanded)', async () => {
    const { useSidebar } = await freshModule()
    const s = useSidebar()
    expect(s.pinned.value).toBe(false)
    expect(s.expanded.value).toBe(false)
  })

  it('restores pinned=true from localStorage', async () => {
    localStorage.setItem('agent-sidebar-pinned', 'true')
    const { useSidebar } = await freshModule()
    expect(useSidebar().expanded.value).toBe(true)
  })

  it('togglePinned flips and persists', async () => {
    const { useSidebar } = await freshModule()
    const s = useSidebar()
    s.togglePinned()
    expect(s.pinned.value).toBe(true)
    expect(localStorage.getItem('agent-sidebar-pinned')).toBe('true')
  })

  it('hovering expands when not pinned', async () => {
    const { useSidebar } = await freshModule()
    const s = useSidebar()
    s.setHovering(true)
    expect(s.expanded.value).toBe(true)
    s.setHovering(false)
    expect(s.expanded.value).toBe(false)
  })

  it('handleShortcut toggles pin on ctrl+b', async () => {
    const { useSidebar } = await freshModule()
    const s = useSidebar()
    const e = new KeyboardEvent('keydown', { key: 'b', ctrlKey: true })
    const prevented = vi.spyOn(e, 'preventDefault')
    s.handleShortcut(e)
    expect(s.pinned.value).toBe(true)
    expect(prevented).toHaveBeenCalled()
  })

  it('handleShortcut ignores plain b', async () => {
    const { useSidebar } = await freshModule()
    const s = useSidebar()
    s.handleShortcut(new KeyboardEvent('keydown', { key: 'b' }))
    expect(s.pinned.value).toBe(false)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- useSidebar`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

`src/composables/useSidebar.ts`:

```ts
import { computed, ref, watch } from 'vue'

const storedPinned = typeof localStorage !== 'undefined'
  ? localStorage.getItem('agent-sidebar-pinned') === 'true'
  : false

const pinned = ref<boolean>(storedPinned)
const hovering = ref(false)
const expanded = computed(() => pinned.value || hovering.value)

watch(pinned, (v) => {
  if (typeof localStorage !== 'undefined')
    localStorage.setItem('agent-sidebar-pinned', String(v))
})

function togglePinned() {
  pinned.value = !pinned.value
}

function setHovering(v: boolean) {
  hovering.value = v
}

function handleShortcut(e: KeyboardEvent) {
  if (e.key === 'b' && (e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey) {
    e.preventDefault()
    togglePinned()
  }
}

export function useSidebar() {
  return { pinned, hovering, expanded, togglePinned, setHovering, handleShortcut }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- useSidebar`
Expected: PASS (6 tests).

- [ ] **Step 5: Commit**

```bash
git add src/composables/useSidebar.ts src/composables/useSidebar.test.ts
git commit -m "feat(ui): useSidebar collapse/pin/hover state + Cmd+B shortcut"
```

---

## Task 5: `useStatusBar` composable

**Files:**
- Create: `src/composables/useStatusBar.ts`
- Test: `src/composables/useStatusBar.test.ts`

State: `collapsed` (whole bar → corner tab, persistent) and `openSegment` (`'system' | 'cost' | null`, transient — which detail panel is expanded upward).

- [ ] **Step 1: Write the failing test**

`src/composables/useStatusBar.test.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest'

async function freshModule() {
  vi.resetModules()
  return import('./useStatusBar')
}

describe('useStatusBar', () => {
  beforeEach(() => localStorage.clear())

  it('defaults: expanded bar, no open segment', async () => {
    const { useStatusBar } = await freshModule()
    const s = useStatusBar()
    expect(s.collapsed.value).toBe(false)
    expect(s.openSegment.value).toBe(null)
  })

  it('toggleSegment opens then closes the same segment', async () => {
    const { useStatusBar } = await freshModule()
    const s = useStatusBar()
    s.toggleSegment('system')
    expect(s.openSegment.value).toBe('system')
    s.toggleSegment('system')
    expect(s.openSegment.value).toBe(null)
  })

  it('toggleSegment switches between segments', async () => {
    const { useStatusBar } = await freshModule()
    const s = useStatusBar()
    s.toggleSegment('system')
    s.toggleSegment('cost')
    expect(s.openSegment.value).toBe('cost')
  })

  it('toggleCollapsed persists and closes any open segment', async () => {
    const { useStatusBar } = await freshModule()
    const s = useStatusBar()
    s.toggleSegment('system')
    s.toggleCollapsed()
    expect(s.collapsed.value).toBe(true)
    expect(s.openSegment.value).toBe(null)
    expect(localStorage.getItem('agent-statusbar-collapsed')).toBe('true')
  })

  it('restores collapsed=true from localStorage', async () => {
    localStorage.setItem('agent-statusbar-collapsed', 'true')
    expect((await freshModule()).useStatusBar().collapsed.value).toBe(true)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- useStatusBar`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

`src/composables/useStatusBar.ts`:

```ts
import { ref, watch } from 'vue'

export type StatusSegment = 'system' | 'cost'

const collapsed = ref<boolean>(
  typeof localStorage !== 'undefined' && localStorage.getItem('agent-statusbar-collapsed') === 'true',
)
const openSegment = ref<StatusSegment | null>(null)

watch(collapsed, (v) => {
  if (typeof localStorage !== 'undefined')
    localStorage.setItem('agent-statusbar-collapsed', String(v))
})

function toggleSegment(seg: StatusSegment) {
  openSegment.value = openSegment.value === seg ? null : seg
}

function toggleCollapsed() {
  collapsed.value = !collapsed.value
  if (collapsed.value)
    openSegment.value = null
}

export function useStatusBar() {
  return { collapsed, openSegment, toggleSegment, toggleCollapsed }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- useStatusBar`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add src/composables/useStatusBar.ts src/composables/useStatusBar.test.ts
git commit -m "feat(ui): useStatusBar segment-expand + collapse state"
```

---

## Task 6: Nav config (SSOT)

**Files:**
- Create: `src/utils/navConfig.ts`
- Test: `src/utils/navConfig.test.ts`

Single source for the nav item list — view key, label, icon glyph, group. Consumed by `AppSidebar` and `AppTopbar` (title lookup).

- [ ] **Step 1: Write the failing test**

`src/utils/navConfig.test.ts`:

```ts
import { describe, expect, it } from 'vitest'
import { NAV_GROUPS, NAV_ITEMS, viewTitle } from './navConfig'

describe('navConfig', () => {
  it('has one item per ActiveView', () => {
    const views = NAV_ITEMS.map(i => i.view).sort()
    expect(views).toEqual(['config', 'cost', 'dashboard', 'pipeline', 'workflows'])
  })

  it('groups are Monitor and Build', () => {
    expect(NAV_GROUPS).toEqual(['Monitor', 'Build'])
  })

  it('every item belongs to a known group', () => {
    for (const item of NAV_ITEMS)
      expect(NAV_GROUPS).toContain(item.group)
  })

  it('viewTitle returns the label for a view', () => {
    expect(viewTitle('dashboard')).toBe('Dashboard')
    expect(viewTitle('cost')).toBe('Cost')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- navConfig`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

`src/utils/navConfig.ts`:

```ts
import type { ActiveView } from '../composables/useViewState'

export type NavGroup = 'Monitor' | 'Build'

export interface NavItemConfig {
  view: ActiveView
  label: string
  icon: string
  group: NavGroup
}

export const NAV_GROUPS: NavGroup[] = ['Monitor', 'Build']

export const NAV_ITEMS: NavItemConfig[] = [
  { view: 'dashboard', label: 'Dashboard', icon: '▦', group: 'Monitor' },
  { view: 'workflows', label: 'Workflows', icon: '⤳', group: 'Monitor' },
  { view: 'pipeline', label: 'Pipeline', icon: '▤', group: 'Build' },
  { view: 'cost', label: 'Cost', icon: '◷', group: 'Build' },
  { view: 'config', label: 'Config', icon: '⊞', group: 'Build' },
]

export function viewTitle(view: ActiveView): string {
  return NAV_ITEMS.find(i => i.view === view)?.label ?? 'Dashboard'
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- navConfig`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add src/utils/navConfig.ts src/utils/navConfig.test.ts
git commit -m "feat(ui): nav config single source of truth"
```

---

## Task 7: `NavItem.vue`

**Files:**
- Create: `src/components/shell/NavItem.vue`
- Test: `src/components/shell/NavItem.test.ts`

Renders one nav row. Shows label only when `expanded`. `aria-current="page"` when active. Optional badge slot. Indigo active treatment via `accent` tokens.

- [ ] **Step 1: Write the failing test**

`src/components/shell/NavItem.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import NavItem from './NavItem.vue'

describe('navItem', () => {
  it('renders label when expanded', () => {
    const w = mount(NavItem, { props: { icon: '▦', label: 'Dashboard', active: false, expanded: true } })
    expect(w.text()).toContain('Dashboard')
  })

  it('hides label text when collapsed (icon-only)', () => {
    const w = mount(NavItem, { props: { icon: '▦', label: 'Dashboard', active: false, expanded: false } })
    // label is present for a11y but visually hidden via sr-only
    expect(w.find('.sr-only').exists()).toBe(true)
  })

  it('sets aria-current=page when active', () => {
    const w = mount(NavItem, { props: { icon: '▦', label: 'Dashboard', active: true, expanded: true } })
    expect(w.get('button').attributes('aria-current')).toBe('page')
  })

  it('emits select on click', async () => {
    const w = mount(NavItem, { props: { icon: '▦', label: 'Dashboard', active: false, expanded: true } })
    await w.get('button').trigger('click')
    expect(w.emitted('select')).toHaveLength(1)
  })

  it('renders badge slot content', () => {
    const w = mount(NavItem, {
      props: { icon: '▦', label: 'Dashboard', active: false, expanded: true },
      slots: { badge: '12' },
    })
    expect(w.text()).toContain('12')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- NavItem`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

`src/components/shell/NavItem.vue`:

```vue
<script setup lang="ts">
defineProps<{
  icon: string
  label: string
  active: boolean
  expanded: boolean
}>()
defineEmits<{ select: [] }>()
</script>

<template>
  <button
    type="button"
    class="group flex items-center gap-3 w-full rounded-lg px-2.5 min-h-[40px] text-[13px] transition-colors"
    :class="active
      ? 'bg-accent-soft text-accent font-semibold'
      : 'text-fg-mute hover:text-fg hover:bg-raised'"
    :aria-current="active ? 'page' : undefined"
    :title="!expanded ? label : undefined"
    @click="$emit('select')"
  >
    <span class="text-[16px] w-5 shrink-0 text-center" aria-hidden="true">{{ icon }}</span>
    <span v-if="expanded" class="truncate">{{ label }}</span>
    <span v-else class="sr-only">{{ label }}</span>
    <span v-if="expanded" class="ml-auto"><slot name="badge" /></span>
  </button>
</template>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- NavItem`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add src/components/shell/NavItem.vue src/components/shell/NavItem.test.ts
git commit -m "feat(ui): NavItem sidebar row component"
```

---

## Task 8: `LivePulse.vue`

**Files:**
- Create: `src/components/shell/LivePulse.vue`
- Test: `src/components/shell/LivePulse.test.ts`

SSE liveness dot. `live` prop true → green pulsing dot + "Live"; false → amber static + "Reconnecting…". Pulse animation carries `motion-reduce:animate-none` so reduced-motion users get a static-but-informative dot.

- [ ] **Step 1: Write the failing test**

`src/components/shell/LivePulse.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import LivePulse from './LivePulse.vue'

describe('livePulse', () => {
  it('shows Live when connected', () => {
    const w = mount(LivePulse, { props: { live: true } })
    expect(w.text()).toContain('Live')
  })

  it('shows Reconnecting when not live', () => {
    const w = mount(LivePulse, { props: { live: false } })
    expect(w.text()).toContain('Reconnecting')
  })

  it('pulse dot disables animation under reduced motion', () => {
    const w = mount(LivePulse, { props: { live: true } })
    expect(w.get('[data-dot]').classes()).toContain('motion-reduce:animate-none')
  })

  it('exposes a status role for screen readers', () => {
    const w = mount(LivePulse, { props: { live: true } })
    expect(w.get('[role="status"]').exists()).toBe(true)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- LivePulse`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

`src/components/shell/LivePulse.vue`:

```vue
<script setup lang="ts">
defineProps<{ live: boolean }>()
</script>

<template>
  <div role="status" class="flex items-center gap-1.5 text-[11px]" :aria-label="live ? 'Live updates connected' : 'Reconnecting to live updates'">
    <span
      data-dot
      class="w-2 h-2 rounded-full"
      :class="live
        ? 'bg-green-500 animate-pulse motion-reduce:animate-none'
        : 'bg-yellow-500'"
      aria-hidden="true"
    />
    <span :class="live ? 'text-fg-mute' : 'text-yellow-600 dark:text-yellow-400'">
      {{ live ? 'Live' : 'Reconnecting…' }}
    </span>
  </div>
</template>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- LivePulse`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add src/components/shell/LivePulse.vue src/components/shell/LivePulse.test.ts
git commit -m "feat(ui): LivePulse reduced-motion-safe SSE indicator"
```

---

## Task 9: `SkeletonCard.vue`

**Files:**
- Create: `src/components/shell/SkeletonCard.vue`
- Test: `src/components/shell/SkeletonCard.test.ts`

Shimmer placeholder matching `AgentCard` proportions; replaces the "Loading agents…" text.

- [ ] **Step 1: Write the failing test**

`src/components/shell/SkeletonCard.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SkeletonCard from './SkeletonCard.vue'

describe('skeletonCard', () => {
  it('renders an aria-hidden shimmer with pulse', () => {
    const w = mount(SkeletonCard)
    const root = w.get('div')
    expect(root.attributes('aria-hidden')).toBe('true')
    expect(root.classes()).toContain('animate-pulse')
    expect(root.classes()).toContain('motion-reduce:animate-none')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- SkeletonCard`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

`src/components/shell/SkeletonCard.vue`:

```vue
<script setup lang="ts">
</script>

<template>
  <div aria-hidden="true" class="bg-card border border-line rounded-lg p-3 animate-pulse motion-reduce:animate-none">
    <div class="h-3.5 w-2/3 bg-raised rounded mb-2" />
    <div class="h-2.5 w-1/3 bg-raised rounded mb-4" />
    <div class="flex gap-2">
      <div class="h-4 w-12 bg-raised rounded" />
      <div class="h-4 w-16 bg-raised rounded" />
    </div>
  </div>
</template>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- SkeletonCard`
Expected: PASS (1 test).

- [ ] **Step 5: Commit**

```bash
git add src/components/shell/SkeletonCard.vue src/components/shell/SkeletonCard.test.ts
git commit -m "feat(ui): SkeletonCard loading placeholder"
```

---

## Task 10: `SidebarFooter.vue`

**Files:**
- Create: `src/components/shell/SidebarFooter.vue`
- Test: `src/components/shell/SidebarFooter.test.ts`

Pinned bottom of the sidebar: quota bar + cost/tokens summary + Sessions/Settings/Theme buttons. Receives display values + the quota object as props (the parent App.vue owns the quota fetch). Emits actions.

- [ ] **Step 1: Write the failing test**

`src/components/shell/SidebarFooter.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import SidebarFooter from './SidebarFooter.vue'

const base = {
  expanded: true,
  totalCostLabel: '$2.34',
  totalTokensLabel: '1.2M',
  quotaPct: 73,
  theme: 'dark' as const,
}

describe('sidebarFooter', () => {
  it('shows cost + tokens when expanded', () => {
    const w = mount(SidebarFooter, { props: base })
    expect(w.text()).toContain('$2.34')
    expect(w.text()).toContain('1.2M')
  })

  it('renders a quota progressbar with aria-valuenow', () => {
    const w = mount(SidebarFooter, { props: base })
    expect(w.get('[role="progressbar"]').attributes('aria-valuenow')).toBe('73')
  })

  it('emits open-sessions / open-settings / toggle-theme', async () => {
    const w = mount(SidebarFooter, { props: base })
    await w.get('[data-testid="footer-sessions"]').trigger('click')
    await w.get('[data-testid="footer-settings"]').trigger('click')
    await w.get('[data-testid="footer-theme"]').trigger('click')
    expect(w.emitted('open-sessions')).toHaveLength(1)
    expect(w.emitted('open-settings')).toHaveLength(1)
    expect(w.emitted('toggle-theme')).toHaveLength(1)
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- SidebarFooter`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

`src/components/shell/SidebarFooter.vue`:

```vue
<script setup lang="ts">
defineProps<{
  expanded: boolean
  totalCostLabel: string
  totalTokensLabel: string
  quotaPct: number
  theme: 'dark' | 'light'
}>()
defineEmits<{
  'open-sessions': []
  'open-settings': []
  'toggle-theme': []
}>()
</script>

<template>
  <div class="mt-auto border-t border-line pt-2 px-1.5 flex flex-col gap-2">
    <div v-if="expanded" class="px-1">
      <div class="flex items-center justify-between text-[10px] text-fg-faint mb-1">
        <span>Quota</span><span>{{ quotaPct }}%</span>
      </div>
      <div
        class="h-1.5 bg-raised rounded-full overflow-hidden"
        role="progressbar"
        :aria-valuenow="quotaPct"
        aria-valuemin="0"
        aria-valuemax="100"
        :aria-label="`Monthly quota ${quotaPct}% used`"
      >
        <div
          class="h-full rounded-full transition-[width]"
          :class="quotaPct >= 90 ? 'bg-red-500' : quotaPct >= 75 ? 'bg-yellow-500' : 'bg-green-500'"
          :style="{ width: `${quotaPct}%` }"
        />
      </div>
      <div class="mt-2 text-[11px] font-mono text-fg-mute flex gap-2">
        <span>{{ totalCostLabel }}</span><span>·</span><span>{{ totalTokensLabel }} tok</span>
      </div>
    </div>
    <div class="flex items-center" :class="expanded ? 'gap-1' : 'flex-col gap-1'">
      <button
        type="button"
        data-testid="footer-sessions"
        class="flex items-center gap-2 rounded-lg px-2 min-h-[36px] text-[12px] text-fg-mute hover:text-fg hover:bg-raised transition-colors"
        :class="expanded ? 'flex-1' : 'w-full justify-center'"
        :title="!expanded ? 'Sessions' : undefined"
        @click="$emit('open-sessions')"
      >
        <span aria-hidden="true">🕘</span><span v-if="expanded">Sessions</span><span v-else class="sr-only">Sessions</span>
      </button>
      <button
        type="button"
        data-testid="footer-settings"
        class="rounded-lg px-2 min-h-[36px] min-w-[36px] text-[14px] text-fg-mute hover:text-fg hover:bg-raised transition-colors"
        aria-label="Settings"
        @click="$emit('open-settings')"
      >
        <span aria-hidden="true">⚙</span>
      </button>
      <button
        type="button"
        data-testid="footer-theme"
        class="rounded-lg px-2 min-h-[36px] min-w-[36px] text-[14px] text-fg-mute hover:text-fg hover:bg-raised transition-colors"
        :aria-label="theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'"
        @click="$emit('toggle-theme')"
      >
        <span aria-hidden="true">{{ theme === 'dark' ? '☀' : '🌙' }}</span>
      </button>
    </div>
  </div>
</template>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- SidebarFooter`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add src/components/shell/SidebarFooter.vue src/components/shell/SidebarFooter.test.ts
git commit -m "feat(ui): SidebarFooter quota + cost + utility actions"
```

---

## Task 11: `AppSidebar.vue`

**Files:**
- Create: `src/components/shell/AppSidebar.vue`
- Test: `src/components/shell/AppSidebar.test.ts`

Composes `NavItem`s from `NAV_ITEMS` grouped by `NAV_GROUPS`, a brand row with pin toggle, and `SidebarFooter`. Drives `useViewState.activeView` and `useSidebar`. Badges: dashboard → `agentCount`, pipeline → `taskCount` (props).

- [ ] **Step 1: Write the failing test**

`src/components/shell/AppSidebar.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

async function load() {
  vi.resetModules()
  localStorage.clear()
  const mod = await import('./AppSidebar.vue')
  const { useViewState } = await import('../../composables/useViewState')
  const { useSidebar } = await import('../../composables/useSidebar')
  return { AppSidebar: mod.default, useViewState, useSidebar }
}

const props = {
  agentCount: 12,
  taskCount: 5,
  totalCostLabel: '$2.34',
  totalTokensLabel: '1.2M',
  quotaPct: 73,
  theme: 'dark' as const,
}

describe('appSidebar', () => {
  beforeEach(() => localStorage.clear())

  it('renders group headers when expanded (pinned)', async () => {
    const { AppSidebar, useSidebar } = await load()
    useSidebar().togglePinned() // expand
    const w = mount(AppSidebar, { props })
    expect(w.text()).toContain('Monitor')
    expect(w.text()).toContain('Build')
  })

  it('clicking a nav item sets activeView', async () => {
    const { AppSidebar, useViewState } = await load()
    const w = mount(AppSidebar, { props })
    const items = w.findAll('button[aria-current], button')
    // find the Pipeline button by its accessible text
    const pipelineBtn = w.findAll('button').find(b => b.text().includes('Pipeline'))!
    await pipelineBtn.trigger('click')
    expect(useViewState().activeView.value).toBe('pipeline')
  })

  it('pin toggle button flips aria-expanded', async () => {
    const { AppSidebar } = await load()
    const w = mount(AppSidebar, { props })
    const toggle = w.get('[data-testid="sidebar-pin"]')
    expect(toggle.attributes('aria-expanded')).toBe('false')
    await toggle.trigger('click')
    expect(toggle.attributes('aria-expanded')).toBe('true')
  })

  it('shows agent count badge on Dashboard', async () => {
    const { AppSidebar, useSidebar } = await load()
    useSidebar().togglePinned()
    const w = mount(AppSidebar, { props })
    expect(w.text()).toContain('12')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- AppSidebar`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

`src/components/shell/AppSidebar.vue`:

```vue
<script setup lang="ts">
import { computed } from 'vue'
import { useSidebar } from '../../composables/useSidebar'
import { useViewState } from '../../composables/useViewState'
import { NAV_GROUPS, NAV_ITEMS } from '../../utils/navConfig'
import NavItem from './NavItem.vue'
import SidebarFooter from './SidebarFooter.vue'

const props = defineProps<{
  agentCount: number
  taskCount: number
  totalCostLabel: string
  totalTokensLabel: string
  quotaPct: number
  theme: 'dark' | 'light'
}>()
const emit = defineEmits<{
  'open-sessions': []
  'open-settings': []
  'toggle-theme': []
}>()

const { expanded, pinned, togglePinned, setHovering } = useSidebar()
const { activeView } = useViewState()

const grouped = computed(() =>
  NAV_GROUPS.map(group => ({ group, items: NAV_ITEMS.filter(i => i.group === group) })))

function badgeFor(view: string): number | null {
  if (view === 'dashboard')
    return props.agentCount
  if (view === 'pipeline')
    return props.taskCount
  return null
}
</script>

<template>
  <nav
    aria-label="Primary"
    class="h-full bg-card border-r border-line flex flex-col py-3 transition-[width] duration-200"
    :class="expanded ? 'w-[220px] px-2' : 'w-[56px] px-1.5'"
    @mouseenter="setHovering(true)"
    @mouseleave="setHovering(false)"
  >
    <div class="flex items-center gap-2 px-1.5 pb-3 mb-2 border-b border-line">
      <div class="w-7 h-7 rounded-lg bg-accent shrink-0" aria-hidden="true" />
      <span v-if="expanded" class="text-[13px] font-semibold text-fg truncate">Agent Overview</span>
      <button
        type="button"
        data-testid="sidebar-pin"
        class="ml-auto text-fg-faint hover:text-fg text-[14px] rounded px-1 min-h-[28px]"
        :aria-expanded="pinned"
        :aria-label="pinned ? 'Unpin sidebar' : 'Pin sidebar open'"
        @click="togglePinned"
      >
        <span aria-hidden="true">{{ pinned ? '«' : '»' }}</span>
      </button>
    </div>

    <div class="flex-1 flex flex-col gap-0.5 overflow-y-auto">
      <template v-for="g in grouped" :key="g.group">
        <div v-if="expanded" class="px-2 pt-3 pb-1 text-[9px] uppercase tracking-wider text-fg-faint font-bold">
          {{ g.group }}
        </div>
        <NavItem
          v-for="item in g.items"
          :key="item.view"
          :icon="item.icon"
          :label="item.label"
          :active="activeView === item.view"
          :expanded="expanded"
          @select="activeView = item.view"
        >
          <template v-if="badgeFor(item.view) !== null" #badge>
            <span class="text-[9px] bg-raised text-fg-mute rounded-full px-1.5 py-0.5">{{ badgeFor(item.view) }}</span>
          </template>
        </NavItem>
      </template>
    </div>

    <SidebarFooter
      :expanded="expanded"
      :total-cost-label="totalCostLabel"
      :total-tokens-label="totalTokensLabel"
      :quota-pct="quotaPct"
      :theme="theme"
      @open-sessions="emit('open-sessions')"
      @open-settings="emit('open-settings')"
      @toggle-theme="emit('toggle-theme')"
    />
  </nav>
</template>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- AppSidebar`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add src/components/shell/AppSidebar.vue src/components/shell/AppSidebar.test.ts
git commit -m "feat(ui): AppSidebar grouped nav + footer"
```

---

## Task 12: `AppTopbar.vue`

**Files:**
- Create: `src/components/shell/AppTopbar.vue`
- Test: `src/components/shell/AppTopbar.test.ts`

48px bar: view title (from `viewTitle`), search input (`v-model` via `searchQuery` prop + `update:searchQuery` emit), a `cta` slot for the per-view primary action, `OfflineBadge`, `LivePulse`.

- [ ] **Step 1: Write the failing test**

`src/components/shell/AppTopbar.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AppTopbar from './AppTopbar.vue'

describe('appTopbar', () => {
  it('renders the view title', () => {
    const w = mount(AppTopbar, { props: { activeView: 'cost', searchQuery: '', live: true } })
    expect(w.text()).toContain('Cost')
  })

  it('emits update:searchQuery on input', async () => {
    const w = mount(AppTopbar, { props: { activeView: 'dashboard', searchQuery: '', live: true } })
    await w.get('input').setValue('foo')
    expect(w.emitted('update:searchQuery')![0]).toEqual(['foo'])
  })

  it('renders the cta slot', () => {
    const w = mount(AppTopbar, {
      props: { activeView: 'dashboard', searchQuery: '', live: true },
      slots: { cta: '<button>+ New Agent</button>' },
    })
    expect(w.text()).toContain('+ New Agent')
  })

  it('shows reconnecting state when not live', () => {
    const w = mount(AppTopbar, { props: { activeView: 'dashboard', searchQuery: '', live: false } })
    expect(w.text()).toContain('Reconnecting')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- AppTopbar`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

`src/components/shell/AppTopbar.vue`:

```vue
<script setup lang="ts">
import type { ActiveView } from '../../composables/useViewState'
import { computed } from 'vue'
import { viewTitle } from '../../utils/navConfig'
import LivePulse from './LivePulse.vue'
import OfflineBadge from '../OfflineBadge.vue'

const props = defineProps<{
  activeView: ActiveView
  searchQuery: string
  live: boolean
}>()
defineEmits<{ 'update:searchQuery': [value: string] }>()

const title = computed(() => viewTitle(props.activeView))
const searchPlaceholder = computed(() =>
  props.activeView === 'pipeline' ? 'Search tasks…' : 'Search…')
</script>

<template>
  <header class="h-12 shrink-0 flex items-center gap-3 px-4 border-b border-line bg-card">
    <h1 class="text-[15px] font-semibold text-fg">{{ title }}</h1>
    <input
      :value="searchQuery"
      type="text"
      :aria-label="searchPlaceholder"
      :placeholder="searchPlaceholder"
      class="ml-auto bg-raised border border-line rounded-lg px-3 py-1.5 text-[13px] text-fg placeholder:text-fg-faint w-[200px] focus:outline-none focus:border-accent focus:w-[260px] transition-[width,border-color] duration-200"
      @input="$emit('update:searchQuery', ($event.target as HTMLInputElement).value)"
    >
    <slot name="cta" />
    <LivePulse :live="live" />
    <OfflineBadge />
  </header>
</template>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- AppTopbar`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add src/components/shell/AppTopbar.vue src/components/shell/AppTopbar.test.ts
git commit -m "feat(ui): AppTopbar title + search + cta slot"
```

---

## Task 13: `DashboardToolbar.vue`

**Files:**
- Create: `src/components/shell/DashboardToolbar.vue`
- Test: `src/components/shell/DashboardToolbar.test.ts`

View-internal control row for the Dashboard: Cards/List segmented control (drives `dashboardLayout`) + "Claude only" filter (`hideNonClaude` via prop + emit). Lighter weight than the topbar.

- [ ] **Step 1: Write the failing test**

`src/components/shell/DashboardToolbar.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import DashboardToolbar from './DashboardToolbar.vue'

describe('dashboardToolbar', () => {
  it('marks the active layout button aria-pressed', () => {
    const w = mount(DashboardToolbar, { props: { layout: 'cards', hideNonClaude: false } })
    const cards = w.get('[data-testid="layout-cards"]')
    expect(cards.attributes('aria-pressed')).toBe('true')
  })

  it('emits update:layout when clicking List', async () => {
    const w = mount(DashboardToolbar, { props: { layout: 'cards', hideNonClaude: false } })
    await w.get('[data-testid="layout-list"]').trigger('click')
    expect(w.emitted('update:layout')![0]).toEqual(['list'])
  })

  it('emits update:hideNonClaude when toggling the filter', async () => {
    const w = mount(DashboardToolbar, { props: { layout: 'cards', hideNonClaude: false } })
    await w.get('[data-testid="claude-only"]').trigger('click')
    expect(w.emitted('update:hideNonClaude')![0]).toEqual([true])
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- DashboardToolbar`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

`src/components/shell/DashboardToolbar.vue`:

```vue
<script setup lang="ts">
import type { DashboardLayout } from '../../composables/useViewState'

const props = defineProps<{
  layout: DashboardLayout
  hideNonClaude: boolean
}>()
defineEmits<{
  'update:layout': [value: DashboardLayout]
  'update:hideNonClaude': [value: boolean]
}>()
</script>

<template>
  <div class="flex items-center gap-1 px-1 py-2">
    <div role="group" aria-label="Layout" class="flex bg-raised rounded-lg overflow-hidden p-0.5 gap-0.5">
      <button
        type="button"
        data-testid="layout-cards"
        class="px-2.5 py-1 text-xs rounded-md transition-colors"
        :class="layout === 'cards' ? 'bg-card text-fg shadow-sm' : 'text-fg-mute hover:text-fg'"
        :aria-pressed="layout === 'cards'"
        @click="$emit('update:layout', 'cards')"
      >
        ⊞ Cards
      </button>
      <button
        type="button"
        data-testid="layout-list"
        class="px-2.5 py-1 text-xs rounded-md transition-colors"
        :class="layout === 'list' ? 'bg-card text-fg shadow-sm' : 'text-fg-mute hover:text-fg'"
        :aria-pressed="layout === 'list'"
        @click="$emit('update:layout', 'list')"
      >
        ≡ List
      </button>
    </div>
    <button
      type="button"
      data-testid="claude-only"
      class="ml-2 border border-line px-2.5 py-1 text-xs rounded-lg transition-colors"
      :class="hideNonClaude ? 'bg-accent text-white border-transparent' : 'text-fg-mute hover:text-fg'"
      :aria-pressed="hideNonClaude"
      @click="$emit('update:hideNonClaude', !hideNonClaude)"
    >
      Claude only
    </button>
  </div>
</template>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- DashboardToolbar`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add src/components/shell/DashboardToolbar.vue src/components/shell/DashboardToolbar.test.ts
git commit -m "feat(ui): DashboardToolbar layout + filter controls"
```

---

## Task 14: `AppStatusBar.vue` (+ delete SystemMetricsPanel)

**Files:**
- Create: `src/components/shell/AppStatusBar.vue`
- Test: `src/components/shell/AppStatusBar.test.ts`
- Delete: `src/components/SystemMetricsPanel.vue`

Bottom Symfony-style bar. Thin strip shows CPU/MEM/DISK/LOAD + cost Δ. Segment buttons (`aria-expanded`) open an upward detail panel. A collapse control reduces the bar to a corner tab. System data comes from `useSystemResources`; cost from a `costDelta` prop. `SystemMetricsPanel` (the duplicate) is deleted; nothing else imports it after Task 15.

- [ ] **Step 1: Confirm SystemMetricsPanel has no other importers**

Run: `grep -rn "SystemMetricsPanel" src/ --include=*.vue --include=*.ts`
Expected: matches only in `src/App.vue` (removed in Task 15) and the file itself. If anything else imports it, stop and report.

- [ ] **Step 2: Write the failing test**

`src/components/shell/AppStatusBar.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../../composables/useSystemResources', () => ({
  useSystemResources: () => ({
    info: { value: {
      cpu: { usage: 34, cores: 8, model: 'x' },
      memory: { total: 100, used: 62, available: 38, usagePercent: 62 },
      disk: { total: 100, used: 48, available: 52, usagePercent: 48, mount: '/' },
      loadAvg: [1.2, 1.0, 0.8],
      uptime: 100,
    } },
  }),
}))

async function load() {
  vi.resetModules()
  localStorage.clear()
  return (await import('./AppStatusBar.vue')).default
}

describe('appStatusBar', () => {
  beforeEach(() => localStorage.clear())

  it('renders compact CPU/MEM values in the strip', async () => {
    const StatusBar = await load()
    const w = mount(StatusBar, { props: { costDelta: 0.42 } })
    expect(w.text()).toContain('34%')
    expect(w.text()).toContain('62%')
  })

  it('expands the system segment on click (aria-expanded)', async () => {
    const StatusBar = await load()
    const w = mount(StatusBar, { props: { costDelta: 0.42 } })
    const seg = w.get('[data-testid="seg-system"]')
    expect(seg.attributes('aria-expanded')).toBe('false')
    await seg.trigger('click')
    expect(seg.attributes('aria-expanded')).toBe('true')
    expect(w.get('[data-testid="panel-system"]').exists()).toBe(true)
  })

  it('collapses to a corner tab', async () => {
    const StatusBar = await load()
    const w = mount(StatusBar, { props: { costDelta: 0.42 } })
    await w.get('[data-testid="statusbar-collapse"]').trigger('click')
    expect(w.get('[data-testid="statusbar-tab"]').exists()).toBe(true)
  })
})
```

- [ ] **Step 3: Run test to verify it fails**

Run: `pnpm test -- AppStatusBar`
Expected: FAIL — module not found.

- [ ] **Step 4: Write the implementation**

`src/components/shell/AppStatusBar.vue`:

```vue
<script setup lang="ts">
import { useStatusBar } from '../../composables/useStatusBar'
import { useSystemResources } from '../../composables/useSystemResources'

defineProps<{ costDelta: number | null }>()

const { collapsed, openSegment, toggleSegment, toggleCollapsed } = useStatusBar()
const { info } = useSystemResources()

function barColor(pct: number): string {
  return pct > 85 ? 'bg-red-500' : pct > 60 ? 'bg-yellow-500' : 'bg-green-500'
}
</script>

<template>
  <div v-if="collapsed" class="shrink-0 flex justify-end border-t border-line bg-card px-2 py-0.5">
    <button
      type="button"
      data-testid="statusbar-tab"
      class="text-[10px] text-fg-faint hover:text-fg px-2 py-0.5 rounded"
      aria-label="Expand status bar"
      @click="toggleCollapsed"
    >
      ▴ metrics
    </button>
  </div>

  <div v-else class="shrink-0 border-t border-line bg-card">
    <!-- upward detail panels -->
    <div v-if="openSegment === 'system'" data-testid="panel-system" class="px-4 py-3 border-b border-line text-[12px] text-fg-mute">
      <div v-if="info" class="grid grid-cols-2 sm:grid-cols-4 gap-3 font-mono">
        <div>CPU {{ Math.round(info.cpu.usage) }}% · {{ info.cpu.cores }} cores</div>
        <div>MEM {{ Math.round(info.memory.usagePercent) }}%</div>
        <div>DISK {{ Math.round(info.disk.usagePercent) }}%</div>
        <div>LOAD {{ info.loadAvg.map(l => l.toFixed(2)).join(' ') }}</div>
      </div>
    </div>
    <div v-if="openSegment === 'cost'" data-testid="panel-cost" class="px-4 py-3 border-b border-line text-[12px] text-fg-mute font-mono">
      Cost delta (3 min): {{ costDelta === null ? '—' : `${costDelta > 0 ? '+' : ''}$${costDelta.toFixed(2)}` }}
    </div>

    <!-- thin strip -->
    <div class="flex items-center gap-3 px-3 h-7 text-[11px] font-mono text-fg-mute">
      <button
        type="button"
        data-testid="seg-system"
        class="flex items-center gap-3 hover:text-fg rounded px-1"
        :aria-expanded="openSegment === 'system'"
        aria-label="Toggle system metrics detail"
        @click="toggleSegment('system')"
      >
        <span v-if="info" class="flex items-center gap-1">CPU
          <span class="inline-block w-10 h-1.5 bg-raised rounded-full overflow-hidden align-middle">
            <span class="block h-full rounded-full" :class="barColor(info.cpu.usage)" :style="{ width: `${info.cpu.usage}%` }" /></span>
          {{ Math.round(info.cpu.usage) }}%</span>
        <span v-if="info">MEM {{ Math.round(info.memory.usagePercent) }}%</span>
        <span v-if="info">DISK {{ Math.round(info.disk.usagePercent) }}%</span>
      </button>
      <span class="w-px h-3.5 bg-line" aria-hidden="true" />
      <button
        type="button"
        data-testid="seg-cost"
        class="hover:text-fg rounded px-1"
        :aria-expanded="openSegment === 'cost'"
        aria-label="Toggle cost trend detail"
        @click="toggleSegment('cost')"
      >
        COST 3m <span :class="(costDelta ?? 0) > 0 ? 'text-green-500' : 'text-fg-faint'">{{ costDelta === null ? '—' : `${costDelta > 0 ? '+' : ''}$${costDelta.toFixed(2)}` }}</span>
      </button>
      <button
        type="button"
        data-testid="statusbar-collapse"
        class="ml-auto text-fg-faint hover:text-fg px-1"
        aria-label="Collapse status bar"
        @click="toggleCollapsed"
      >
        ▾
      </button>
    </div>
  </div>
</template>
```

- [ ] **Step 5: Run test to verify it passes**

Run: `pnpm test -- AppStatusBar`
Expected: PASS (3 tests).

- [ ] **Step 6: Delete the duplicate panel**

Run: `git rm src/components/SystemMetricsPanel.vue`
(If a `SystemMetricsPanel.test.ts` exists, `git rm` it too.)

- [ ] **Step 7: Commit**

```bash
git add src/components/shell/AppStatusBar.vue src/components/shell/AppStatusBar.test.ts
git commit -m "feat(ui): AppStatusBar Symfony-style bar; remove duplicate SystemMetricsPanel"
```

---

## Task 15: `AppShell.vue` (grid composition)

**Files:**
- Create: `src/components/shell/AppShell.vue`
- Test: `src/components/shell/AppShell.test.ts`

Pure layout: a flex column of `[ row: sidebar | (topbar + content) ] + statusbar`. Exposes named slots: `sidebar`, `topbar`, `default` (content), `statusbar`. Keeps the skip-to-content link.

- [ ] **Step 1: Write the failing test**

`src/components/shell/AppShell.test.ts`:

```ts
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import AppShell from './AppShell.vue'

describe('appShell', () => {
  it('renders all four slots', () => {
    const w = mount(AppShell, {
      slots: {
        sidebar: '<div>SIDEBAR</div>',
        topbar: '<div>TOPBAR</div>',
        default: '<div>CONTENT</div>',
        statusbar: '<div>STATUS</div>',
      },
    })
    const t = w.text()
    expect(t).toContain('SIDEBAR')
    expect(t).toContain('TOPBAR')
    expect(t).toContain('CONTENT')
    expect(t).toContain('STATUS')
  })

  it('has a skip-to-content link targeting #main-content', () => {
    const w = mount(AppShell)
    const link = w.get('a[href="#main-content"]')
    expect(link.exists()).toBe(true)
  })

  it('main region is focusable (tabindex -1) with id main-content', () => {
    const w = mount(AppShell)
    const main = w.get('#main-content')
    expect(main.attributes('tabindex')).toBe('-1')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm test -- AppShell`
Expected: FAIL — module not found.

- [ ] **Step 3: Write the implementation**

`src/components/shell/AppShell.vue`:

```vue
<script setup lang="ts">
</script>

<template>
  <div class="h-screen flex flex-col bg-app text-fg font-sans">
    <a
      href="#main-content"
      class="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-[9999] focus:px-4 focus:py-2 focus:bg-accent focus:text-white focus:rounded focus:text-sm focus:font-semibold"
    >Skip to main content</a>

    <div class="flex-1 flex min-h-0">
      <slot name="sidebar" />
      <div class="flex-1 flex flex-col min-w-0">
        <slot name="topbar" />
        <main id="main-content" tabindex="-1" class="flex-1 min-h-0 overflow-y-auto" style="scroll-margin-top: 48px">
          <slot />
        </main>
      </div>
    </div>

    <slot name="statusbar" />
  </div>
</template>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm test -- AppShell`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add src/components/shell/AppShell.vue src/components/shell/AppShell.test.ts
git commit -m "feat(ui): AppShell grid layout with named slots"
```

---

## Task 16: Wire `App.vue` to the shell

**Files:**
- Modify: `src/App.vue` (template + script)

Replace the header + 5-strip stack with `AppShell` + the new shell components. Switch from `useAgents().viewMode` to `useViewState()`. Map `activeView` to the rendered view. Move the channel-script bar + Install button out of the chrome (into a small Dashboard callout for now; full relocation to Settings is Plan 2). Focus `#main-content` on view change.

- [ ] **Step 1: Update the script block**

In `src/App.vue`, replace the `useAgents` destructure (line 62) to drop `viewMode` and add `useViewState`. After the existing imports, add:

```ts
import AppShell from './components/shell/AppShell.vue'
import AppSidebar from './components/shell/AppSidebar.vue'
import AppTopbar from './components/shell/AppTopbar.vue'
import AppStatusBar from './components/shell/AppStatusBar.vue'
import DashboardToolbar from './components/shell/DashboardToolbar.vue'
import SkeletonCard from './components/shell/SkeletonCard.vue'
import { useViewState } from './composables/useViewState'
```

Change line 62 to remove `viewMode`:

```ts
const { agents, costTrend, filteredAgents, selectedAgent, isLoading, error, searchQuery, hideNonClaude, selectAgent, startStream: startAgents } = useAgents({ autoStart: false })
```

Add after it:

```ts
const { activeView, dashboardLayout } = useViewState()

// SSE liveness: agents stream sets isLoading=false on first message.
const live = computed(() => !error.value)

// Focus the main region when the view changes (a11y: announce new view).
watch(activeView, () => {
  nextTick(() => document.getElementById('main-content')?.focus())
})

// Cost delta for the status bar (last 3 min), mirrors CostTrend logic.
const costDelta = computed(() => {
  const pts = costTrend.value
  if (pts.length < 2)
    return null
  return pts[pts.length - 1].cost - pts[Math.max(0, pts.length - 61)].cost
})
```

Add a derived label pair for the footer (reuse existing `totalCost`/`totalTokens` computeds at lines 161-162):

```ts
const totalCostLabel = computed(() => formatCost(totalCost.value))
const totalTokensLabel = computed(() => formatTokens(totalTokens.value))
```

- [ ] **Step 2: Replace the template chrome**

Replace the entire `<div v-else-if="loaded" …>` block (App.vue:206–530) — header, the four `shrink-0` strips, the secondary toolbar, and `<main>` — with the shell composition below. Keep the modal/toast/dialog block (SpawnDialog, RefinementChat, AppModal backlog, SessionList, ApiKeySettings, EditGateModal, SpotlightSearch, the two toast live-regions, the PWA banner) exactly as-is, moved to sit as siblings after `</AppShell>` inside the same root `<div>`.

```vue
  <div v-else-if="loaded">
    <AppShell>
      <template #sidebar>
        <AppSidebar
          :agent-count="filteredAgents.length"
          :task-count="tasks.length"
          :total-cost-label="totalCostLabel"
          :total-tokens-label="totalTokensLabel"
          :quota-pct="quotaPct"
          :theme="theme"
          @open-sessions="showSessions = true"
          @open-settings="showSettings = true"
          @toggle-theme="toggleTheme"
        />
      </template>

      <template #topbar>
        <AppTopbar
          :active-view="activeView"
          :search-query="searchQuery"
          :live="live"
          @update:search-query="searchQuery = $event"
        >
          <template #cta>
            <button
              v-if="activeView === 'pipeline'"
              type="button"
              class="bg-accent text-white rounded-lg px-3 py-1.5 text-[13px] font-semibold hover:brightness-110"
              @click="openNewTask"
            >
              + New Task
            </button>
            <button
              v-if="activeView === 'pipeline'"
              type="button"
              class="bg-raised text-fg border border-line rounded-lg px-3 py-1.5 text-[13px] font-semibold hover:brightness-110"
              data-testid="open-backlog-form"
              @click="openBacklogForm"
            >
              + Backlog
            </button>
            <button
              v-else-if="activeView === 'dashboard'"
              type="button"
              class="bg-accent text-white rounded-lg px-3 py-1.5 text-[13px] font-semibold hover:brightness-110"
              @click="showSpawnDialog = true"
            >
              + New Agent
            </button>
          </template>
        </AppTopbar>
      </template>

      <div class="p-5">
        <DashboardToolbar
          v-if="activeView === 'dashboard'"
          :layout="dashboardLayout"
          :hide-non-claude="hideNonClaude"
          @update:layout="dashboardLayout = $event"
          @update:hide-non-claude="hideNonClaude = $event"
        />

        <div v-if="isLoading && activeView === 'dashboard'" class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-3">
          <SkeletonCard v-for="n in 6" :key="n" />
        </div>
        <p v-else-if="error" class="text-center py-12 text-red-600 dark:text-red-400">
          Error: {{ error }}
        </p>

        <template v-else-if="activeView === 'dashboard'">
          <template v-if="dashboardLayout === 'list'">
            <EmptyAgentState v-if="filteredAgents.length === 0" :search-query="searchQuery" />
            <AgentTable v-else :agents="filteredAgents" @select="selectAgent" />
          </template>
          <template v-else>
            <EmptyAgentState v-if="filteredAgents.length === 0" :search-query="searchQuery" />
            <AgentCardGrid v-else :agents="filteredAgents" @select="selectAgent" />
          </template>
        </template>

        <PipelineBoard
          v-else-if="activeView === 'pipeline'"
          @select="selectTask"
          @open-chat="(t) => { activeConceptTask = t; showRefinementChat = true }"
        />
        <CostAnalyticsView v-else-if="activeView === 'cost'" />
        <ConfigExplorer v-else-if="activeView === 'config'" />
        <WorkflowsView
          v-else-if="activeView === 'workflows'"
          @navigate="(sessionId) => { const a = agents.find(x => x.sessionId === sessionId); if (a) selectAgent(a) }"
        />
      </div>

      <template #statusbar>
        <AppStatusBar :cost-delta="costDelta" />
      </template>
    </AppShell>

    <!-- existing modals/toasts/dialogs block moved here unchanged -->
    <AgentModal :agent="selectedAgent" @close="selectAgent(null)" @navigate="(taskId: string) => navigateTo({ taskId })" />
    <!-- … keep TaskModal, PWA banner, toasts, SpawnDialog, RefinementChat, AppModal backlog, SessionList, ApiKeySettings, EditGateModal, SpotlightSearch exactly as in the current file … -->
  </div>
```

> Keep the existing `LoginPage` branch (App.vue:205) and the final `<div v-else class="min-h-screen bg-app" />` (App.vue:531) unchanged.

- [ ] **Step 3: Remove now-unused imports**

Delete imports no longer referenced: `ResourceBar`, `SystemMetricsPanel`, `CostTrend` (its data now feeds AppStatusBar; the standalone strip is gone), and `OfflineBadge` (now used inside AppTopbar — remove from App.vue only if no longer referenced there). Run `pnpm lint` to surface unused-import errors and remove exactly those.

- [ ] **Step 4: Typecheck + lint + unit tests**

Run: `pnpm typecheck`
Expected: PASS (no `viewMode` errors remain).

Run: `pnpm lint`
Expected: PASS (no unused imports).

Run: `pnpm test`
Expected: PASS — all unit tests including the new shell suite.

- [ ] **Step 5: Commit**

```bash
git add src/App.vue
git commit -m "feat(ui): compose AppShell in App.vue; retire header + strip stack"
```

---

## Task 17: E2E — shell navigation & status bar

**Files:**
- Create: `e2e/shell.spec.ts`

> Check the existing `e2e/` directory for the project's Playwright base URL / config conventions before writing; mirror them. The dev server auto-starts on port 13120 (per `playwright.config`).

- [ ] **Step 1: Write the E2E spec**

`e2e/shell.spec.ts`:

```ts
import { expect, test } from '@playwright/test'

test.describe('app shell', () => {
  test.beforeEach(async ({ page }) => {
    await page.goto('/')
  })

  test('sidebar nav switches the active view', async ({ page }) => {
    await page.getByRole('navigation', { name: 'Primary' }).getByRole('button', { name: 'Pipeline' }).click()
    await expect(page.getByRole('heading', { level: 1 })).toHaveText('Pipeline')
  })

  test('Cmd/Ctrl+B toggles the sidebar pin', async ({ page }) => {
    const pin = page.getByTestId('sidebar-pin')
    await expect(pin).toHaveAttribute('aria-expanded', 'false')
    await page.keyboard.press('Control+b')
    await expect(pin).toHaveAttribute('aria-expanded', 'true')
  })

  test('status bar segment expands a detail panel', async ({ page }) => {
    await page.getByTestId('seg-system').click()
    await expect(page.getByTestId('panel-system')).toBeVisible()
  })

  test('status bar collapses to a corner tab', async ({ page }) => {
    await page.getByTestId('statusbar-collapse').click()
    await expect(page.getByTestId('statusbar-tab')).toBeVisible()
  })
})
```

- [ ] **Step 2: Run the E2E suite**

Run: `pnpm test:e2e -- shell`
Expected: PASS (4 tests). If the runner can't reach live data, the nav/keyboard/collapse assertions still pass (they don't depend on agents); the system panel renders even with no `info` (shows empty grid) — if it flakes on missing data, gate the panel assertion behind `info` presence or seed via a route mock following existing `e2e/` patterns.

- [ ] **Step 3: Commit**

```bash
git add e2e/shell.spec.ts
git commit -m "test(e2e): app shell navigation, pin shortcut, status bar"
```

---

## Self-Review (completed during planning)

**Spec coverage:**
- Shell skeleton (spec §3.1) → Tasks 15, 16. ✅
- Sidebar collapse/pin/hover + Cmd+B (D2) → Tasks 4, 11; e2e Task 17. ✅
- Grouped nav Monitor/Build (D3) → Tasks 6, 11. ✅
- Sidebar footer quota + cost/tokens (D4) → Tasks 10, 11. ✅
- Symfony-style status bar (D5) → Tasks 5, 14. ✅
- Calm/Linear tokens + indigo accent (D6) → Task 1; consumed everywhere. ✅
- viewMode split (spec §3.2) → Tasks 2, 3, 16. ✅
- Dedup SystemMetricsPanel → Task 14. ✅
- Skeletons + LivePulse + focus-on-view-change (spec §8) → Tasks 8, 9, 16. ✅
- 48px topbar, per-view CTA (spec §3.3) → Tasks 12, 16. ✅
- a11y: aria-current/expanded, skip-link, focus move (spec §6) → Tasks 7, 11, 15, 16. ✅
- **Deferred to later plans (documented):** full per-component restyle, channel-script→Settings relocation, Install→Settings, D3 palette, responsive drawer (<1024px), full `blue-*`→`accent` sweep, light-theme contrast QA. These are Plan 2–4 scope; the spec's §4.2/§5 list them.

**Type consistency:** `ActiveView`/`DashboardLayout` (Task 2) reused verbatim in Tasks 6, 12, 16. `StatusSegment` (Task 5) matches `seg-system`/`seg-cost` test ids (Task 14). `useSidebar` returns (`expanded`/`pinned`/`togglePinned`/`setHovering`/`handleShortcut`) consumed consistently in Task 11. ✅

**Placeholder scan:** No TBD/TODO; every code step shows full source. The one "keep existing block unchanged" reference (Task 16 modals) points at concrete current line ranges, not vague instructions. ✅

**Note for executor:** `handleShortcut` from `useSidebar` must be registered on a global `keydown` listener. The existing App.vue `handleKeydown` (App.vue:44-50, Shift+D theme toggle) is the natural host — in Task 16 add `useSidebar().handleShortcut(e)` to that handler, or register separately in `onMounted`. Add this when wiring App.vue.
