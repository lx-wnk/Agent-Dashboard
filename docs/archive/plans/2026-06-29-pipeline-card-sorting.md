# Plan: Pipeline kanban card sorting (3 behaviors)

## Goal

Fix three related issues in the pipeline kanban view: (1) manual drag-sort snaps back because SSE updates
rebuild the Sortable-bound list mid-drag, (2) drag is currently enabled on every column when it should only be
allowed in three, (3) non-sortable columns have no defined order and should auto-sort by last activity
descending so the most recently changed card appears at the top.

## Architecture

- `byActivityDesc` comparator lives in `useTasks.ts` next to `byRank`, exported so `PipelineBoard` can import it.
- `SortableTaskList` gains an `isDragging` ref (set in `onStart`, cleared first in `onEnd`) that guards the
  `watch(() => props.tasks, ...)` reset. A secondary guard skips the reset when the incoming ID order matches the
  current list (SSE field-update churn with no reorder). A new `sortable` prop (default `true`) passes
  `disabled: !sortable` to `useSortable` and threads through to `TaskCard`.
- `ColumnDef` gains `sortable: boolean`. `COLUMNS` marks `needs-you`, `concept`, `backlog` as `sortable: true`
  and all others as `false`. `tasksForColumn` applies `byActivityDesc` sort to non-sortable columns; sortable
  columns keep the existing rank order from `tasksByStageMap`.
- `TaskCard` gains a `sortable` prop (default `true`); the `.task-drag-handle` span uses `v-if` so it is
  absent from the DOM when `sortable` is false (prevents Sortable.js from finding a handle).

## Tech Stack

Vue 3 TS, vitest, @vueuse/integrations useSortable

---

## Task 1 — `byActivityDesc` comparator

**Files**

- `src/composables/useTasks.ts` — add + export `byActivityDesc` after line 33
- `src/composables/useTasks.test.ts` — new file

### Failing test

```typescript
// src/composables/useTasks.test.ts
import type { PipelineTask } from '../types'
import { describe, expect, it } from 'vitest'
import { byActivityDesc } from './useTasks'

function makeTask(id: string, updatedAt: string): PipelineTask {
  return {
    id,
    slug: `slug-${id}`,
    title: `Task ${id}`,
    description: null,
    cwd: '/repo',
    worktreePath: null,
    sourceBranch: null,
    targetBranch: null,
    currentStage: 'backlog',
    parentTaskId: null,
    maxIterations: 10,
    tokenBudget: null,
    costBudgetCents: null,
    stageTimeoutSeconds: 300,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt,
    metadata: null,
    silverBullet: false,
    planMode: false,
    priority: 'medium',
    userId: null,
    rank: null,
  }
}

describe('byActivityDesc', () => {
  it('places the task with the newer updatedAt first', () => {
    const older = makeTask('a', '2026-01-01T00:00:00Z')
    const newer = makeTask('b', '2026-06-01T00:00:00Z')
    expect([older, newer].sort(byActivityDesc).map(t => t.id)).toEqual(['b', 'a'])
  })

  it('returns 0 for equal timestamps', () => {
    const ts = '2026-06-01T12:00:00Z'
    expect(byActivityDesc(makeTask('x', ts), makeTask('y', ts))).toBe(0)
  })

  it('sorts three tasks newest-first', () => {
    const tasks = [
      makeTask('mid', '2026-03-01T00:00:00Z'),
      makeTask('newest', '2026-06-01T00:00:00Z'),
      makeTask('oldest', '2026-01-01T00:00:00Z'),
    ]
    expect([...tasks].sort(byActivityDesc).map(t => t.id)).toEqual(['newest', 'mid', 'oldest'])
  })
})
```

**Run-fail**

```
pnpm test src/composables/useTasks.test.ts
# Expected: error — byActivityDesc is not exported
```

### Minimal impl

Add after the `byRank` function (line 33 of `src/composables/useTasks.ts`):

```typescript
export function byActivityDesc(a: PipelineTask, b: PipelineTask): number {
  return new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime()
}
```

**Run-pass**

```
pnpm test src/composables/useTasks.test.ts
```

