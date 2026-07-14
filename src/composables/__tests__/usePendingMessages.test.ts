import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { SW_MSG_MESSAGES_REPLAYED } from '@/utils/swConstants'

const countPendingMock = vi.fn()
const replayPendingMock = vi.fn()

vi.mock('@/utils/pendingMessages', () => ({
  countPending: countPendingMock,
  replayPending: replayPendingMock,
}))

let usePendingMessagesMod: typeof import('../usePendingMessages')

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

beforeEach(async () => {
  vi.useFakeTimers()
  countPendingMock.mockReset().mockResolvedValue(0)
  replayPendingMock.mockReset().mockResolvedValue(undefined)
  vi.resetModules()
  usePendingMessagesMod = await import('../usePendingMessages')
})

afterEach(() => {
  vi.useRealTimers()
  Reflect.deleteProperty(navigator, 'serviceWorker')
})

describe('usePendingMessages', () => {
  it('refreshes the pending count on mount', async () => {
    countPendingMock.mockResolvedValue(3)
    const { result, wrapper } = withSetup(() => usePendingMessagesMod.usePendingMessages())
    await flushPromises()

    expect(result.pendingCount.value).toBe(3)
    wrapper.unmount()
  })

  it('leaves the count untouched when IndexedDB is unavailable', async () => {
    countPendingMock.mockRejectedValue(new Error('IDB unavailable'))
    const { result, wrapper } = withSetup(() => usePendingMessagesMod.usePendingMessages())
    await flushPromises()

    expect(result.pendingCount.value).toBe(0)
    wrapper.unmount()
  })

  it('polls every 10 seconds while mounted', async () => {
    countPendingMock.mockResolvedValue(1)
    const { wrapper } = withSetup(() => usePendingMessagesMod.usePendingMessages())
    await flushPromises()
    expect(countPendingMock).toHaveBeenCalledTimes(1)

    countPendingMock.mockResolvedValue(5)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(countPendingMock).toHaveBeenCalledTimes(2)

    wrapper.unmount()
  })

  it('stops polling on unmount', async () => {
    countPendingMock.mockResolvedValue(1)
    const { wrapper } = withSetup(() => usePendingMessagesMod.usePendingMessages())
    await flushPromises()
    wrapper.unmount()

    const callsAtUnmount = countPendingMock.mock.calls.length
    await vi.advanceTimersByTimeAsync(30_000)
    expect(countPendingMock).toHaveBeenCalledTimes(callsAtUnmount)
  })

  it('drainPendingMessages replays pending messages and refreshes the count', async () => {
    countPendingMock.mockResolvedValueOnce(2).mockResolvedValueOnce(0)
    const { result, wrapper } = withSetup(() => usePendingMessagesMod.usePendingMessages())
    await flushPromises()
    expect(result.pendingCount.value).toBe(2)

    await result.drainPendingMessages()

    expect(replayPendingMock).toHaveBeenCalledOnce()
    expect(result.pendingCount.value).toBe(0)
    wrapper.unmount()
  })

  it('drainPendingMessages swallows replay errors', async () => {
    replayPendingMock.mockRejectedValue(new Error('replay failed'))

    await expect(usePendingMessagesMod.drainPendingMessages()).resolves.toBeUndefined()
  })

  it('dispatches a drain-success event when the service worker reports replayed messages', async () => {
    const listeners: Record<string, (e: MessageEvent) => void> = {}
    Object.defineProperty(navigator, 'serviceWorker', {
      value: {
        addEventListener: (type: string, cb: (e: MessageEvent) => void) => { listeners[type] = cb },
        removeEventListener: vi.fn(),
      },
      configurable: true,
    })
    const onDrainSuccess = vi.fn()
    window.addEventListener('drain-success', onDrainSuccess)

    const { wrapper } = withSetup(() => usePendingMessagesMod.usePendingMessages())
    await flushPromises()

    listeners.message(new MessageEvent('message', { data: { type: SW_MSG_MESSAGES_REPLAYED, count: 2 } }))
    expect(onDrainSuccess).toHaveBeenCalledTimes(1)

    listeners.message(new MessageEvent('message', { data: { type: SW_MSG_MESSAGES_REPLAYED, count: 0 } }))
    expect(onDrainSuccess).toHaveBeenCalledTimes(1)

    window.removeEventListener('drain-success', onDrainSuccess)
    wrapper.unmount()
  })
})
