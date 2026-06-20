import { ref } from 'vue'

export interface PipelineConfig {
  maxParallelOrchestrators: number
  stageTimeoutSeconds: number
  maxAutoRetries: number
  retryBackoffSeconds: number
  stageModels: Record<'implementation' | 'self_review' | 'finalization', string>
}

interface PartialPipelineConfig {
  maxParallelOrchestrators?: number
  stageTimeoutSeconds?: number
  stageModels?: Partial<PipelineConfig['stageModels']>
}

const config = ref<PipelineConfig | null>(null)
const maxAutoRetries = ref(3)
const loading = ref(false)
const error = ref<string | null>(null)
let configPromise: Promise<void> | null = null

function fetchConfig(): Promise<void> {
  if (configPromise)
    return configPromise
  loading.value = true
  error.value = null
  configPromise = (async () => {
    try {
      const res = await fetch('/api/pipeline/config')
      if (!res.ok)
        throw new Error(`HTTP ${res.status}`)
      config.value = await res.json() as PipelineConfig
      maxAutoRetries.value = config.value.maxAutoRetries
    }
    catch (err) {
      error.value = (err as Error).message
      configPromise = null // allow a later retry after a failed fetch
    }
    finally {
      loading.value = false
    }
  })()
  return configPromise
}

async function saveConfig(partial: PartialPipelineConfig): Promise<void> {
  loading.value = true
  error.value = null
  try {
    const res = await fetch('/api/pipeline/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(partial),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
      throw new Error((err as { error: string }).error || `HTTP ${res.status}`)
    }
    configPromise = null // force a fresh fetch so callers see the saved values
    await fetchConfig()
  }
  catch (err) {
    error.value = (err as Error).message
    loading.value = false
  }
}

export function usePipelineConfig() {
  fetchConfig()
  return { config, maxAutoRetries, loading, error, fetchConfig, saveConfig }
}
