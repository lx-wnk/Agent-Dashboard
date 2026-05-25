import { ref } from 'vue'
import { useVisibilityPolling } from './useVisibilityPolling'

export interface SystemInfo {
  cpu: { usage: number, cores: number, model: string }
  memory: { total: number, used: number, available: number, usagePercent: number }
  disk: { total: number, used: number, available: number, usagePercent: number, mount: string }
  loadAvg: number[]
  uptime: number
}

export function useSystemResources(pollIntervalMs = 15000) {
  const info = ref<SystemInfo | null>(null)
  const error = ref<string | null>(null)

  async function fetchSystemInfo() {
    try {
      const res = await fetch('/api/system')
      if (res.ok)
        info.value = await res.json()
      else
        error.value = `Failed to load system info (${res.status})`
    }
    catch {
      error.value = 'Network error loading system info.'
    }
  }

  useVisibilityPolling(fetchSystemInfo, pollIntervalMs)

  return {
    info,
    error,
    refetch: fetchSystemInfo,
  }
}
