# Error Display: Unify Inline Errors as Toasts

**Goal:** Replace ~37 scattered inline `<p v-if="error" class="text-danger-text">` blocks with
a singleton `useToast` composable callable from any component, and wire a `ToastHost.vue`
into App.vue that supersedes the existing hand-rolled single-toast system.

**Architecture:**
- `src/composables/useToast.ts` — module-level singleton reactive store; multiple concurrent
  toasts; per-toast auto-dismiss timer with pause/resume.
- `src/components/ToastHost.vue` — renders the toast stack; replaces App.vue lines 76–228 +
  505–516.
- Remove the `@toast` event prop chain (App.vue → AgentTriageBand → AgentRow/AgentTable;
  App.vue → AgentModal); migrated emitters call `useToast` directly.
- 5 migration batches covering every confirmed inline error component.

**Tech Stack:** Vue 3, TypeScript, Vitest (vi.useFakeTimers for timer tests),
@vue/test-utils (mount harness for component tests).

---

## Task 1 — Build `useToast.ts` + unit tests

### What to build

`src/composables/useToast.ts`:

```typescript
import { ref } from 'vue'

export interface Toast {
  id: string
  message: string
  type: 'error' | 'success' | 'info'
}

const DEFAULT_DURATION_MS = 5000

// Module-level state — singleton across all importers.
const toasts = ref<Toast[]>([])

interface TimerEntry {
  remaining: number   // ms remaining when last started/resumed
  startedAt: number   // performance.now() when timer was started
  timerId: ReturnType<typeof setTimeout>
}
const timers = new Map<string, TimerEntry>()

function add(message: string, type: Toast['type'], duration = DEFAULT_DURATION_MS): string {
  const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`
  toasts.value.push({ id, message, type })
  schedule(id, duration)
  return id
}

function schedule(id: string, remaining: number) {
  const timerId = setTimeout(() => dismiss(id), remaining)
  timers.set(id, { remaining, startedAt: performance.now(), timerId })
}

export function dismiss(id: string) {
  clearTimeout(timers.get(id)?.timerId)
  timers.delete(id)
  toasts.value = toasts.value.filter(t => t.id !== id)
}

export function pauseToast(id: string) {
  const entry = timers.get(id)
  if (!entry) return
  clearTimeout(entry.timerId)
  entry.remaining = Math.max(0, entry.remaining - (performance.now() - entry.startedAt))
  timers.set(id, { ...entry, timerId: -1 as unknown as ReturnType<typeof setTimeout> })
}

export function resumeToast(id: string) {
  const entry = timers.get(id)
  if (!entry || entry.remaining <= 0) { dismiss(id); return }
  schedule(id, entry.remaining)
}

export const toast = {
  error: (msg: string, duration?: number) => add(msg, 'error', duration),
  success: (msg: string, duration?: number) => add(msg, 'success', duration),
  info: (msg: string, duration?: number) => add(msg, 'info', duration),
}

export function useToast() {
  return { toasts, toast, dismiss, pauseToast, resumeToast }
}
```

### Unit tests

`src/composables/__tests__/useToast.test.ts`:

```typescript
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
// Re-import after vi.resetModules() so each test gets fresh module state.
let useToast: typeof import('../useToast').useToast
let dismiss: typeof import('../useToast').dismiss
let pauseToast: typeof import('../useToast').pauseToast
let resumeToast: typeof import('../useToast').resumeToast
let toast: typeof import('../useToast').toast

beforeEach(async () => {
  vi.useFakeTimers()
  vi.resetModules()
  const mod = await import('../useToast')
  useToast = mod.useToast
  dismiss = mod.dismiss
  pauseToast = mod.pauseToast
  resumeToast = mod.resumeToast
  toast = mod.toast
})

afterEach(() => {
  vi.useRealTimers()
})

