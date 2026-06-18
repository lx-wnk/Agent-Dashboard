import { onUnmounted, ref } from 'vue'

// Re-scan cadence matches the server-side cost scan (5 min). Polling slightly
// faster is fine — the query is a cheap aggregation.
const POLL_INTERVAL_MS = 5 * 60 * 1000

function todayStr(): string {
  return new Date().toISOString().slice(0, 10)
}

/**
 * Fetches today's total spend from the shared /api/cost/summary endpoint
 * (from=to=today) so the dashboard footer/status bar reuse the same historical
 * cost logic as the Cost Analytics view, rather than a separate calculation.
 *
 * Distinct from the live "cost of currently-running agents" figure — this is the
 * persisted total for the calendar day.
 */
export function useTodayCost() {
  const todayUsd = ref<number | null>(null)
  let intervalId: ReturnType<typeof setInterval> | null = null
  let aborter: AbortController | null = null

  async function refresh() {
    aborter?.abort()
    aborter = new AbortController()
    const day = todayStr()
    try {
      const res = await fetch(`/api/cost/summary?from=${day}&to=${day}`, { signal: aborter.signal })
      if (!res.ok)
        return
      const data = await res.json() as { totalUsd?: number }
      todayUsd.value = data.totalUsd ?? 0
    }
    catch {
      // AbortError and transient failures: leave the last known value in place.
    }
  }

  function start() {
    if (intervalId !== null)
      return
    void refresh()
    intervalId = setInterval(refresh, POLL_INTERVAL_MS)
  }

  function stop() {
    if (intervalId !== null) {
      clearInterval(intervalId)
      intervalId = null
    }
    aborter?.abort()
    aborter = null
  }

  onUnmounted(stop)

  return { todayUsd, refresh, start, stop }
}
