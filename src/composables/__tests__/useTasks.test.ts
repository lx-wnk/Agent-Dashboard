import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'

// Minimal EventSource stub to prevent actual network calls.
class MockEventSource {
  static instances: MockEventSource[] = []
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: ((e: Event) => void) | null = null
  readyState = 0

  constructor(public url: string) {
    MockEventSource.instances.push(this)
  }

  close() {
    this.readyState = 2
  }
}

// Provide a minimal localStorage stub (the composable reads/writes to it at module load).
const store: Record<string, string> = {}
globalThis.localStorage = {
  getItem: (k: string) => store[k] ?? null,
  setItem: (k: string, v: string) => { store[k] = v },
  removeItem: (k: string) => { delete store[k] },
  clear: () => { Object.keys(store).forEach(k => delete store[k]) },
  length: 0,
  key: () => null,
}

let useTasks: typeof import('../useTasks').useTasks

beforeEach(async () => {
  globalThis.localStorage.clear()
  MockEventSource.instances = []
  vi.stubGlobal('EventSource', MockEventSource)
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve([]),
  }))
  vi.resetModules()
  const mod = await import('../useTasks')
  useTasks = mod.useTasks
})

afterEach(() => {
  vi.unstubAllGlobals()
})

/**
 * Helper: mount a wrapper component to provide the Vue lifecycle context
 * that composables using onUnmounted require.
 */
function withSetup<T>(composable: () => T) {
  let result!: T
  const Wrapper = defineComponent({
    setup() {
      result = composable()
      return {}
    },
    template: '<div />',
  })
  const wrapper = mount(Wrapper, { attachTo: document.body })
  return { result, wrapper }
}

