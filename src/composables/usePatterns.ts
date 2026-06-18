import { onMounted, ref } from 'vue'
import { errorMessage } from '../utils/errorMessage'

export interface ToolPattern {
  tools: string
  frequency: number
}

export function usePatterns() {
  const patterns = ref<ToolPattern[]>([])
  const isLoading = ref(false)
  const error = ref<string | null>(null)

  async function load() {
    isLoading.value = true
    error.value = null
    try {
      const res = await fetch('/api/analytics/patterns')
      if (!res.ok)
        throw new Error(`HTTP ${res.status}`)
      const data = await res.json() as { patterns: ToolPattern[] }
      patterns.value = data.patterns ?? []
    }
    catch (e) {
      error.value = errorMessage(e, 'Failed to load patterns')
      patterns.value = []
    }
    finally {
      isLoading.value = false
    }
  }

  onMounted(() => {
    void load()
  })

  return { patterns, isLoading, error, reload: load }
}
