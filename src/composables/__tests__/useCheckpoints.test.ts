import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick, ref } from 'vue'
import { useCheckpoints } from '../useCheckpoints'

const errorSpy = vi.fn()
vi.mock('../useToast', () => ({
  toast: { error: (msg: string) => errorSpy(msg) },
}))

/**
 * Mount a wrapper so the composable's onUnmounted/watch run with a lifecycle.
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
  const wrapper = mount(Wrapper)
  return { result, wrapper }
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.clearAllMocks()
})

describe('useCheckpoints', () => {
  it('toasts an error when the list fetch fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500 }))
    const taskId = ref<string | null>('task-1')
    const { result, wrapper } = withSetup(() => useCheckpoints(taskId))
    await nextTick()
    await nextTick()
    expect(errorSpy).toHaveBeenCalledTimes(1)
    expect(errorSpy.mock.calls[0][0]).toContain('Failed to load checkpoints')
    expect(result.error.value).toBeTruthy()
    wrapper.unmount()
  })

  it('toasts an error and clears reverting when revert fails', async () => {
    const fetchMock = vi.fn()
      // initial load on mount → succeeds
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => [] })
      // revert POST → fails
      .mockResolvedValueOnce({ ok: false, status: 409, json: async () => ({ error: 'no worktree' }) })
    vi.stubGlobal('fetch', fetchMock)

    const taskId = ref<string | null>('task-1')
    const { result, wrapper } = withSetup(() => useCheckpoints(taskId))
    await nextTick()
    errorSpy.mockClear()

    await result.revert('cp-1')
    expect(errorSpy).toHaveBeenCalledTimes(1)
    expect(errorSpy.mock.calls[0][0]).toContain('Revert failed')
    expect(result.reverting.value).toBe(false)
    wrapper.unmount()
  })

  it('sets reverting during the revert call and ignores re-entry', async () => {
    let resolveRevert!: (v: unknown) => void
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: true, status: 200, json: async () => [] })
      .mockImplementationOnce(() => new Promise((resolve) => { resolveRevert = resolve }))
    vi.stubGlobal('fetch', fetchMock)

    const taskId = ref<string | null>('task-1')
    const { result, wrapper } = withSetup(() => useCheckpoints(taskId))
    await nextTick()

    const p = result.revert('cp-1')
    expect(result.reverting.value).toBe(true)

    // Re-entry while in-flight must not fire a second POST.
    await result.revert('cp-1')
    expect(fetchMock).toHaveBeenCalledTimes(2)

    resolveRevert({ ok: true, status: 200, json: async () => [] })
    await p
    expect(result.reverting.value).toBe(false)
    wrapper.unmount()
  })
})
