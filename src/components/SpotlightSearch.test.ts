import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import SpotlightSearch from './SpotlightSearch.vue'

const createTask = vi.fn()
const suggestFolders = vi.fn()
const projects = ref<Array<{ id: string, name: string }>>([])
const activeView = ref('dashboard')
let searchBody: unknown = { tasks: [], agents: [] }

vi.mock('@/features/pipeline', () => ({
  createTask: (...args: unknown[]) => createTask(...args),
}))
vi.mock('@/composables/useProjectFolders', () => ({
  suggestFolders: (...args: unknown[]) => suggestFolders(...args),
}))
vi.mock('@/composables/useProjects', () => ({
  useProjects: () => ({ projects }),
}))
vi.mock('@/composables/useViewState', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/composables/useViewState')>()
  return { ...actual, useViewState: () => ({ activeView }) }
})

const mockFetch = vi.fn(async () => ({
  ok: true,
  json: async () => searchBody,
}))

// The query watcher debounces by 200 ms before it searches, so a test that
// types has to let that elapse or it asserts against the pre-search state.
const DEBOUNCE_MS = 200

async function openSpotlight(text: string) {
  const wrapper = mount(SpotlightSearch, { attachTo: document.body })
  window.dispatchEvent(new KeyboardEvent('keydown', { key: 'k', metaKey: true }))
  await flushPromises()
  const input = document.querySelector<HTMLInputElement>('input[placeholder]')!
  input.value = text
  input.dispatchEvent(new Event('input'))
  await new Promise(resolve => setTimeout(resolve, DEBOUNCE_MS + 50))
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.stubGlobal('fetch', mockFetch)
  createTask.mockReset()
  suggestFolders.mockReset()
  searchBody = { tasks: [], agents: [] }
  projects.value = [{ id: 'p1', name: 'Dashboard' }]
  suggestFolders.mockResolvedValue([{ path: '/repo', isDefault: true }])
  createTask.mockResolvedValue({ id: 't1' })
  activeView.value = 'dashboard'
})

afterEach(() => {
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
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

describe('spotlightSearch commands and capture', () => {
  it('offers a navigation command for a matching view and runs it', async () => {
    const wrapper = await openSpotlight('pipeline')
    const option = document.querySelector('[data-testid="spotlight-command-view:pipeline"]')
    expect(option).not.toBeNull()
    ;(option as HTMLElement).click()
    await flushPromises()
    expect(activeView.value).toBe('pipeline')
    wrapper.unmount()
  })

  // One line, no project picked, no slug typed: the capture the origin
  // document asked for. If this ever passes quietly, the field lost its point.
  it('captures free text that matched nothing as a backlog task', async () => {
    const wrapper = await openSpotlight('teach the field to capture')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await flushPromises()
    expect(createTask).toHaveBeenCalledWith(expect.objectContaining({
      title: 'teach the field to capture',
      cwd: '/repo',
      projectId: 'p1',
    }))
    expect(createTask.mock.calls[0][0].slug).toMatch(/^[a-z0-9-]+$/)
    expect(wrapper.emitted('captured')).toEqual([['t1']])
    wrapper.unmount()
  })

  it('says so instead of failing silently when no project exists', async () => {
    projects.value = []
    const wrapper = await openSpotlight('something')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await flushPromises()
    expect(createTask).not.toHaveBeenCalled()
    expect(document.querySelector('[data-testid="spotlight-problem"]')?.textContent)
      .toContain('No project exists yet')
    wrapper.unmount()
  })

  // A search hit and a capture are mutually exclusive: text that found
  // something must never also be swallowed as a new backlog item.
  it('does not capture when the search found a result', async () => {
    searchBody = { tasks: [{ id: 'x1', title: 'existing', currentStage: 'ready' }], agents: [] }
    const wrapper = await openSpotlight('existing')
    expect(document.querySelector('[data-testid="spotlight-capture"]')).toBeNull()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await flushPromises()
    expect(createTask).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
