import type { Ref } from 'vue'
import { onUnmounted, ref, watch } from 'vue'
import { errorMessage } from '../utils/errorMessage'

export interface Checkpoint {
  id: string
  taskId: string
  seq: number
  commitSha: string
  filesChanged: number
  preRevert: boolean
  createdAt: string
}

// Module-level fan-out so the single task SSE stream (owned by useTasks) can push
// checkpoint_added events to every mounted useCheckpoints instance.
const checkpointListeners = new Set<(cp: Checkpoint) => void>()

export function emitCheckpointAdded(cp: Checkpoint): void {
  checkpointListeners.forEach(fn => fn(cp))
}

export function useCheckpoints(taskId: Ref<string | null>) {
  const checkpoints = ref<Checkpoint[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function load(id: string) {
    loading.value = true
    error.value = null
    try {
      const res = await fetch(`/api/tasks/${id}/checkpoints`)
      if (!res.ok)
        throw new Error(`HTTP ${res.status}`)
      checkpoints.value = await res.json() as Checkpoint[]
    }
    catch (err) {
      error.value = errorMessage(err)
    }
    finally {
      loading.value = false
    }
  }

  function handleCheckpointAdded(cp: Checkpoint) {
    if (cp.taskId !== taskId.value)
      return
    if (checkpoints.value.some(c => c.id === cp.id))
      return
    checkpoints.value = [cp, ...checkpoints.value]
  }
  checkpointListeners.add(handleCheckpointAdded)
  onUnmounted(() => checkpointListeners.delete(handleCheckpointAdded))

  watch(taskId, (id) => {
    checkpoints.value = []
    if (id)
      void load(id)
  }, { immediate: true })

  async function revert(cpId: string): Promise<void> {
    const id = taskId.value
    if (!id)
      return
    error.value = null
    try {
      const res = await fetch(`/api/tasks/${id}/checkpoints/${cpId}/revert`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Origin': window.location.origin },
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
        throw new Error(body.error || 'Revert failed')
      }
      // Reload so the auto-captured pre-revert checkpoint appears.
      await load(id)
    }
    catch (err) {
      error.value = errorMessage(err)
    }
  }

  return { checkpoints, loading, error, revert }
}
