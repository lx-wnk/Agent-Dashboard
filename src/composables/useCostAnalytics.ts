import { onUnmounted, ref, shallowRef } from 'vue'

const POLL_INTERVAL_MS = 60_000

export interface ModelBreakdown {
  model: string
  costUsd: number
  sessions: number
}

export interface DayPoint {
  day: string
  model: string
  costUsd: number
}

export interface WeekPoint {
  week: string
  costUsd: number
}

export interface CostSummary {
  byModel: ModelBreakdown[]
  byDay: DayPoint[]
  byWeek: WeekPoint[]
  totalUsd: number
  updatedAt: number
}

function emptySummary(): CostSummary {
  return {
    byModel: [],
    byDay: [],
    byWeek: [],
    totalUsd: 0,
    updatedAt: 0,
  }
}

/**
 * Fetches /api/cost/summary and re-polls every 60s while the composable is
 * active. Returns a shared reactive view; safe to call from multiple sites
 * — only one fetch loop runs per `useCostAnalytics()` invocation.
 */
export function useCostAnalytics() {
  const summary = shallowRef<CostSummary>(emptySummary())
  const isLoading = ref(true)
  const error = ref<string | null>(null)

  let intervalId: ReturnType<typeof setInterval> | null = null
  let aborter: AbortController | null = null
  let active = true

  async function fetchOnce() {
    if (!active)
      return
    aborter?.abort()
    aborter = new AbortController()
    try {
      const res = await fetch('/api/cost/summary', { signal: aborter.signal })
      if (!res.ok)
        throw new Error(`HTTP ${res.status}`)
      const data = await res.json() as CostSummary
      summary.value = data
      error.value = null
    }
    catch (e: unknown) {
      if ((e as { name?: string })?.name === 'AbortError')
        return
      error.value = e instanceof Error ? e.message : 'Failed to load cost summary'
    }
    finally {
      isLoading.value = false
    }
  }

  function start() {
    if (intervalId !== null)
      return
    active = true
    void fetchOnce()
    intervalId = setInterval(fetchOnce, POLL_INTERVAL_MS)
  }

  function stop() {
    active = false
    if (intervalId !== null) {
      clearInterval(intervalId)
      intervalId = null
    }
    aborter?.abort()
    aborter = null
  }

  onUnmounted(stop)

  return {
    summary,
    isLoading,
    error,
    refresh: fetchOnce,
    start,
    stop,
  }
}