describe('useToast', () => {
  it('adds a toast and exposes it in toasts', async () => {
    const { toasts } = useToast()
    toast.error('boom')
    await nextTick()
    expect(toasts.value).toHaveLength(1)
    expect(toasts.value[0]).toMatchObject({ message: 'boom', type: 'error' })
  })

  it('stacks multiple concurrent toasts', async () => {
    const { toasts } = useToast()
    toast.error('a')
    toast.success('b')
    toast.info('c')
    await nextTick()
    expect(toasts.value).toHaveLength(3)
  })

  it('auto-dismisses after 5 s', async () => {
    const { toasts } = useToast()
    toast.error('bye')
    await nextTick()
    expect(toasts.value).toHaveLength(1)
    vi.advanceTimersByTime(5000)
    await nextTick()
    expect(toasts.value).toHaveLength(0)
  })

  it('dismiss() removes a specific toast immediately', async () => {
    const { toasts } = useToast()
    toast.error('keep')
    const id = toast.error('remove')
    await nextTick()
    dismiss(id)
    await nextTick()
    expect(toasts.value).toHaveLength(1)
    expect(toasts.value[0].message).toBe('keep')
  })

  it('pause halts auto-dismiss; resume restarts with remaining time', async () => {
    const { toasts } = useToast()
    const id = toast.error('hover', 5000)
    await nextTick()
    vi.advanceTimersByTime(3000)
    pauseToast(id)
    vi.advanceTimersByTime(5000) // would dismiss without pause
    await nextTick()
    expect(toasts.value).toHaveLength(1) // still alive
    resumeToast(id)
    vi.advanceTimersByTime(2000) // remaining ~2 s
    await nextTick()
    expect(toasts.value).toHaveLength(0)
  })

  it('respects a custom duration', async () => {
    const { toasts } = useToast()
    toast.info('quick', 1000)
    await nextTick()
    vi.advanceTimersByTime(999)
    await nextTick()
    expect(toasts.value).toHaveLength(1)
    vi.advanceTimersByTime(1)
    await nextTick()
    expect(toasts.value).toHaveLength(0)
  })
})
```

**Commit:** `feat: add useToast singleton composable with stack + pause/resume`
`git commit --no-gpg-sign`

---

## Task 2 — Build `ToastHost.vue` + wire App.vue + migrate @toast emitters

### 2a — `src/components/ToastHost.vue`

```vue
<script setup lang="ts">
import { dismiss, pauseToast, resumeToast, useToast } from '../composables/useToast'

const { toasts } = useToast()

const typeClasses: Record<string, string> = {
  error:   'bg-raised border-line text-fg',
  success: 'bg-raised border-line text-fg',
  info:    'bg-raised border-line text-fg',
}
</script>

<template>
  <!-- F-UIUX-011: live region always in DOM so screen readers pick up insertions. -->
  <div role="status" aria-live="polite" aria-atomic="true" class="pointer-events-none">
    <TransitionGroup name="toast" tag="div">
      <div
        v-for="t in toasts"
        :key="t.id"
        class="pointer-events-auto fixed bottom-6 left-1/2 -translate-x-1/2 border px-5 py-2.5 rounded-lg text-[13px] z-[2000] shadow-[0_4px_16px_rgba(0,0,0,0.4)] mb-1"
        :class="typeClasses[t.type]"
        @mouseenter="pauseToast(t.id)"
        @mouseleave="resumeToast(t.id)"
      >
        {{ t.message }}
      </div>
    </TransitionGroup>
  </div>
</template>
```

### 2b — App.vue changes (App.vue is the only file modified in this step)

Remove lines 76–90 (`TOAST_DURATION_MS`, `toastMessage`, `toastTimer`, `toastPaused`,
`onUnmounted` timer clear) and lines 201–226 (`startToastTimer`, `pauseToast`,
`resumeToast`, `showToast`).

Remove `@toast="showToast"` from `<AgentTriageBand>` (line 420).

Add `import ToastHost from './components/ToastHost.vue'` and add `<ToastHost />` in
the template where lines 505–516 were. The PWA refresh toast (lines 485–502) is a
separate intentional pattern and stays as-is.

### 2c — Migrate emitters to call useToast directly

Four files currently emit `toast` events up the chain:

**AgentModal.vue** — remove `toast: [message: string]` from `defineEmits`, add
`import { toast } from '../composables/useToast'`, replace every `emit('toast', msg)`
with `toast.error(msg)`.

**AgentTriageBand.vue** — same pattern: remove emit, import toast, replace all three
call sites (lines 207, 228, 232).

**AgentRow.vue** — same pattern.

**AgentTable.vue** — same pattern (it only re-emits from AgentRow; after AgentRow is
migrated the AgentTable `@toast` re-emit chain disappears automatically — remove the
re-emit and the `defineEmits` entry).

### ToastHost component test

`src/components/__tests__/ToastHost.test.ts`:

```typescript
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

