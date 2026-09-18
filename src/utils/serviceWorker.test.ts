import { afterEach, describe, expect, it, vi } from 'vitest'
import { refreshServiceWorker } from './serviceWorker'
import { SW_MSG_SKIP_WAITING } from './swConstants'

function stubContainer(registrations: unknown[]) {
  vi.stubGlobal('navigator', {
    serviceWorker: { getRegistrations: vi.fn().mockResolvedValue(registrations) },
  })
}

afterEach(() => vi.unstubAllGlobals())

describe('refreshServiceWorker', () => {
  // Without the update() the browser never fetches the new sw.js, nothing
  // enters "waiting", and the reload after a rebuild is served from the old
  // precache — the window shows the previous build against a server that
  // 404s its bundle.
  it('fetches a new worker and hands control to the one that is waiting', async () => {
    const postMessage = vi.fn()
    const update = vi.fn().mockResolvedValue(undefined)
    stubContainer([{ update, waiting: { postMessage } }])

    await refreshServiceWorker()

    expect(update).toHaveBeenCalled()
    expect(postMessage).toHaveBeenCalledWith({ type: SW_MSG_SKIP_WAITING })
  })

  it('updates without posting when no worker is waiting', async () => {
    const update = vi.fn().mockResolvedValue(undefined)
    stubContainer([{ update, waiting: null }])

    await expect(refreshServiceWorker()).resolves.toBeUndefined()
    expect(update).toHaveBeenCalled()
  })

  // The caller reloads right after this. A rejected update must not become an
  // unhandled rejection that strands the page on the old build.
  it('swallows a failing update', async () => {
    stubContainer([{ update: vi.fn().mockRejectedValue(new Error('blocked')), waiting: null }])
    await expect(refreshServiceWorker()).resolves.toBeUndefined()
  })

  it('does nothing when the browser has no service worker container', async () => {
    vi.stubGlobal('navigator', {})
    await expect(refreshServiceWorker()).resolves.toBeUndefined()
  })
})