**Commit**

```
git commit --no-gpg-sign -m "feat: add byActivityDesc comparator to useTasks"
```

---

## Task 2 — Drag guard + `sortable` prop in SortableTaskList

**Files**

- `src/components/SortableTaskList.vue` — isDragging guard, sortable prop, disabled, onStart/onEnd
- `src/components/SortableTaskList.test.ts` — new file

### Failing tests

```typescript
// src/components/SortableTaskList.test.ts
import type { PipelineTask, Project } from '../types'
import { shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import SortableTaskList from './SortableTaskList.vue'
import TaskCard from './TaskCard.vue'

// Capture useSortable callbacks so tests can trigger them manually.
let capturedOnStart: (() => void) | undefined
let capturedOnEnd: ((evt: { oldIndex?: number, newIndex?: number }) => void) | undefined
let capturedOptions: Record<string, unknown> = {}

vi.mock('@vueuse/integrations/useSortable', () => ({
  useSortable: (_el: unknown, _list: unknown, opts: Record<string, unknown>) => {
    capturedOnStart = opts?.onStart as () => void
    capturedOnEnd = opts?.onEnd as (evt: { oldIndex?: number, newIndex?: number }) => void
    capturedOptions = { ...opts }
    return undefined
  },
}))

// SortableTaskList only imports reorderTask — mock the whole module to avoid SSE side-effects.
vi.mock('../composables/useTasks', () => ({
  reorderTask: vi.fn(),
}))

beforeEach(() => {
  capturedOnStart = undefined
  capturedOnEnd = undefined
  capturedOptions = {}
})

function makeTask(id: string, overrides: Partial<PipelineTask> = {}): PipelineTask {
  return {
    id,
    slug: `slug-${id}`,
    title: `Task ${id}`,
    description: null,
    cwd: '/repo',
    worktreePath: null,
    sourceBranch: null,
    targetBranch: null,
    currentStage: 'backlog',
    parentTaskId: null,
    maxIterations: 10,
    tokenBudget: null,
    costBudgetCents: null,
    stageTimeoutSeconds: 300,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    metadata: null,
    silverBullet: false,
    planMode: false,
    priority: 'medium',
    userId: null,
    rank: null,
    ...overrides,
  }
}

const emptyProjectById = new Map<string, Project>()

describe('SortableTaskList — drag guard', () => {
  it('resets list normally when a prop update arrives outside a drag', async () => {
    const taskA = makeTask('a')
    const taskB = makeTask('b')
    const wrapper = shallowMount(SortableTaskList, {
      props: { tasks: [taskA, taskB], projectById: emptyProjectById },
    })

    await wrapper.setProps({ tasks: [taskB, taskA] })
    await nextTick()

    const cards = wrapper.findAllComponents(TaskCard)
    expect(cards[0].props('task').id).toBe('b')
    expect(cards[1].props('task').id).toBe('a')

    wrapper.unmount()
  })

  it('does NOT reset list while a drag is in progress (onStart fired, onEnd not yet)', async () => {
    const taskA = makeTask('a')
    const taskB = makeTask('b')
    const wrapper = shallowMount(SortableTaskList, {
      props: { tasks: [taskA, taskB], projectById: emptyProjectById },
    })

    capturedOnStart?.()

    // SSE delivers new prop order during the drag
    await wrapper.setProps({ tasks: [taskB, taskA] })
    await nextTick()

    // Guard must have fired — list should still show original order
    const cards = wrapper.findAllComponents(TaskCard)
    expect(cards[0].props('task').id).toBe('a')
    expect(cards[1].props('task').id).toBe('b')

    wrapper.unmount()
  })

  it('resets list after drag ends (onEnd clears isDragging before returning)', async () => {
    const taskA = makeTask('a')
    const taskB = makeTask('b')
    const wrapper = shallowMount(SortableTaskList, {
      props: { tasks: [taskA, taskB], projectById: emptyProjectById },
    })

    capturedOnStart?.()
    // End with same position — still clears isDragging
    capturedOnEnd?.({ oldIndex: 0, newIndex: 0 })

    await wrapper.setProps({ tasks: [taskB, taskA] })
    await nextTick()

    const cards = wrapper.findAllComponents(TaskCard)
    expect(cards[0].props('task').id).toBe('b')
    expect(cards[1].props('task').id).toBe('a')

    wrapper.unmount()
  })

  it('skips reset when incoming order matches current (same IDs, same positions)', async () => {
    const taskA = makeTask('a')
    const taskB = makeTask('b')
    const wrapper = shallowMount(SortableTaskList, {
      props: { tasks: [taskA, taskB], projectById: emptyProjectById },
    })

    // New objects, same IDs, same order — simulates an SSE field-update with no rerank
    await wrapper.setProps({ tasks: [{ ...taskA, updatedAt: '2026-06-01T00:00:00Z' }, { ...taskB }] })
    await nextTick()

    const cards = wrapper.findAllComponents(TaskCard)
    expect(cards[0].props('task').id).toBe('a')
    expect(cards[1].props('task').id).toBe('b')

    wrapper.unmount()
  })
})

describe('SortableTaskList — sortable prop', () => {
  it('passes disabled:true to useSortable when sortable is false', () => {
    shallowMount(SortableTaskList, {
      props: { tasks: [], projectById: emptyProjectById, sortable: false },
    }).unmount()
    expect(capturedOptions.disabled).toBe(true)
  })

  it('passes disabled:false to useSortable when sortable is true (default)', () => {
    shallowMount(SortableTaskList, {
      props: { tasks: [], projectById: emptyProjectById },
    }).unmount()
    expect(capturedOptions.disabled).toBe(false)
  })

  it('forwards sortable prop to each TaskCard', () => {
    const wrapper = shallowMount(SortableTaskList, {
      props: { tasks: [makeTask('x')], projectById: emptyProjectById, sortable: false },
    })
    const card = wrapper.findAllComponents(TaskCard)[0]
    expect(card.props('sortable')).toBe(false)
    wrapper.unmount()
  })
})
```

