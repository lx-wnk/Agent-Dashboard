import { isDesktopShell } from './desktopShell'

/**
 * Service-worker lifecycle for the two hosts of this SPA.
 *
 * Browser: register, so the PWA keeps its offline precache and the
 * "A new version is available" prompt (see usePWA).
 *
 * Desktop shell: do not register, and evict whatever is already installed. The
 * shell serves this SPA from its own in-process server, so precaching buys no
 * offline capability there — it only pins the previous build's assets until
 * someone clicks Reload after every rebuild.
 */
export const SW_URL = '/sw.js'

// `'serviceWorker' in navigator` is true for a property that exists but holds
// undefined, so the container itself is what gets checked.
function swContainer(): ServiceWorkerContainer | undefined {
  return typeof navigator === 'undefined' ? undefined : navigator.serviceWorker
}

// Workbox names every cache it creates `workbox-*` or `<prefix>-precache-*`.
function isWorkboxCache(name: string): boolean {
  return name.startsWith('workbox-') || name.includes('-precache-') || name.includes('-runtime-')
}

export async function unregisterServiceWorkers(): Promise<number> {
  const container = swContainer()
  if (!container)
    return 0

  const registrations = await container.getRegistrations()
  await Promise.all(registrations.map(r => r.unregister()))

  // Unregistering leaves the precache behind; without this the next visit still
  // has a populated cache storage for a worker that will never run again. Only
  // the workbox-owned caches are dropped — CacheStorage is origin-scoped, so a
  // blanket delete would also take any cache a future feature adds here.
  if (typeof caches !== 'undefined') {
    const keys = await caches.keys()
    await Promise.all(keys.filter(isWorkboxCache).map(k => caches.delete(k)))
  }
  return registrations.length
}

export async function initServiceWorker(): Promise<void> {
  const container = swContainer()
  if (!container)
    return

  if (isDesktopShell()) {
    await unregisterServiceWorkers()
    return
  }

  // Dev serves an untransformed worker and has no precache manifest; registering
  // there would cache the dev build and defeat HMR.
  if (!import.meta.env.PROD)
    return

  try {
    await container.register(SW_URL, { scope: '/' })
  }
  catch {
    // Blocked (insecure origin, disabled by policy) — the app works without it.
  }
}