let ToastHost: any
let toastMod: typeof import('../../composables/useToast')

beforeEach(async () => {
  vi.useFakeTimers()
  vi.resetModules()
  toastMod = await import('../../composables/useToast')
  ToastHost = (await import('../ToastHost.vue')).default
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('ToastHost', () => {
  it('renders a toast when added', async () => {
    const w = mount(ToastHost)
    toastMod.toast.error('something failed')
    await nextTick()
    expect(w.text()).toContain('something failed')
  })

  it('removes the toast after auto-dismiss', async () => {
    const w = mount(ToastHost)
    toastMod.toast.error('gone soon')
    await nextTick()
    vi.advanceTimersByTime(5000)
    await nextTick()
    expect(w.text()).not.toContain('gone soon')
  })

  it('renders multiple stacked toasts', async () => {
    const w = mount(ToastHost)
    toastMod.toast.error('first')
    toastMod.toast.success('second')
    await nextTick()
    expect(w.text()).toContain('first')
    expect(w.text()).toContain('second')
  })
})
```

**Commit:** `feat: add ToastHost component, wire into App.vue, drop @toast event chain`
`git commit --no-gpg-sign`

---

## Batch 3 — Settings panels (11 files)

**Files:**
`AppSettings.vue`, `NotificationSettings.vue`, `PipelineConfigSettings.vue`,
`ProjectSettings.vue`, `SpawnerSettings.vue`, `RemoteSettings.vue`,
`ProviderSettings.vue`, `SystemPromptSettings.vue`, `ApiKeySettings.vue`,
`PluginSettings.vue`, `PluginSettingsForm.vue`

### Transform pattern

All settings components follow the same shape: a local `error` ref (sometimes exposed
from a composable, sometimes set in a catch block) rendered in a `role="alert"` block
or a plain `<p>` / `<div>` conditional.

**Before (AppSettings.vue, representative):**

```typescript
// script setup
const { items, loading, error, refetch, update } = useSettings()

async function apply(item: SettingView, value: string) {
  try { ... }
  catch (e) {
    error.value = errorMessage(e, 'Failed to save setting')  // sets reactive ref
  }
}
```

```html
<!-- template -->
<div role="alert" aria-atomic="true" class="text-xs text-danger-text"
     :class="{ 'sr-only': !error || loading }">
  {{ !loading ? (error ?? '') : '' }}
</div>
<div v-if="!loading && !error" class="space-y-6">...</div>
```

**After (AppSettings.vue):**

```typescript
import { toast } from '../composables/useToast'
// Remove: error from useSettings destructure (no longer displayed)
const { items, loading, refetch, update } = useSettings()

async function apply(item: SettingView, value: string) {
  try { ... }
  catch (e) {
    toast.error(errorMessage(e, 'Failed to save setting'))  // calls singleton
  }
}
```

```html
<!-- template — remove the role="alert" block entirely -->
<!-- Remove the `&& !error` guard from the content div -->
<div v-if="!loading" class="space-y-6">...</div>
```

Note on `AppSettings.vue`: the composable exposes `error` as a settable ref (the catch
block assigns to it). After migration, stop assigning to it at all — the composable ref
can be left in place but unused by this component if other call sites still need it.
The `NotificationSettings` pattern is identical.

Note on `PipelineConfigSettings.vue`: it uses `text-red-600 dark:text-red-400` rather
than `text-danger-text` — still migrate (both patterns are inline error display).

### Representative test assertion (AppSettings)

```typescript
// src/components/__tests__/AppSettings.test.ts
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

let AppSettings: any
let toastMod: typeof import('../../composables/useToast')

beforeEach(async () => {
  vi.resetModules()
  toastMod = await import('../../composables/useToast')
  vi.spyOn(toastMod.toast, 'error')
  AppSettings = (await import('../AppSettings.vue')).default
})

it('calls toast.error when save fails', async () => {
  globalThis.fetch = vi.fn()
    .mockResolvedValueOnce({ ok: true, json: async () => [] })          // GET settings
    .mockResolvedValueOnce({ ok: false, status: 500, json: async () => ({}) }) // PATCH fails
  const w = mount(AppSettings)
  await flushPromises()
  // trigger an update — simulate calling apply() directly
  await (w.vm as any).apply({ key: 'some.key', type: 'bool', value: 'false' }, 'true')
  await nextTick()
  expect(toastMod.toast.error).toHaveBeenCalled()
  // No inline danger text in the rendered output
  expect(w.find('.text-danger-text').exists()).toBe(false)
})
```

**Commit:** `feat: migrate settings panels to toast.error, remove inline error blocks`
`git commit --no-gpg-sign`

---

## Batch 4 — Data/async views (8 files)

**Files:**
`CostAnalyticsView.vue`, `SchedulesView.vue`, `EvalView.vue`, `ConfigExplorer.vue`,
`AuditLogTab.vue`, `ExecutionWaterfall.vue`, `GitStatusPanel.vue`, `WorktreePanel.vue`

### Transform pattern

These components receive `error` from a composable and render it in a full-view
`v-else-if` block that hides content.

**Before (CostAnalyticsView.vue, representative):**

```typescript
const { summary, isLoading, error, from, to, setRange, start, refresh } = useCostAnalytics()
```

```html
<p v-else-if="error" class="text-sm text-danger-text">{{ error }}</p>
```

**After:**

```typescript
import { watch } from 'vue'
import { toast } from '../composables/useToast'
const { summary, isLoading, error, from, to, setRange, start, refresh } = useCostAnalytics()

// Surface async load errors as toasts; component shows empty/loading state instead.
watch(error, (msg) => { if (msg) toast.error(msg) })
```

```html
<!-- Remove the v-else-if="error" branch; the content renders with isLoading guard only -->
```

Note on `ConfigExplorer.vue`: the 409 conflict inline message ("changed on disk" +
Reload button) is NOT an error toast — it is an interactive inline prompt. Keep that
block inline. Only the `fetchError` load-failure display migrates.

`EvalView.vue` uses `text-red-600` — still migrate.

### Representative test assertion (SchedulesView)

```typescript
// src/components/__tests__/SchedulesView.test.ts
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

let SchedulesView: any
let toastMod: typeof import('../../composables/useToast')

beforeEach(async () => {
  vi.resetModules()
  toastMod = await import('../../composables/useToast')
  vi.spyOn(toastMod.toast, 'error')
  globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status: 500, json: async () => ({}) })
  SchedulesView = (await import('../SchedulesView.vue')).default
})