**Run-fail**

```
pnpm test src/components/SortableTaskList.test.ts
# Expected: import errors (no sortable prop), watch fires during drag, disabled not passed
```

### Minimal impl

In `SortableTaskList.vue`:

1. Add `sortable?: boolean` to the `defineProps` block.
2. Add `const isDragging = ref(false)`.
3. Replace the bare `watch` with:
   ```typescript
   watch(() => props.tasks, (v) => {
     if (isDragging.value) return
     if (v.length === list.value.length && v.every((t, i) => t.id === list.value[i]?.id)) return
     list.value = [...v]
   })
   ```
4. Add `disabled: props.sortable === false` to the `useSortable` options object.
5. Add `onStart() { isDragging.value = true }` to the options.
6. Set `isDragging.value = false` as the **first statement** in `onEnd` (before the early-return guard).
7. Pass `:sortable="props.sortable !== false"` on every `<TaskCard>` in the template.

**Run-pass**

```
pnpm test src/components/SortableTaskList.test.ts
```

**Commit**

```
git commit --no-gpg-sign -m "fix: guard SortableTaskList watch against mid-drag SSE resets"
```

---

## Task 3 — TaskCard `sortable` prop → handle visibility

**Files**

- `src/components/TaskCard.vue` — add `sortable` prop, `v-if` on drag handle
- `src/components/TaskCard.test.ts` — new file

### Failing tests

