import type { DriftAlert, EvalMetricSnapshot, MetricKey } from '@/types'
import { onUnmounted, ref, shallowRef } from 'vue'
import { METRIC_KEYS } from '@/utils/evalMetrics'

const POLL_INTERVAL_MS = 60_000

export function useEvalMetrics() {
  // snapshots grouped by metricKey for easy charting
  const snapshots = shallowRef<Record<MetricKey, EvalMetricSnapshot[]>>(emptySnapshots())
  const openAlerts = shallowRef<DriftAlert[]>([])
  const isLoading = ref(true)
  const error = ref<string | null>(null)

  let intervalId: ReturnType<typeof setInterval> | null = null
  let aborter: AbortController | null = null
  let driftEventSource: EventSource | null = null
  let active = true

  function emptySnapshots(): Record<MetricKey, EvalMetricSnapshot[]> {
    return Object.fromEntries(METRIC_KEYS.map(k => [k, [] as EvalMetricSnapshot[]])) as Record<MetricKey, EvalMetricSnapshot[]>
  }

  async function fetchMetrics(signal: AbortSignal): Promise<void> {
    const grouped = emptySnapshots()
    await Promise.all(
      METRIC_KEYS.map(async (key) => {
        const res = await fetch(`/api/eval/metrics?metric=${key}&hours=168`, { signal })
        if (!res.ok)
          throw new Error(`HTTP ${res.status}`)
        const data = await res.json() as EvalMetricSnapshot[]
        grouped[key] = data
      }),
    )
    snapshots.value = grouped
  }

  async function fetchAlerts(signal: AbortSignal): Promise<void> {
    const res = await fetch('/api/eval/drift?status=open', { signal })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    openAlerts.value = await res.json() as DriftAlert[]
  }

  async function fetchAll(): Promise<void> {
    if (!active)
      return
    aborter?.abort()
    aborter = new AbortController()
    const { signal } = aborter
    try {
      await Promise.all([fetchMetrics(signal), fetchAlerts(signal)])
      error.value = null
    }
    catch (e: unknown) {
      if ((e as { name?: string })?.name === 'AbortError')
        return
      error.value = e instanceof Error ? e.message : 'Failed to load eval data'
    }
    finally {
      isLoading.value = false
    }
  }

  async function fetchAlertsOnly(): Promise<void> {
    if (!active)
      return
    const ctrl = new AbortController()
    try {
      await fetchAlerts(ctrl.signal)
      error.value = null
    }
    catch (e: unknown) {
      if ((e as { name?: string })?.name !== 'AbortError')
        error.value = e instanceof Error ? e.message : 'Failed to load alerts'
    }
  }

  function startDriftStream(): void {
    if (driftEventSource)
      return
    // EventSource is absent in non-browser environments (jsdom/SSR); the 60s poll covers data freshness there.
    if (typeof EventSource === 'undefined')
      return
    driftEventSource = new EventSource('/api/tasks/stream')
    driftEventSource.onmessage = (e) => {
      try {
        const event = JSON.parse(e.data as string) as { type: string }
        if (event.type === 'eval_drift')
          void fetchAlertsOnly()
      }
      catch { /* ignore malformed frames */ }
    }
    driftEventSource.onerror = () => {
      if (driftEventSource?.readyState === EventSource.CLOSED) {
        driftEventSource = null
        // SSE dropped — the 60 s setInterval poll in start() keeps data fresh
      }
    }
  }

  function stopDriftStream(): void {
    driftEventSource?.close()
    driftEventSource = null
  }

  async function acknowledge(id: string): Promise<void> {
    const res = await fetch(`/api/eval/drift/${id}/ack`, { method: 'POST' })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    await fetchAlertsOnly()
  }

  async function runScan(): Promise<void> {
    const res = await fetch('/api/eval/scan', { method: 'POST' })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    await fetchAll()
  }

  function start(): void {
    if (intervalId !== null)
      return
    active = true
    void fetchAll()
    intervalId = setInterval(fetchAll, POLL_INTERVAL_MS)
    startDriftStream()
  }

  function stop(): void {
    active = false
    if (intervalId !== null) {
      clearInterval(intervalId)
      intervalId = null
    }
    aborter?.abort()
    aborter = null
    stopDriftStream()
  }

  onUnmounted(stop)

  return {
    snapshots,
    openAlerts,
    isLoading,
    error,
    acknowledge,
    runScan,
    refresh: fetchAll,
    start,
    stop,
  }
}
