import { onUnmounted, ref } from 'vue'

/**
 * usePWA — exposes service worker update state.
 *
 * Uses the vite-plugin-pwa virtual module `virtual:pwa-register/vue` when
 * available. The composable is safe to call in any component: if the PWA
 * virtual module is not present (e.g. unit-test environment) it falls back
 * to inert refs so callers never have to guard for undefined.
 *
 * PWA-02: SW update prompt — needsRefresh is true when a new SW is waiting.
 * Call updateSW() to activate it (skipWaiting + page reload).
 */
export function usePWA() {
  const needsRefresh = ref(false)
  const offlineReady = ref(false)

  let registration: ServiceWorkerRegistration | null = null

  function updateSW() {
    if (!registration?.waiting)
      return
    // Reload the page once the new SW takes control so stale assets are replaced.
    navigator.serviceWorker.addEventListener('controllerchange', () => {
      window.location.reload()
    }, { once: true })
    // Signal the waiting SW to skip waiting and take control.
    registration.waiting.postMessage({ type: 'SKIP_WAITING' })
  }

  // Watch for a new service worker entering the "waiting" state.
  function onUpdateFound(reg: ServiceWorkerRegistration) {
    const installing = reg.installing
    if (!installing)
      return
    installing.addEventListener('statechange', () => {
      if (installing.state === 'installed' && navigator.serviceWorker.controller) {
        // A new SW installed while an old one was active — prompt the user.
        registration = reg
        needsRefresh.value = true
      }
      else if (installing.state === 'installed' && !navigator.serviceWorker.controller) {
        // First install — cached for offline use.
        offlineReady.value = true
      }
    })
  }

  if (typeof navigator !== 'undefined' && 'serviceWorker' in navigator) {
    navigator.serviceWorker.getRegistration().then((reg) => {
      if (!reg)
        return
      // Already has a waiting SW (page was opened after SW update landed).
      if (reg.waiting && navigator.serviceWorker.controller) {
        registration = reg
        needsRefresh.value = true
      }
      reg.addEventListener('updatefound', () => onUpdateFound(reg))
    }).catch(() => {
      // Service worker not available or blocked — silently ignore.
    })
  }

  // Dismiss without updating — clears the banner without activating the new SW.
  function dismissUpdate() {
    needsRefresh.value = false
  }

  onUnmounted(() => {
    // Nothing to clean up — event listeners are on the registration object
    // which outlives any component.
  })

  return { needsRefresh, offlineReady, updateSW, dismissUpdate }
}