```typescript
// src/components/TaskCard.test.ts
import type { PipelineTask } from '../types'
import { shallowMount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../composables/useAgentIdentity', () => ({
  useAgentIdentity: () => ({ getIdentity: () => null }),
}))
vi.mock('../composables/usePipelineConfig', () => ({
  usePipelineConfig: () => ({ maxAutoRetries: { value: 5 } }),
}))
vi.mock('../composables/useCopyId', () => ({
  useCopyId: () => ({ copy: vi.fn(), copied: { value: false } }),
  shortId: (id: string) => id.slice(0, 8),
}))

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, json: async () => ({}) }))
  vi.stubGlobal('EventSource', class {
    static CONNECTING = 0; static OPEN = 1; static CLOSED = 2
    onmessage: null = null; onerror: null = null; readyState = 0
    close() {}
  })
})
afterEach(() => vi.unstubAllGlobals())

function makeTask(overrides: Partial<PipelineTask> = {}): PipelineTask {
  return {
    id: 'task-1',
    slug: 'task-slug',
    title: 'Task Title',
    description: null,
    cwd: '/repo',
    worktreePath: null,
    sourceBranch: null,
    targetBranch: null,
    currentStage: 'backlog',
    parentTaskId: null,
    maxIterations: 10,
    tokenBudget: null,
    costBudgetCents: null,
    stageTimeoutSeconds: 300,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    metadata: null,
    silverBullet: false,
    planMode: false,
    priority: 'medium',
    userId: null,
    rank: null,
    ...overrides,
  }
}

import TaskCard from './TaskCard.vue'

describe('TaskCard drag handle', () => {
  it('renders the drag handle when sortable is not specified (default behaviour)', () => {
    const wrapper = shallowMount(TaskCard, { props: { task: makeTask() } })
    expect(wrapper.find('.task-drag-handle').exists()).toBe(true)
    wrapper.unmount()
  })

  it('renders the drag handle when sortable is explicitly true', () => {
    const wrapper = shallowMount(TaskCard, { props: { task: makeTask(), sortable: true } })
    expect(wrapper.find('.task-drag-handle').exists()).toBe(true)
    wrapper.unmount()
  })

  it('hides the drag handle when sortable is false', () => {
    const wrapper = shallowMount(TaskCard, { props: { task: makeTask(), sortable: false } })
    expect(wrapper.find('.task-drag-handle').exists()).toBe(false)
    wrapper.unmount()
  })
})
```

**Run-fail**

```
pnpm test src/components/TaskCard.test.ts
# Expected: no sortable prop, handle always renders → third test fails
```

### Minimal impl

In `TaskCard.vue`:

1. Add `sortable?: boolean` to `defineProps`.
2. Change the `<span class="task-drag-handle ...">` to use `v-if="props.sortable !== false"`.

Using `v-if` (not `v-show`) so the element is absent from the DOM — Sortable.js's `handle: '.task-drag-handle'` selector will find no handle when the span is absent, naturally preventing drag initiation.

**Run-pass**

```
pnpm test src/components/TaskCard.test.ts
```

**Commit**

```
git commit --no-gpg-sign -m "feat: hide drag handle in non-sortable kanban columns"
```

---

## Task 4 — ColumnDef.sortable + COLUMNS + thread through PipelineBoard

**Files**

- `src/components/PipelineBoard.vue` — ColumnDef interface, COLUMNS, SortableTaskList binding
- `src/components/PipelineBoard.test.ts` — new file

### Failing tests

```typescript
// src/components/PipelineBoard.test.ts
import type { PipelineStage, PipelineTask } from '../types'
import { shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import PipelineBoard from './PipelineBoard.vue'
import SortableTaskList from './SortableTaskList.vue'

// Mutable state shared between vi.mock factory and test bodies.
// The factory's useTasks() function closes over this variable and reads
// its current value each time it is called (at component mount time),
// so setting it before shallowMount works correctly.
let mockStageMap: Partial<Record<PipelineStage, PipelineTask[]>> = {}

vi.mock('../composables/useTasks', () => ({
  useTasks: () => ({
    tasks: { value: [] as PipelineTask[] },
    tasksByStageMap: {
      // Plain object — not reactive. Tests rely on initial render only.
      get value() { return mockStageMap },
    },
    isLoading: ref(false),
    error: ref(null),
    selectTask: vi.fn(),
    startStream: vi.fn(),
  }),
  reorderTask: vi.fn(),
  byActivityDesc: (a: PipelineTask, b: PipelineTask) =>
    new Date(b.updatedAt).getTime() - new Date(a.updatedAt).getTime(),
}))

vi.mock('../composables/useProjects', () => ({
  useProjects: () => ({ projects: ref([]) }),
}))

// Prevent SortableTaskList (shallow stub) from calling real useSortable
vi.mock('@vueuse/integrations/useSortable', () => ({
  useSortable: vi.fn(),
}))

beforeEach(() => {
  mockStageMap = {}
})

// COLUMNS order (from PipelineBoard.vue):
// 0 needs-you, 1 concept, 2 backlog, 3 plan_review, 4 implementation,
// 5 finalization, 6 done, 7 cancelled
describe('PipelineBoard — sortable prop threading', () => {
  it('passes sortable=true to needs-you, concept, and backlog SortableTaskLists', () => {
    const wrapper = shallowMount(PipelineBoard)
    const lists = wrapper.findAllComponents(SortableTaskList)
    expect(lists[0].props('sortable')).toBe(true) // needs-you
    expect(lists[1].props('sortable')).toBe(true) // concept
    expect(lists[2].props('sortable')).toBe(true) // backlog
    wrapper.unmount()
  })

  it('passes sortable=false to plan_review, implementation, finalization, done, cancelled', () => {
    const wrapper = shallowMount(PipelineBoard)
    const lists = wrapper.findAllComponents(SortableTaskList)
    expect(lists[3].props('sortable')).toBe(false) // plan_review
    expect(lists[4].props('sortable')).toBe(false) // implementation
    expect(lists[5].props('sortable')).toBe(false) // finalization
    expect(lists[6].props('sortable')).toBe(false) // done
    expect(lists[7].props('sortable')).toBe(false) // cancelled
    wrapper.unmount()
  })
})
```

