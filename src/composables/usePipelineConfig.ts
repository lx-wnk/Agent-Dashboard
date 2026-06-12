import { ref } from 'vue'

export interface PipelineConfig {
  maxParallelOrchestrators: number
  stageTimeoutSeconds: number
  maxAutoRetries: number
  retryBackoffSeconds: number
}

export function usePipelineConfig() {
  const config = ref<PipelineConfig | null>(null)
  const maxAutoRetries = ref(3)

  async function fetchConfig() {
    try {
      const res = await fetch('/api/pipeline/config')
      if (res.ok) {
        config.value = await res.json()
        maxAutoRetries.value = config.value!.maxAutoRetries
      }
    }
    catch { /* keep default */ }
  }

  fetchConfig()

  return { config, maxAutoRetries }
}