it('calls toast.error and does not render inline danger text when load fails', async () => {
  const w = mount(SchedulesView)
  await flushPromises()
  await nextTick()
  expect(toastMod.toast.error).toHaveBeenCalled()
  expect(w.find('.text-danger-text').exists()).toBe(false)
})
```

**Commit:** `feat: migrate async views to toast.error, remove inline error paragraphs`
`git commit --no-gpg-sign`

---

## Batch 5 — Visualizations / charts (7 files)

**Files:**
`visualizations/SankeyChart.vue`, `visualizations/SessionDagChart.vue`,
`visualizations/CoOccurrenceMatrix.vue`, `visualizations/SpawnTreeChart.vue`,
`DependencyGraph.vue`, `CostHeatmap.vue`, `CostForecast.vue`

### Transform pattern

Charts have two error surfaces:

1. **Prop-passed `error`** (`error: string | null` defineProps) — data-fetch error from
   the parent view. Add a `watch` that calls `toast.error()` when the prop becomes
   non-null; remove `<div v-else-if="error">` from the template. Do NOT remove the
   prop itself — parent views may still pass it for other guards.

2. **Internal `renderError`** (e.g., SankeyChart's d3 layout failure) — replace
   `renderError.value = \`Could not lay out sankey: ...\`` with
   `toast.error(\`Could not lay out sankey: ...\`)` and delete the `renderError` ref and
   its template block.

