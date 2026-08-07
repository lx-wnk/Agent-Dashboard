import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { isDesktopShell } from '../desktopShell'
import { initServiceWorker, SW_URL, unregisterServiceWorkers } from '../serviceWorker'

const register = vi.fn()
const getRegistrations = vi.fn()
const cachesDelete = vi.fn()
const cachesKeys = vi.fn()

function stubServiceWorker(): void {
  Object.defineProperty(navigator, 'serviceWorker', {
    configurable: true,
    value: { register, getRegistrations },
  })
  vi.stubGlobal('caches', { keys: cachesKeys, delete: cachesDelete })
}

function setSearch(search: string): void {
  Object.defineProperty(window, 'location', {
    configurable: true,
    value: { ...window.location, search },
  })
}

beforeEach(() => {
  vi.clearAllMocks()
  sessionStorage.clear()
  register.mockResolvedValue({})
  getRegistrations.mockResolvedValue([])
  cachesKeys.mockResolvedValue([])
  cachesDelete.mockResolvedValue(true)
  stubServiceWorker()
  setSearch('')
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('isDesktopShell', () => {
  it('reads the marker the shell bootstrap redirect appends', () => {
    expect(isDesktopShell('?shell=desktop')).toBe(true)
    expect(isDesktopShell('')).toBe(true) // remembered for the rest of the session
  })

  it('is false for a plain browser visit', () => {
    expect(isDesktopShell('')).toBe(false)
    expect(isDesktopShell('?foo=bar')).toBe(false)
  })
})

describe('initServiceWorker', () => {
  it('registers in the browser', async () => {
    vi.stubEnv('PROD', true)
    await initServiceWorker()
    expect(register).toHaveBeenCalledWith(SW_URL, { scope: '/' })
  })

  it('does not register in dev, where the worker would cache the dev build', async () => {
    vi.stubEnv('PROD', false)
    await initServiceWorker()
    expect(register).not.toHaveBeenCalled()
  })

  it('never registers inside the desktop shell', async () => {
    vi.stubEnv('PROD', true)
    setSearch('?shell=desktop')
    await initServiceWorker()
    expect(register).not.toHaveBeenCalled()
  })

  // Users who ran an earlier build already have a worker installed; skipping
  // registration alone would leave it serving the previous build's assets.
  it('evicts an already-installed worker and its precache in the desktop shell', async () => {
    vi.stubEnv('PROD', true)
    setSearch('?shell=desktop')
    const unregister = vi.fn().mockResolvedValue(true)
    getRegistrations.mockResolvedValue([{ unregister }])
    cachesKeys.mockResolvedValue(['workbox-precache-v2'])

    await initServiceWorker()

    expect(unregister).toHaveBeenCalledOnce()
    expect(cachesDelete).toHaveBeenCalledWith('workbox-precache-v2')
  })

  // A property that exists but holds undefined still passes `'serviceWorker' in
  // navigator`, so both entry points have to survive that shape.
  it('does nothing when the browser exposes no service worker container', async () => {
    vi.stubEnv('PROD', true)
    Object.defineProperty(navigator, 'serviceWorker', { configurable: true, value: undefined })

    await expect(initServiceWorker()).resolves.toBeUndefined()
    await expect(unregisterServiceWorkers()).resolves.toBe(0)
    expect(register).not.toHaveBeenCalled()
  })
})