**Run-fail**

```
pnpm test src/components/PipelineBoard.test.ts
# Expected: ColumnDef has no sortable field, SortableTaskList receives no sortable prop
```

### Minimal impl

In `PipelineBoard.vue`:

1. Add `sortable: boolean` to the `ColumnDef` interface.
2. Set `sortable: true` on `needs-you`, `concept`, `backlog` entries in `COLUMNS`; `sortable: false` on all others.
3. Add `:sortable="col.sortable"` to the `<SortableTaskList>` binding around line 336.

**Run-pass**

```
pnpm test src/components/PipelineBoard.test.ts
```

**Commit**

```
git commit --no-gpg-sign -m "feat: restrict manual drag-sort to needs-you, concept, and backlog columns"
```

---

## Task 5 — Auto-sort non-sortable columns by last activity

**Files**

- `src/components/PipelineBoard.vue` — import `byActivityDesc`, update `tasksForColumn`
- `src/components/PipelineBoard.test.ts` — add ordering tests

### Failing tests

Add to `src/components/PipelineBoard.test.ts`:

```typescript
function makeTask(id: string, overrides: Partial<PipelineTask> = {}): PipelineTask {
  return {
    id,
    slug: `slug-${id}`,
    title: `Task ${id}`,
    description: null,
    cwd: '/repo',
    worktreePath: null,
    sourceBranch: null,
    targetBranch: null,
    currentStage: 'backlog',
    parentTaskId: null,
    maxIterations: 10,
    tokenBudget: null,
    costBudgetCents: null,
    stageTimeoutSeconds: 300,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
    metadata: null,
    silverBullet: false,
    planMode: false,
    priority: 'medium',
    userId: null,
    rank: null,
    needsUser: false,
    ...overrides,
  }
}

describe('PipelineBoard — column task ordering', () => {
  it('sorts non-sortable column tasks by updatedAt descending', () => {
    const older = makeTask('older', { currentStage: 'implementation', updatedAt: '2026-01-01T00:00:00Z' })
    const newer = makeTask('newer', { currentStage: 'implementation', updatedAt: '2026-06-01T00:00:00Z' })
    // tasksByStageMap delivers them in rank order (both rank=null → creation order)
    mockStageMap = { implementation: [older, newer] }

    const wrapper = shallowMount(PipelineBoard)
    // implementation column is at index 4
    const tasks = wrapper.findAllComponents(SortableTaskList)[4].props('tasks') as PipelineTask[]
    expect(tasks[0].id).toBe('newer')
    expect(tasks[1].id).toBe('older')
    wrapper.unmount()
  })

  it('preserves rank order for sortable columns (does not re-sort by activity)', () => {
    // rank-first has earlier updatedAt but lower rank → should stay first in backlog
    const rankFirst = makeTask('rank-first', {
      currentStage: 'backlog',
      rank: 100,
      updatedAt: '2026-01-01T00:00:00Z',
    })
    const rankSecond = makeTask('rank-second', {
      currentStage: 'backlog',
      rank: 200,
      updatedAt: '2026-06-01T00:00:00Z',
    })
    // tasksByStageMap delivers them already rank-sorted
    mockStageMap = { backlog: [rankFirst, rankSecond] }

    const wrapper = shallowMount(PipelineBoard)
    // backlog column is at index 2
    const tasks = wrapper.findAllComponents(SortableTaskList)[2].props('tasks') as PipelineTask[]
    expect(tasks[0].id).toBe('rank-first')
    expect(tasks[1].id).toBe('rank-second')
    wrapper.unmount()
  })

  it('sorts implementation column tasks with three tasks newest-first', () => {
    const t1 = makeTask('t1', { currentStage: 'implementation', updatedAt: '2026-03-01T00:00:00Z' })
    const t2 = makeTask('t2', { currentStage: 'implementation', updatedAt: '2026-06-01T00:00:00Z' })
    const t3 = makeTask('t3', { currentStage: 'implementation', updatedAt: '2026-01-01T00:00:00Z' })
    mockStageMap = { implementation: [t1, t2, t3] }

    const wrapper = shallowMount(PipelineBoard)
    const tasks = wrapper.findAllComponents(SortableTaskList)[4].props('tasks') as PipelineTask[]
    expect(tasks.map((t: PipelineTask) => t.id)).toEqual(['t2', 't1', 't3'])
    wrapper.unmount()
  })
})
```