**Before (SankeyChart.vue, representative):**

```typescript
const renderError = ref<string | null>(null)

// in render():
catch (err) {
  svg.selectAll('*').remove()
  renderError.value = `Could not lay out sankey: ${errorMessage(err)}`
}
```

```html
<div v-else-if="error" class="text-sm text-danger-text p-4">{{ error }}</div>
<div v-if="renderError" class="text-sm text-danger-text p-4">{{ renderError }}</div>
```

**After (SankeyChart.vue):**

```typescript
import { toast } from '../../composables/useToast'
import { watch } from 'vue'

// Remove: renderError ref
watch(() => props.error, (msg) => { if (msg) toast.error(msg) })

// in render():
catch (err) {
  svg.selectAll('*').remove()
  toast.error(`Could not lay out sankey: ${errorMessage(err)}`)
}
```

```html
<!-- Remove both v-else-if="error" and v-if="renderError" blocks -->
```

### Representative test assertion (SankeyChart)

```typescript
// src/components/__tests__/SankeyChart.test.ts
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

let SankeyChart: any
let toastMod: typeof import('../../composables/useToast')

beforeEach(async () => {
  vi.resetModules()
  toastMod = await import('../../composables/useToast')
  vi.spyOn(toastMod.toast, 'error')
  SankeyChart = (await import('../visualizations/SankeyChart.vue')).default
})

it('calls toast.error when error prop is set', async () => {
  const w = mount(SankeyChart, { props: { data: null, loading: false, error: 'fetch failed' } })
  await nextTick()
  expect(toastMod.toast.error).toHaveBeenCalledWith('fetch failed')
  expect(w.find('.text-danger-text').exists()).toBe(false)
})
```

**Commit:** `feat: migrate chart components to toast.error, remove inline error blocks`
`git commit --no-gpg-sign`

---

## Batch 6 — Forms (3 files)

**Files:** `BacklogForm.vue`, `QuickCreateProjectPanel.vue`, `SpawnDialog.vue`

### Transform pattern

Forms keep local `errorMsg` refs for synchronous validation feedback (required fields,
missing project folder). Per the product decision, even these move to toasts.

**Before (BacklogForm.vue, representative):**

```typescript
const errorMsg = ref('')

function buildTask() {
  if (!t || !s) {
    errorMsg.value = 'Title and slug are required, and a project must be selected.'
    return null
  }
  ...
  catch (err) {
    errorMsg.value = errorMessage(err, 'Failed to create task')
  }
}
```

```html
<p v-if="errorMsg" class="text-xs text-danger-text">{{ errorMsg }}</p>
```

**After (BacklogForm.vue):**

```typescript
import { toast } from '../composables/useToast'
// Remove: errorMsg ref

function buildTask() {
  if (!t || !s) {
    toast.error('Title and slug are required, and a project must be selected.')
    return null
  }
  ...
  catch (err) {
    toast.error(errorMessage(err, 'Failed to create task'))
  }
}
```

```html
<!-- Remove the <p v-if="errorMsg"> block -->
```

Note: `isSubmitting` and other loading/disabled state refs are NOT removed — the
directive explicitly preserves those; only the error display moves to toast.

### Representative test assertion (BacklogForm)

