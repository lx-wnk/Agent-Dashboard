import { computed, onUnmounted, ref } from 'vue'

const POLL_INTERVAL_MS = 5 * 60 * 1_000

export interface WindowData {
  key: string
  tokens: number
  costCents: number
  budgetTokens: number | null
  pct: number | null
}

export interface AccountData {
  label: string
  w5h: { tokens: number, costCents: number }
  w7d: { tokens: number, costCents: number }
}

export interface UsageData {
  windows: WindowData[]
  accounts: AccountData[]
}

export function useUsage() {
  const data = ref<UsageData | null>(null)
  let intervalId: ReturnType<typeof setInterval> | null = null
  let aborter: AbortController | null = null

  async function refresh() {
    aborter?.abort()
    aborter = new AbortController()
    try {
      const res = await fetch('/api/usage', { signal: aborter.signal })
      if (!res.ok)
        return
      data.value = await res.json() as UsageData
    }
    catch {
      // AbortError and transient failures: leave last known value
    }
  }

  // worst is the budgeted window with the highest pct; null when no budgets are set.
  const worst = computed<WindowData | null>(() => {
    if (!data.value)
      return null
    const budgeted = data.value.windows.filter(w => w.pct !== null)
    if (budgeted.length === 0)
      return null
    return budgeted.reduce((a, b) => (b.pct! > a.pct! ? b : a))
  })

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

  return { data, worst, refresh, start, stop }
}
