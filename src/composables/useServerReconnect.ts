import { ref } from 'vue'
import { RECONNECT_POLL_MS } from '../utils/sse'

const isReconnecting = ref(false)
let pollTimer: ReturnType<typeof setTimeout> | null = null

function poll() {
  pollTimer = setTimeout(async () => {
    try {
      const res = await fetch('/api/system/health')
      if (res.ok) {
        window.location.reload()
        return
      }
    }
    catch {
      // server still down — keep polling
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
  return { isReconnecting, beginReconnect, triggerRestart }
}

/** Test-only: reset module-level singleton state between test cases. */
export function _resetForTesting() {
  if (pollTimer !== null) {
    clearTimeout(pollTimer)
    pollTimer = null
  }
  isReconnecting.value = false
}
