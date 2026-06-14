import { ref } from 'vue'

export interface PipelineConfig {
  maxParallelOrchestrators: number
  stageTimeoutSeconds: number
  maxAutoRetries: number
  retryBackoffSeconds: number
}

const config = ref<PipelineConfig | null>(null)
const maxAutoRetries = ref(3)
let configPromise: Promise<void> | null = null

function fetchConfig() {
  if (configPromise)
    return configPromise
  configPromise = (async () => {
    try {
      const res = await fetch('/api/pipeline/config')
      if (res.ok) {
        config.value = await res.json()
        maxAutoRetries.value = config.value!.maxAutoRetries
      }
    }
    catch {
      configPromise = null // allow a later retry after a failed fetch
    }
  })()
  return configPromise
}

export function usePipelineConfig() {
  fetchConfig()
  return { config, maxAutoRetries }
}
