import { ref } from 'vue'

type SpawnerStage = 'implementation' | 'self_review' | 'finalization'

export interface ProjectPipelineConfig {
  stageModels: Record<SpawnerStage, string>
  stageSpawners: Record<SpawnerStage, string>
}

export interface PartialProjectPipelineConfig {
  stageModels?: Partial<ProjectPipelineConfig['stageModels']>
  stageSpawners?: Partial<ProjectPipelineConfig['stageSpawners']>
}

export function useProjectPipelineConfig() {
  const config = ref<ProjectPipelineConfig | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function fetch(projectId: string): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const res = await window.fetch(`/api/projects/${projectId}/pipeline-config`)
      if (!res.ok)
        throw new Error(`HTTP ${res.status}`)
      config.value = await res.json() as ProjectPipelineConfig
    }
    catch (err) {
      error.value = (err as Error).message
    }
    finally {
      loading.value = false
    }
  }

  async function save(projectId: string, partial: PartialProjectPipelineConfig): Promise<void> {
    loading.value = true
    error.value = null
    try {
      const res = await window.fetch(`/api/projects/${projectId}/pipeline-config`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(partial),
      })
      if (!res.ok) {
        const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
        throw new Error((err as { error: string }).error || `HTTP ${res.status}`)
      }
      config.value = await res.json() as ProjectPipelineConfig
    }
    catch (err) {
      error.value = (err as Error).message
    }
    finally {
      loading.value = false
    }
  }

  return { config, loading, error, fetch, save }
}