```typescript
// src/components/__tests__/BacklogForm.test.ts
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

let BacklogForm: any
let toastMod: typeof import('../../composables/useToast')

beforeEach(async () => {
  vi.resetModules()
  toastMod = await import('../../composables/useToast')
  vi.spyOn(toastMod.toast, 'error')
  globalThis.fetch = vi.fn().mockResolvedValue({
    ok: true,
    json: async () => ([]),
  })
  BacklogForm = (await import('../BacklogForm.vue')).default
})

it('calls toast.error for validation failure, no inline danger text', async () => {
  const w = mount(BacklogForm)
  // Submit without required fields
  await (w.vm as any).buildTask()
  await nextTick()
  expect(toastMod.toast.error).toHaveBeenCalledWith(
    expect.stringContaining('required'),
  )
  expect(w.find('.text-danger-text').exists()).toBe(false)
})
```

**Commit:** `feat: migrate form components to toast.error, remove inline error messages`
`git commit --no-gpg-sign`

---

## Batch 7 — Task components + misc (7 files)

**Files:**
`task/TaskOverviewTab.vue`, `task/TaskFooter.vue`, `task/CoordinationTab.vue`,
`RefinementChat.vue`, `PlanReviewPanel.vue`,
`SessionDetailModal.vue`, `AgentChatStream.vue`

### Transform pattern

Mixed — some hold one error ref, some (TaskOverviewTab) hold multiple
operation-specific refs (`autonomyPatchError`, `approveSpecError`, `assignError`).
Each maps to the same transform: call `toast.error()` at the catch site, remove the
inline conditional block.

**Before (TaskFooter.vue, representative):**

```typescript
const actionError = ref<string | null>(null)

async function triggerAction(...) {
  try { ... }
  catch (e) { actionError.value = errorMessage(e) }
}
```

```html
<p v-if="actionError" class="text-danger-text text-xs mb-2">{{ actionError }}</p>
```

**After (TaskFooter.vue):**

```typescript
import { toast } from '../../composables/useToast'
// Remove: actionError ref

async function triggerAction(...) {
  try { ... }
  catch (e) { toast.error(errorMessage(e)) }
}
```

```html
<!-- Remove the <p v-if="actionError"> block -->
```

`TaskOverviewTab.vue` has three independent error refs; apply the same transform to
each one individually.

`CoordinationTab.vue` receives `error` from `useTaskCoordination` — use the watch
approach from Batch 4.

`AgentChatStream.vue` has a `statusMsg`/`statusIsError` pair that colors the status
line (`:class="statusIsError ? 'text-danger-text' : '...'"`). This is a deliberate
status indicator, NOT an ephemeral error notification — keep it inline. Only the
`fetchError` full-block display migrates.

`SessionDetailModal.vue:369` has `fetchError` for a transcript load failure — migrate.
Its `statusMsg` status-color pair follows the same rule as AgentChatStream — keep
inline.

### Representative test assertion (CoordinationTab)

```typescript
// src/components/__tests__/CoordinationTab.test.ts
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

let CoordinationTab: any
let toastMod: typeof import('../../composables/useToast')

beforeEach(async () => {
  vi.resetModules()
  toastMod = await import('../../composables/useToast')
  vi.spyOn(toastMod.toast, 'error')
  globalThis.fetch = vi.fn().mockResolvedValue({ ok: false, status: 503, json: async () => ({}) })
  CoordinationTab = (await import('../task/CoordinationTab.vue')).default
})

it('calls toast.error when coordination data fails to load', async () => {
  const w = mount(CoordinationTab, { props: { task: { id: 't1' } } })
  await nextTick()
  expect(toastMod.toast.error).toHaveBeenCalled()
  expect(w.find('.text-danger-text').exists()).toBe(false)
})
```

**Commit:** `feat: migrate task/misc components to toast.error, remove inline error blocks`
`git commit --no-gpg-sign`

---

## Final verification

### Commands (run in order, all must be green before done)

```bash
pnpm i                  # required in the worktree — node_modules absent
pnpm test               # Vitest full suite — touches every migrated composable/component
pnpm typecheck          # tsc --noEmit
pnpm lint               # antfu ESLint; SDK-generated file is also linted
```

