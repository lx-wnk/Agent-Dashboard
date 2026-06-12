import { ref } from 'vue'

export interface PipelineConfig {
  maxParallelOrchestrators: number
  stageTimeoutSeconds: number
  maxAutoRetries: number
  retryBackoffSeconds: number
}

const config = ref<PipelineConfig | null>(null)
const maxAutoRetries = ref(3)
let fetched = false

async function fetchConfig() {
  if (fetched)
    return
  fetched = true
  try {
    const res = await fetch('/api/pipeline/config')
    if (res.ok) {
      config.value = await res.json()
      maxAutoRetries.value = config.value!.maxAutoRetries
    }
  }
  catch { /* keep default */ }
}

export function usePipelineConfig() {
  fetchConfig()
  return { config, maxAutoRetries }
}
