import { ref } from 'vue'

/**
 * The version of the server binary serving this page.
 *
 * The dashboard reaches its user through three build paths — the `serve` CLI,
 * the macOS desktop shell, and a release build — and each embeds its own copy of
 * this SPA. Without the version on screen, a window from a months-old bundle is
 * indistinguishable from a fresh one, which is exactly how a rebuild can look
 * like it did nothing.
 *
 * Fetched once: it cannot change without the server restarting, and a restart
 * reloads the page.
 */
const version = ref<string | null>(null)
let started = false

export function useBuildVersion() {
  if (!started) {
    started = true
    fetch('/api/system/health')
      .then(res => (res.ok ? res.json() : null))
      .then((data) => {
        if (typeof data?.version === 'string')
          version.value = data.version
      })
      .catch(() => {
        // Offline or blocked: showing nothing beats showing a wrong version.
      })
  }
  return { version }
}