### Grep confirmation (must return 0 matches in migrated files)

```bash
grep -rn "text-danger-text\|text-red-600\|text-red-400" \
  src/components/AppSettings.vue \
  src/components/NotificationSettings.vue \
  src/components/PipelineConfigSettings.vue \
  src/components/ProjectSettings.vue \
  src/components/SpawnerSettings.vue \
  src/components/RemoteSettings.vue \
  src/components/ProviderSettings.vue \
  src/components/SystemPromptSettings.vue \
  src/components/ApiKeySettings.vue \
  src/components/PluginSettings.vue \
  src/components/PluginSettingsForm.vue \
  src/components/CostAnalyticsView.vue \
  src/components/SchedulesView.vue \
  src/components/EvalView.vue \
  src/components/AuditLogTab.vue \
  src/components/ExecutionWaterfall.vue \
  src/components/GitStatusPanel.vue \
  src/components/WorktreePanel.vue \
  src/components/visualizations/SankeyChart.vue \
  src/components/visualizations/SessionDagChart.vue \
  src/components/visualizations/CoOccurrenceMatrix.vue \
  src/components/visualizations/SpawnTreeChart.vue \
  src/components/DependencyGraph.vue \
  src/components/CostHeatmap.vue \
  src/components/CostForecast.vue \
  src/components/BacklogForm.vue \
  src/components/QuickCreateProjectPanel.vue \
  src/components/SpawnDialog.vue \
  src/components/RefinementChat.vue \
  src/components/PlanReviewPanel.vue \
  src/components/task/TaskOverviewTab.vue \
  src/components/task/TaskFooter.vue \
  src/components/task/CoordinationTab.vue \
  src/components/SessionDetailModal.vue \
  src/components/AgentChatStream.vue
```

Also confirm `ConfigExplorer.vue` has no `text-danger-text` outside the kept
conflict-reload block:

```bash
grep -n "text-danger-text" src/components/ConfigExplorer.vue
# Expected: 0 results (the conflict block uses different classes)
```

### antfu ESLint rules to respect

- No `export` inside `<script setup>` — use `defineOptions` for component name.
- `globalThis` not bare `global` in tests (memory from project lessons).
- When adding `<script setup>` imports, antfu will lint-error unused imports — remove
  any `error` refs that are de-structured but no longer consumed.
- `TransitionGroup` needs `tag` attribute to avoid vue/require-explicit-emits warnings.

---

## Intentionally kept inline (do not migrate)

| Component | Element | Reason |
|---|---|---|
| `App.vue` | `v-else-if="error"` (main SSE error, line 410) | Replaces entire content area; persistent blocking state; auto-dismiss would be wrong UX |
| `ui/AppInput.vue` | `<span class="text-danger-text">` with `:id="errorId"` | Field-level validation; aria-describedby requires it to be DOM-adjacent to the input |
| `LoginPage.vue` | `<p v-if="errorMessage" role="alert">` | OAuth redirect error surfaced on the auth page; toast auto-dismiss (5 s) too short for the user to act on a login failure |
| `StageOutputView.vue` | `text-danger-text` on stage error header | Structural log colorization, not an ephemeral error notification |
| `AgentChatStream.vue:257` | `statusIsError ? 'text-danger-text'` | Status-line color indicator; not an error message block |
| `SessionDetailModal.vue:257` | same `statusIsError` pair | Same reason |
| `ui/AppBadge.vue`, `ui/AppChip.vue` | `danger:` variant classes | Color token usage for status variants, unrelated to error messages |

---

## Summary

| Phase | Tasks | Files touched | New files |
|---|---|---|---|
| Foundation (Tasks 1–2) | 2 | App.vue + 4 emitters | useToast.ts, ToastHost.vue |
| Settings batch | 1 | 11 | — |
| Views batch | 1 | 8 | — |
| Charts batch | 1 | 7 | — |
| Forms batch | 1 | 3 | — |
| Task/misc batch | 1 | 7 | — |
| **Total** | **7** | **~40** | **2** |
