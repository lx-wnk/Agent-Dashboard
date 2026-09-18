import { isDesktopShell } from './desktopShell'
import { isWorkboxCache, SW_MSG_SKIP_WAITING } from './swConstants'

/**
 * Service-worker lifecycle for the two hosts of this SPA.
 *
 * Browser: register, so push notifications and the background replay of
 * agent messages have a worker to run in. Nothing is cached.
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

/**
 * Fetch a new worker and hand control to it, for the one moment the page is
 * about to reload deliberately: right after a server restart.
 *
 * The worker takes over on install, so after a rebuild this mainly forces the
 * new sw.js to be fetched before the page reloads rather than on the next
 * navigation.
 *
 * Never throws. A worker that cannot be refreshed must not stop the reload.
 */
export async function refreshServiceWorker(): Promise<void> {
  const container = swContainer()
  if (!container)
    return

  try {
    const registrations = await container.getRegistrations()
    await Promise.all(registrations.map(async (registration) => {
      await registration.update()
      registration.waiting?.postMessage({ type: SW_MSG_SKIP_WAITING })
    }))
  }
  catch {
    // Blocked, offline, or the worker is gone — the caller reloads regardless.
  }
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
