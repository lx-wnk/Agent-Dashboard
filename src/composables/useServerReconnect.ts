import { ref } from 'vue'
import { RECONNECT_POLL_MS } from '../utils/sse'

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
          // Down→up transition confirmed: safe to reload.
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

async function triggerRestart() {
  const res = await fetch('/api/admin/restart', {
    method: 'POST',
    headers: { Origin: window.location.origin },
  })
  if (!res.ok) {
    let detail = `restart failed (${res.status})`
    try {
      const body = await res.json()
      if (body?.error)
        detail = body.error
    }
    catch { /* no body */ }
    throw new Error(detail)
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
