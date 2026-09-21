import type { NextThing } from '../composables/useNextThing'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'

const items = ref<NextThing[]>([
  { kind: 'permission', taskId: 't1', taskTitle: 'First', projectName: 'Dashboard', stage: 'implementation', title: 'First', why: 'w' },
  { kind: 'permission', taskId: 't2', taskTitle: 'Second', projectName: 'Dashboard', stage: 'implementation', title: 'Second', why: 'w' },
  { kind: 'permission', taskId: 't3', taskTitle: 'Third', projectName: 'Dashboard', stage: 'implementation', title: 'Third', why: 'w' },
])

vi.mock('@/composables/usePendingPermissions', () => ({ usePendingPermissions: () => ({ items: ref([]), refresh: vi.fn() }) }))
vi.mock('@/features/agents', () => ({ useAgents: () => ({ agents: ref([]) }) }))
vi.mock('@/features/pipeline', () => ({ useTasks: () => ({ tasks: ref([]), refetch: vi.fn() }) }))
vi.mock('../composables/useNextThing', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../composables/useNextThing')>()
  return { ...actual, rankNextThings: () => items.value }
})
vi.mock('../components/NextThing.vue', () => ({
  default: { props: ['next'], template: '<p data-testid="stub-next">{{ next ? next.title : "calm" }}</p>' },
}))

const { default: NeedsYouQueue } = await import('../components/NeedsYouQueue.vue')

describe('needsYouQueue', () => {
  it('shows the first item and its position, and pages with the arrows', async () => {
    const w = mount(NeedsYouQueue, { props: { variant: 'docked' } })
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
    const docked = mount(NeedsYouQueue, { props: { variant: 'docked' } })
    expect(docked.get('[data-testid="stub-next"]').text()).toBe('calm')
    const strip = mount(NeedsYouQueue, { props: { variant: 'strip' } })
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
    const w = mount(NeedsYouQueue, { props: { variant: 'docked' } })
    await w.get('[data-testid="needs-you-next"]').trigger('click')
    await w.get('[data-testid="needs-you-next"]').trigger('click')
    items.value = items.value.slice(0, 1)
    await nextTick()
    expect(w.get('[data-testid="stub-next"]').text()).toBe('First')
    w.unmount()
  })
})
