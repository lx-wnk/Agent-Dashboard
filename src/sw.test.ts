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

describe('push notification handling', () => {
  function stubSelf() {
    const listeners: Record<string, (e: unknown) => void> = {}
    const showNotification = vi.fn(() => Promise.resolve())
    const openWindow = vi.fn(() => Promise.resolve(null))
    const matchAll = vi.fn(() => Promise.resolve([]))
    vi.stubGlobal('self', {
      __WB_MANIFEST: [],
      skipWaiting: vi.fn(),
      registration: { showNotification },
      clients: { matchAll, openWindow },
      addEventListener: (type: string, cb: (e: unknown) => void) => {
        listeners[type] = cb
      },
    })
    return { listeners, showNotification, openWindow, matchAll }
  }

  it('shows a notification for a valid push payload', async () => {
    vi.resetModules()
    const { listeners, showNotification } = stubSelf()
    await import('./sw')

    const waited: Promise<unknown>[] = []
    listeners.push({
      data: { json: () => ({ title: 'Approval needed', body: 'x: Bash', url: '/', tag: 'permission-t1' }) },
      waitUntil: (p: Promise<unknown>) => { waited.push(p) },
    })
    await Promise.all(waited)

    expect(showNotification).toHaveBeenCalledWith('Approval needed', expect.objectContaining({ body: 'x: Bash', tag: 'permission-t1' }))

    vi.unstubAllGlobals()
  })

  it('shows no notification when the payload lacks a title', async () => {
    vi.resetModules()
    const { listeners, showNotification } = stubSelf()
    await import('./sw')

    const waited: Promise<unknown>[] = []
    listeners.push({
      data: { json: () => ({ body: 'x: Bash', url: '/', tag: 'permission-t1' }) },
      waitUntil: (p: Promise<unknown>) => { waited.push(p) },
    })
    await Promise.all(waited)

    expect(showNotification).not.toHaveBeenCalled()

    vi.unstubAllGlobals()
  })

  it('closes the notification and opens a window on click when none is open', async () => {
    vi.resetModules()
    const { listeners, openWindow } = stubSelf()
    await import('./sw')

    const waited: Promise<unknown>[] = []
    const close = vi.fn()
    listeners.notificationclick({
      notification: { close, data: { url: '/' } },
      waitUntil: (p: Promise<unknown>) => { waited.push(p) },
    })
    await Promise.all(waited)

    expect(close).toHaveBeenCalledTimes(1)
    expect(openWindow).toHaveBeenCalledWith('/')

    vi.unstubAllGlobals()
  })
})