describe('useTasks', () => {
  it('initialises with empty task list', () => {
    const { result, wrapper } = withSetup(() => useTasks({ autoStart: false }))
    expect(result.tasks).toBeDefined()
    expect(Array.isArray(result.tasks.value)).toBe(true)
    wrapper.unmount()
  })

  it('exposes a loading ref that starts as true', () => {
    const { result, wrapper } = withSetup(() => useTasks({ autoStart: false }))
    expect(result.isLoading.value).toBe(true)
    wrapper.unmount()
  })

  it('exposes selectTask function that updates selectedTask', () => {
    const { result, wrapper } = withSetup(() => useTasks({ autoStart: false }))
    expect(typeof result.selectTask).toBe('function')
    // Set a non-null task first, then clear it to verify null assignment works.
    // Note: Vue wraps objects in a Proxy, so use toStrictEqual rather than toBe.
    const fakeTask = { id: 'test-id' } as any
    result.selectTask(fakeTask)
    expect(result.selectedTask.value).toStrictEqual(fakeTask)
    result.selectTask(null)
    expect(result.selectedTask.value).toBeNull()
    wrapper.unmount()
  })

  it('exposes tasksByStage function', () => {
    const { result, wrapper } = withSetup(() => useTasks({ autoStart: false }))
    expect(typeof result.tasksByStage).toBe('function')
    wrapper.unmount()
  })

  it('tasksByStageMap orders each stage by rank ascending', () => {
    const { result, wrapper } = withSetup(() => useTasks({ autoStart: false }))
    result.tasks.value = [
      { id: 'c', currentStage: 'backlog', rank: 300, createdAt: '2026-01-01T00:00:00Z' },
      { id: 'a', currentStage: 'backlog', rank: 100, createdAt: '2026-01-01T00:00:00Z' },
      { id: 'b', currentStage: 'backlog', rank: 200, createdAt: '2026-01-01T00:00:00Z' },
    ] as any
    expect(result.tasksByStageMap.value.backlog!.map(t => t.id)).toEqual(['a', 'b', 'c'])
    wrapper.unmount()
  })

  it('falls back to createdAt when rank is absent', () => {
    const { result, wrapper } = withSetup(() => useTasks({ autoStart: false }))
    result.tasks.value = [
      { id: 'new', currentStage: 'backlog', createdAt: '2026-02-01T00:00:00Z' },
      { id: 'old', currentStage: 'backlog', createdAt: '2026-01-01T00:00:00Z' },
    ] as any
    expect(result.tasksByStageMap.value.backlog!.map(t => t.id)).toEqual(['old', 'new'])
    wrapper.unmount()
  })

  it('reorderTask sets the midpoint rank optimistically, then reconciles with the server value', async () => {
    const mod = await import('../useTasks')
    const { result, wrapper } = withSetup(() => useTasks({ autoStart: false }))
    result.tasks.value = [
      { id: 'a', currentStage: 'backlog', rank: 1000, createdAt: '2026-01-01T00:00:00Z' },
      { id: 'm', currentStage: 'backlog', rank: 5000, createdAt: '2026-01-01T00:00:00Z' },
      { id: 'b', currentStage: 'backlog', rank: 3000, createdAt: '2026-01-01T00:00:00Z' },
    ] as any
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ id: 'm', rank: 2222 }),
    }))

    const pending = mod.reorderTask('m', 'a', 'b')
    // Synchronous optimistic update: midpoint of 1000 and 3000.
    expect(result.tasks.value.find(t => t.id === 'm')!.rank).toBe(2000)
    await pending
    // Reconciled with the server-authoritative rank.
    expect(result.tasks.value.find(t => t.id === 'm')!.rank).toBe(2222)
    wrapper.unmount()
  })

  it('reorderTask rolls back the rank when the server rejects', async () => {
    const mod = await import('../useTasks')
    const { result, wrapper } = withSetup(() => useTasks({ autoStart: false }))
    result.tasks.value = [
      { id: 'a', currentStage: 'backlog', rank: 1000, createdAt: '2026-01-01T00:00:00Z' },
      { id: 'm', currentStage: 'backlog', rank: 5000, createdAt: '2026-01-01T00:00:00Z' },
      { id: 'b', currentStage: 'backlog', rank: 3000, createdAt: '2026-01-01T00:00:00Z' },
    ] as any
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      json: () => Promise.resolve({ error: 'boom' }),
    }))

    await expect(mod.reorderTask('m', 'a', 'b')).rejects.toThrow('boom')
    expect(result.tasks.value.find(t => t.id === 'm')!.rank).toBe(5000)
    wrapper.unmount()
  })

  it('resolvePermissionRequest posts to the task-scoped route with the outcome', async () => {
    const mod = await import('../useTasks')
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({}) })
    vi.stubGlobal('fetch', fetchMock)

    await mod.resolvePermissionRequest('T1', 'R1', 'granted')

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/tasks/T1/permission-requests/R1/resolve')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({ outcome: 'granted' })
  })

  it('bulkResolvePermissionRequests sends the outcome and permissionIds', async () => {
    const mod = await import('../useTasks')
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ resolved: 2, errors: [] }) })
    vi.stubGlobal('fetch', fetchMock)

    await mod.bulkResolvePermissionRequests('T1', ['R1', 'R2'], 'granted')

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/permission-requests/bulk-resolve')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({ taskId: 'T1', outcome: 'granted', permissionIds: ['R1', 'R2'], remember: false })
  })

  it('bulkResolvePermissionRequests sends a denied outcome', async () => {
    const mod = await import('../useTasks')
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ resolved: 1, errors: [] }) })
    vi.stubGlobal('fetch', fetchMock)

    await mod.bulkResolvePermissionRequests('T1', ['R1'], 'denied')

    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ taskId: 'T1', outcome: 'denied', permissionIds: ['R1'], remember: false })
  })

  it('bulkResolvePermissionRequests sends remember: true when opted in', async () => {
    const mod = await import('../useTasks')
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, json: () => Promise.resolve({ resolved: 1, errors: [] }) })
    vi.stubGlobal('fetch', fetchMock)

    await mod.bulkResolvePermissionRequests('T1', ['R1'], 'granted', true)

    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ taskId: 'T1', outcome: 'granted', permissionIds: ['R1'], remember: true })
  })

  it('bulkResolvePermissionRequests returns the resolved/errors payload', async () => {
    const mod = await import('../useTasks')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ resolved: 1, errors: ['permission R2 not pending for task T1'] }),
    }))

    const res = await mod.bulkResolvePermissionRequests('T1', ['R1', 'R2'], 'granted')

    expect(res).toEqual({ resolved: 1, errors: ['permission R2 not pending for task T1'] })
  })

  it('resolvePermissionRequest throws the server error on a non-ok response', async () => {
    const mod = await import('../useTasks')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      json: () => Promise.resolve({ error: 'not found' }),
    }))

    await expect(mod.resolvePermissionRequest('T1', 'R1', 'granted')).rejects.toThrow('not found')
  })
})
