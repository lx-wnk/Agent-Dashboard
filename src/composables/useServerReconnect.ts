import { ref } from 'vue'
import { refreshServiceWorker } from '../utils/serviceWorker'
import { RECONNECT_POLL_MS } from '../utils/sse'

// Bound on the worker refresh below. A stale page is bad; a page that never
// comes back is worse, so the reload happens either way once this elapses.
const SW_REFRESH_TIMEOUT_MS = 3000

// After ~30s of consecutive failures, stop auto-polling and surface stalled state.
const STALL_THRESHOLD = 20

const isReconnecting = ref(false)
const stalled = ref(false)
let pollTimer: ReturnType<typeof setTimeout> | null = null
let seenDown = false
let failCount = 0

function poll() {
  pollTimer = setTimeout(async () => {
    try {
      const res = await fetch('/api/system/health')
      if (res.ok) {
        if (seenDown) {
          // Down→up transition confirmed: safe to reload. The SPA is precached,
          // so a bare reload here can be served entirely from the old worker
          // and the restarted server then 404s the bundle that page asks for —
          // the window would show the previous build while reporting itself
          // up to date. Hand control to a fresh worker first.
          await Promise.race([
            refreshServiceWorker(),
            new Promise(resolve => setTimeout(resolve, SW_REFRESH_TIMEOUT_MS)),
          ])
          window.location.reload()
          return
        }
        // Old server still alive; wait for it to go down before reloading.
        poll()
        return
      }
    }
    catch {
      // server unreachable
    }
    seenDown = true
    failCount++
    if (failCount >= STALL_THRESHOLD) {
      stalled.value = true
      return
    }
    poll()
  }, RECONNECT_POLL_MS)
}

function beginReconnect() {
  if (isReconnecting.value)
    return
  isReconnecting.value = true
  poll()
}

/**
 * Thrown by triggerRestart on a non-2xx response. Carries the build output a
 * rebuild failure reports, when the server sent one.
 */
export class RestartError extends Error {
  output?: string
  constructor(message: string, output?: string) {
    super(message)
    this.name = 'RestartError'
    this.output = output
  }
}

async function triggerRestart(opts: { rebuild?: boolean } = {}) {
  const res = await fetch('/api/admin/restart', {
    method: 'POST',
    headers: opts.rebuild
      ? { 'Origin': window.location.origin, 'Content-Type': 'application/json' }
      : { Origin: window.location.origin },
    body: opts.rebuild ? JSON.stringify({ rebuild: true }) : undefined,
  })
  if (!res.ok) {
    let detail = `restart failed (${res.status})`
    let output: string | undefined
    try {
      const body = await res.json()
      if (body?.error)
        detail = body.error
      if (typeof body?.output === 'string')
        output = body.output
    }
    catch { /* no body */ }
    throw new RestartError(detail, output)
  }
  beginReconnect()
}

// Singleton: the down-signal is process-wide, shared by the overlay and any
// trigger site.
export function useServerReconnect() {
  return { isReconnecting, stalled, beginReconnect, triggerRestart }
}

/** Test-only: reset module-level singleton state between test cases. */
export function _resetForTesting() {
  if (pollTimer !== null) {
    clearTimeout(pollTimer)
    pollTimer = null
  }
  isReconnecting.value = false
  stalled.value = false
  seenDown = false
  failCount = 0
}
