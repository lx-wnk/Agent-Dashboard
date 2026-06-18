import { describe, expect, it, vi } from 'vitest'
import { SW_MSG_MESSAGES_REPLAYED, SW_MSG_SKIP_WAITING } from './utils/swConstants'

vi.mock('workbox-precaching', () => ({
  precacheAndRoute: vi.fn(),
  cleanupOutdatedCaches: vi.fn(),
}))

describe('service worker message handler', () => {
  it('calls skipWaiting only for a SKIP_WAITING message', async () => {
    const listeners: Record<string, (e: unknown) => void> = {}
    const skipWaiting = vi.fn()
    vi.stubGlobal('self', {
      __WB_MANIFEST: [],
      skipWaiting,
      clients: { matchAll: () => Promise.resolve([]) },
      addEventListener: (type: string, cb: (e: unknown) => void) => {
        listeners[type] = cb
      },
    })

    await import('./sw')

    expect(typeof listeners.message).toBe('function')

    listeners.message({ data: { type: SW_MSG_SKIP_WAITING } })
    expect(skipWaiting).toHaveBeenCalledTimes(1)

    skipWaiting.mockClear()
    listeners.message({ data: { type: SW_MSG_MESSAGES_REPLAYED } })
    expect(skipWaiting).not.toHaveBeenCalled()

    vi.unstubAllGlobals()
  })
})