**Run-fail**

```
pnpm test src/components/PipelineBoard.test.ts
# Expected: non-sortable tasks delivered in rank order (not activity-desc) → ordering tests fail
```

### Minimal impl

In `PipelineBoard.vue`:

1. Add `byActivityDesc` to the import from `../composables/useTasks`.
2. In `tasksForColumn`, change the final `return all` (for non-needs-you columns) to:
   ```typescript
   return col.sortable ? all : [...all].sort(byActivityDesc)
   ```
   The spread creates a new array so the original `tasksByStageMap`-sourced array is not mutated.

**Run-pass**

```
pnpm test src/components/PipelineBoard.test.ts
```

**Commit**

```
git commit --no-gpg-sign -m "feat: auto-sort non-sortable kanban columns by last activity"
```

---

## What is unit-tested vs visually verified

| Behavior | Tested as |
|---|---|
| `byActivityDesc` comparator correctness | Unit test on exported function |
| `isDragging` guard: watch skips reset mid-drag | Observable: rendered card order unchanged after `onStart` + prop update |
| `isDragging` guard: watch resets after `onEnd` | Observable: rendered card order updates after `onEnd` + prop update |
| Same-order guard: no churn on SSE field-updates | Observable: rendered card order unchanged when IDs match |
| `sortable` prop → `disabled` option forwarded | Direct assertion on captured `useSortable` options object |
| `sortable` prop → TaskCard forwarded | Stub prop assertion via `findAllComponents(TaskCard)` |
| Handle absent from DOM when sortable=false | `wrapper.find('.task-drag-handle').exists()` is false |
| Correct `sortable` value per column | Stub prop assertion via `findAllComponents(SortableTaskList)` |
| Non-sortable columns ordered by updatedAt desc | Stub prop assertion: `tasks` prop order on `SortableTaskList` stubs |
| Sortable columns keep rank order | Same as above, rank order preserved |
| **Drag physically stays put on drop** | Visual verify in browser only — Sortable.js DOM reorder cannot be simulated in jsdom |

---

## Final verify

Run all touched tests, typecheck, and lint in one pass:

```
pnpm test src/composables/useTasks.test.ts src/components/SortableTaskList.test.ts src/components/TaskCard.test.ts src/components/PipelineBoard.test.ts
pnpm typecheck
pnpm lint src/composables/useTasks.ts src/components/SortableTaskList.vue src/components/TaskCard.vue src/components/PipelineBoard.vue
```

All three must be green before the branch is pushed.
