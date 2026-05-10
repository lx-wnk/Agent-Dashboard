import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SpotlightSearch from './SpotlightSearch.vue'

const mockFetch = vi.fn().mockResolvedValue({
  ok: true,
  json: async () => ({ tasks: [], agents: [] }),
})

beforeEach(() => {
  vi.stubGlobal('fetch', mockFetch)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('spotlightSearch', () => {
  it('is hidden by default', () => {
    mount(SpotlightSearch)
    expect(document.querySelector('input[placeholder]')).toBeNull()
  })

  it('opens on Cmd+K', async () => {
    const wrapper = mount(SpotlightSearch, { attachTo: document.body })
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true }))
    await wrapper.vm.$nextTick()
    expect(document.querySelector('input[placeholder]')).not.toBeNull()
    wrapper.unmount()
  })

  it('closes on Escape', async () => {
    const wrapper = mount(SpotlightSearch, { attachTo: document.body })
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true }))
    await wrapper.vm.$nextTick()
    expect(document.querySelector('input[placeholder]')).not.toBeNull()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await wrapper.vm.$nextTick()
    expect(document.querySelector('input[placeholder]')).toBeNull()
    wrapper.unmount()
  })

  it('emits navigateTask on Enter when task selected', async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: async () => ({
        tasks: [{ id: 't1', title: 'Test Task', currentStage: 'implementation', slug: 'test-task', description: null, cwd: '/', worktreePath: null, sourceBranch: null, targetBranch: null, parentTaskId: null, maxIterations: 3, tokenBudget: null, costBudgetCents: null, stageTimeoutSeconds: 1800, createdAt: '', updatedAt: '', metadata: null, silverBullet: false, priority: 'medium', userId: null }],
        agents: [],
      }),
    })
    const wrapper = mount(SpotlightSearch, { attachTo: document.body })
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true }))
    await wrapper.vm.$nextTick()
    // The input lives inside a Teleport; set the reactive query directly on the vm
    const vm = wrapper.vm as unknown as { query: string }
    vm.query = 'test'
    await new Promise(resolve => setTimeout(resolve, 300))
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('navigateTask')).toBeTruthy()
    wrapper.unmount()
  })
})
