import { onUnmounted, ref } from 'vue'

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
      const json = await res.json() as UsageData
      // accounts is omitted from the response for single-account users; guarantee an array.
      data.value = { ...json, accounts: json.accounts ?? [] }
    }
    catch {
      // AbortError and transient failures: leave last known value
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

  return { data, refresh, start, stop }
}
