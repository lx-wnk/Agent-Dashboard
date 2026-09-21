import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, ref } from 'vue'

const session = {
  pid: ref<number | null>(42),
  status: ref<'idle' | 'starting' | 'running' | 'error'>('running'),
}
const agents = ref([{ pid: 42, status: 'active', working: true, lastOutput: 'PR #467 has four red checks.' }])

vi.mock('../composables/useKontorSession', () => ({ useKontorSession: () => session }))
vi.mock('@/features/agents', () => ({ useAgents: () => ({ agents }) }))
vi.mock('../components/KontorTile.vue', () => ({ default: { template: '<div data-testid="stub-kontor-tile" />' } }))

const { default: KontorWidget } = await import('../components/KontorWidget.vue')

beforeEach(() => {
  session.pid.value = 42
  session.status.value = 'running'
  agents.value = [{ pid: 42, status: 'active', working: true, lastOutput: 'PR #467 has four red checks.' }]
})

describe('kontorWidget', () => {
  it('shows state and the last output collapsed', () => {
    const w = mount(KontorWidget, { attachTo: document.body })
    expect(w.get('[data-testid="kontor-collapsed-state"]').text()).toContain('working')
    expect(w.get('[data-testid="kontor-collapsed-last"]').text()).toBe('PR #467 has four red checks.')
    expect(document.querySelector('[data-testid="kontor-expanded"]')).toBeNull()
    w.unmount()
  })

  it('grows on click and on "/", and collapses on Escape', async () => {
    const w = mount(KontorWidget, { attachTo: document.body })
    await w.get('[data-testid="kontor-collapsed"]').trigger('click')
    expect(document.querySelector('[data-testid="kontor-expanded"] [data-testid="stub-kontor-tile"]')).not.toBeNull()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(document.querySelector('[data-testid="kontor-expanded"]')).toBeNull()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: '/' }))
    await nextTick()
    expect(document.querySelector('[data-testid="kontor-expanded"]')).not.toBeNull()
    w.unmount()
  })

  // "/" typed into a field is text, not a shortcut.
  it('ignores "/" typed into an input', async () => {
    const w = mount(KontorWidget, { attachTo: document.body })
    const input = document.createElement('input')
    document.body.appendChild(input)
    input.dispatchEvent(new KeyboardEvent('keydown', { key: '/', bubbles: true }))
    await nextTick()
    expect(document.querySelector('[data-testid="kontor-expanded"]')).toBeNull()
    input.remove()
    w.unmount()
  })
})
