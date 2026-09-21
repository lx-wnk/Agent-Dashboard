import type { NextThing } from '../composables/useNextThing'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { OPEN_TASK, PENDING_PERMISSIONS } from '@/composables/openTask'

const items = ref<NextThing[]>([
  { kind: 'permission', taskId: 't1', taskTitle: 'First', projectName: 'Dashboard', stage: 'implementation', title: 'First', why: 'w' },
  { kind: 'permission', taskId: 't2', taskTitle: 'Second', projectName: 'Dashboard', stage: 'implementation', title: 'Second', why: 'w' },
  { kind: 'permission', taskId: 't3', taskTitle: 'Third', projectName: 'Dashboard', stage: 'implementation', title: 'Third', why: 'w' },
])

vi.mock('@/features/agents', () => ({ useAgents: () => ({ agents: ref([]) }) }))
vi.mock('@/features/pipeline', () => ({ useTasks: () => ({ tasks: ref([]), refetch: vi.fn() }) }))
vi.mock('../composables/useNextThing', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../composables/useNextThing')>()
  return { ...actual, rankNextThings: () => items.value }
})
vi.mock('../components/NextThing.vue', () => ({
  default: {
    props: ['next'],
    emits: ['open', 'resolved'],
    template: '<div>'
      + '<p data-testid="stub-next">{{ next ? next.title : "calm" }}</p>'
      + '<button type="button" data-testid="stub-open" @click="$emit(\'open\', \'t1\')">open</button>'
      + '<button type="button" data-testid="stub-resolved" @click="$emit(\'resolved\')">resolved</button>'
      + '</div>',
  },
}))

const { default: NeedsYouQueue } = await import('../components/NeedsYouQueue.vue')

// App.vue is the one owner of usePendingPermissions(tasks) (SSOT) — every
// mount here provides a fresh stand-in instead of letting the component reach
// for a second, out-of-sync cache.
function mountQueue(variant: 'docked' | 'strip', overrides: { openTask?: (taskId: string) => void, refresh?: () => void } = {}) {
  return mount(NeedsYouQueue, {
    props: { variant },
    global: {
      provide: {
        [PENDING_PERMISSIONS]: { items: ref([]), refresh: overrides.refresh ?? vi.fn() },
        [OPEN_TASK]: overrides.openTask ?? vi.fn(),
      },
    },
  })
}

describe('needsYouQueue', () => {
  it('shows the first item and its position, and pages with the arrows', async () => {
    const w = mountQueue('docked')
    expect(w.get('[data-testid="stub-next"]').text()).toBe('First')
    expect(w.get('[data-testid="needs-you-position"]').text()).toBe('1 of 3')
    await w.get('[data-testid="needs-you-next"]').trigger('click')
    expect(w.get('[data-testid="stub-next"]').text()).toBe('Second')
    await w.get('[data-testid="needs-you-prev"]').trigger('click')
    await w.get('[data-testid="needs-you-prev"]').trigger('click')
    expect(w.get('[data-testid="stub-next"]').text()).toBe('Third')
    w.unmount()
  })

  // Calm when empty: docked it says so in one line; a strip on another page
  // would be noise on every screen, so it renders nothing.
  it('is one calm line docked and nothing as a strip when empty', async () => {
    items.value = []
    const docked = mountQueue('docked')
    expect(docked.get('[data-testid="stub-next"]').text()).toBe('calm')
    const strip = mountQueue('strip')
    expect(strip.find('[data-testid="needs-you"]').exists()).toBe(false)
    docked.unmount()
    strip.unmount()
  })

  it('keeps a valid position when the list shrinks', async () => {
    items.value = [
      { kind: 'permission', taskId: 't1', taskTitle: 'First', projectName: 'Dashboard', stage: 'implementation', title: 'First', why: 'w' },
      { kind: 'permission', taskId: 't2', taskTitle: 'Second', projectName: 'Dashboard', stage: 'implementation', title: 'Second', why: 'w' },
      { kind: 'permission', taskId: 't3', taskTitle: 'Third', projectName: 'Dashboard', stage: 'implementation', title: 'Third', why: 'w' },
    ]
    const w = mountQueue('docked')
    await w.get('[data-testid="needs-you-next"]').trigger('click')
    await w.get('[data-testid="needs-you-next"]').trigger('click')
    items.value = items.value.slice(0, 1)
    await nextTick()
    expect(w.get('[data-testid="stub-next"]').text()).toBe('First')
    w.unmount()
  })

  it('opens a task through the injected OPEN_TASK and refreshes the injected instance on resolved', async () => {
    items.value = [
      { kind: 'permission', taskId: 't1', taskTitle: 'First', projectName: 'Dashboard', stage: 'implementation', title: 'First', why: 'w' },
    ]
    const openTask = vi.fn()
    const refresh = vi.fn()
    const w = mountQueue('docked', { openTask, refresh })
    await w.get('[data-testid="stub-open"]').trigger('click')
    expect(openTask).toHaveBeenCalledWith('t1')
    await w.get('[data-testid="stub-resolved"]').trigger('click')
    expect(refresh).toHaveBeenCalledOnce()
    w.unmount()
  })
})
