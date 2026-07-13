import type { Ref } from 'vue'
import type { PipelineTask } from '@/types'
import { computed, ref } from 'vue'
import { useProjects } from '@/composables/useProjects'
import { useSpawners } from '@/composables/useSpawners'

export function useTaskAssignment(task: Ref<PipelineTask | null>) {
  const { projects } = useProjects()
  const { spawners } = useSpawners()

  const isAssigningProject = ref(false)
  const isAssigningSpawner = ref(false)
  const assignError = ref<string | null>(null)

  const currentProject = computed(() =>
    task.value?.projectId
      ? (projects.value.find(p => p.id === task.value!.projectId) ?? null)
      : null,
  )

  // Explicit spawner on the task wins; otherwise fall back to the project default.
  const effectiveSpawner = computed(() => {
    const spawnerId = task.value?.spawnerId ?? currentProject.value?.defaultSpawnerId ?? null
    if (!spawnerId)
      return null
    return spawners.value.find(s => s.id === spawnerId) ?? null
  })

  async function patchTask(patch: { projectId?: string | null, spawnerId?: string | null }): Promise<void> {
    if (!task.value)
      return
    assignError.value = null
    const res = await fetch(`/api/tasks/${task.value.id}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(patch),
    })
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
      assignError.value = (err as { error?: string }).error ?? 'Failed to update task'
    }
  }

  async function onProjectChange(e: Event): Promise<void> {
    const value = (e.target as HTMLSelectElement).value
    isAssigningProject.value = true
    try {
      await patchTask({ projectId: value || null })
    }
    finally {
      isAssigningProject.value = false
    }
  }

  async function onSpawnerChange(e: Event): Promise<void> {
    const value = (e.target as HTMLSelectElement).value
    isAssigningSpawner.value = true
    try {
      await patchTask({ spawnerId: value || null })
    }
    finally {
      isAssigningSpawner.value = false
    }
  }

  return {
    projects,
    spawners,
    currentProject,
    effectiveSpawner,
    isAssigningProject,
    isAssigningSpawner,
    assignError,
    onProjectChange,
    onSpawnerChange,
  }
}
