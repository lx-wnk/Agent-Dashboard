import type { WorktreeStatusDTO } from '@/sdk.generated'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick, ref } from 'vue'
import { useWorktreeStatus } from '../useWorktreeStatus'

function withSetup<T>(composable: () => T) {
  let result!: T
  const Wrapper = defineComponent({
    setup() {
      result = composable()
      return {}
    },
    template: '<div />',
  })
  const wrapper = mount(Wrapper)
  return { result, wrapper }
}

function makeStatus(overrides: Partial<WorktreeStatusDTO> = {}): WorktreeStatusDTO {
  return { branch: 'feat/x', ahead: 0, behind: 0, dirty: false, fileCount: 0, ...overrides }
}

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('useWorktreeStatus', () => {
  it('fetches immediately when active and a taskId are present', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve(makeStatus()) })
    vi.stubGlobal('fetch', fetchMock)
    const taskId = ref<string | null>('task-1')
    const active = ref(true)

    const { result, wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await flushPromises()

    expect(fetchMock).toHaveBeenCalledWith('/api/tasks/task-1/worktree')
    expect(result.status.value).toEqual(makeStatus())
    expect(result.isLoading.value).toBe(false)
    wrapper.unmount()
  })

  it('does not fetch when inactive', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const taskId = ref<string | null>('task-1')
    const active = ref(false)

    const { wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await flushPromises()

    expect(fetchMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('treats 204 as "no worktree"', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 204 }))
    const taskId = ref<string | null>('task-1')
    const active = ref(true)

    const { result, wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await flushPromises()

    expect(result.status.value).toBeNull()
    expect(result.error.value).toBeNull()
    wrapper.unmount()
  })

  it('sets an error on a non-ok response and clears status', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500 }))
    const taskId = ref<string | null>('task-1')
    const active = ref(true)

    const { result, wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await flushPromises()

    expect(result.error.value).toBe('HTTP 500')
    expect(result.status.value).toBeNull()
    wrapper.unmount()
  })

  it('drops a stale response when the taskId changes before it resolves', async () => {
    let resolveFirst!: (v: unknown) => void
    const fetchMock = vi.fn()
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve }))
      .mockResolvedValueOnce({ ok: true, status: 200, json: () => Promise.resolve(makeStatus({ branch: 'second' })) })
    vi.stubGlobal('fetch', fetchMock)
    const taskId = ref<string | null>('task-1')
    const active = ref(true)

    const { result, wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await nextTick()

    taskId.value = 'task-2'
    await flushPromises()
    expect(result.status.value).toEqual(makeStatus({ branch: 'second' }))

    resolveFirst({ ok: true, status: 200, json: () => Promise.resolve(makeStatus({ branch: 'first-stale' })) })
    await flushPromises()

    expect(result.status.value).toEqual(makeStatus({ branch: 'second' }))
    wrapper.unmount()
  })

  it('polls every 30s while active', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve(makeStatus()) })
    vi.stubGlobal('fetch', fetchMock)
    const taskId = ref<string | null>('task-1')
    const active = ref(true)

    const { wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(30_000)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('stops polling and clears status when active flips false', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve(makeStatus()) })
    vi.stubGlobal('fetch', fetchMock)
    const taskId = ref<string | null>('task-1')
    const active = ref(true)

    const { result, wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await flushPromises()
    expect(result.status.value).not.toBeNull()

    active.value = false
    await nextTick()
    expect(result.status.value).toBeNull()

    await vi.advanceTimersByTimeAsync(60_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('stops polling on unmount', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: () => Promise.resolve(makeStatus()) })
    vi.stubGlobal('fetch', fetchMock)
    const taskId = ref<string | null>('task-1')
    const active = ref(true)

    const { wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await flushPromises()
    wrapper.unmount()

    await vi.advanceTimersByTimeAsync(60_000)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('create() POSTs and refreshes status on success', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 204 }) // initial immediate fetch: no worktree yet
      .mockResolvedValueOnce({ ok: true, status: 200 }) // POST create
      .mockResolvedValueOnce({ ok: true, status: 200, json: () => Promise.resolve(makeStatus()) }) // refresh
    vi.stubGlobal('fetch', fetchMock)
    const taskId = ref<string | null>('task-1')
    const active = ref(true)

    const { result, wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await flushPromises()

    await result.create()

    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/tasks/task-1/worktree', { method: 'POST' })
    expect(result.status.value).toEqual(makeStatus())
    wrapper.unmount()
  })

  it('create() sets an error on failure without refreshing', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 204 })
      .mockResolvedValueOnce({ ok: false, status: 500 })
    vi.stubGlobal('fetch', fetchMock)
    const taskId = ref<string | null>('task-1')
    const active = ref(true)

    const { result, wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await flushPromises()

    await result.create()

    expect(result.error.value).toBe('Failed to create worktree (HTTP 500)')
    expect(fetchMock).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('remove() with force=false issues a plain DELETE and refreshes on 204', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 200, json: () => Promise.resolve(makeStatus()) })
      .mockResolvedValueOnce({ ok: true, status: 204 }) // DELETE
      .mockResolvedValueOnce({ ok: true, status: 204 }) // refresh -> no worktree
    vi.stubGlobal('fetch', fetchMock)
    const taskId = ref<string | null>('task-1')
    const active = ref(true)

    const { result, wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await flushPromises()

    const status = await result.remove(false)

    expect(status).toBe(204)
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/tasks/task-1/worktree', { method: 'DELETE' })
    expect(result.status.value).toBeNull()
    wrapper.unmount()
  })

  it('remove() with force=true appends the force query param', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 200, json: () => Promise.resolve(makeStatus()) })
      .mockResolvedValueOnce({ ok: true, status: 204 })
      .mockResolvedValueOnce({ ok: true, status: 204 })
    vi.stubGlobal('fetch', fetchMock)
    const taskId = ref<string | null>('task-1')
    const active = ref(true)

    const { result, wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await flushPromises()

    await result.remove(true)

    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/tasks/task-1/worktree?force=true', { method: 'DELETE' })
    wrapper.unmount()
  })

  it('remove() surfaces a 409 as a dirty-worktree error', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 200, json: () => Promise.resolve(makeStatus()) })
      .mockResolvedValueOnce({ ok: false, status: 409 })
    vi.stubGlobal('fetch', fetchMock)
    const taskId = ref<string | null>('task-1')
    const active = ref(true)

    const { result, wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await flushPromises()

    const status = await result.remove(false)

    expect(status).toBe(409)
    expect(result.error.value).toBe('Worktree has uncommitted changes')
    wrapper.unmount()
  })

  it('remove() returns 0 and sets an error message on a network failure', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 200, json: () => Promise.resolve(makeStatus()) })
      .mockRejectedValueOnce(new Error('offline'))
    vi.stubGlobal('fetch', fetchMock)
    const taskId = ref<string | null>('task-1')
    const active = ref(true)

    const { result, wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await flushPromises()

    const status = await result.remove(false)

    expect(status).toBe(0)
    expect(result.error.value).toBe('offline')
    wrapper.unmount()
  })

  it('create()/remove() no-op without a taskId', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    const taskId = ref<string | null>(null)
    const active = ref(true)

    const { result, wrapper } = withSetup(() => useWorktreeStatus(taskId, active))
    await result.create()
    const status = await result.remove(false)

    expect(fetchMock).not.toHaveBeenCalled()
    expect(status).toBe(0)
    wrapper.unmount()
  })
})
