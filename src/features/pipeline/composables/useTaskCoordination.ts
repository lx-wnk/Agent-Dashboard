import type { Ref } from 'vue'
import type { PipelineTask } from '@/types'
import { computed, ref, watch } from 'vue'

export interface ScratchpadEntry {
  namespace: string
  key: string
  value: string
  updated_at: string
  updated_by_task_id: string
}

export interface CoordLock {
  namespace: string
  key: string
  owner_task_id: string
  acquired_at: string
  expires_at: string
}

export function useTaskCoordination(task: Ref<PipelineTask | null>) {
  const scratchpads = ref<ScratchpadEntry[]>([])
  const locks = ref<CoordLock[]>([])
  const loading = ref(false)
  const error = ref('')

  const namespace = computed(() => task.value?.parentTaskId ?? task.value?.id ?? null)

  async function load(ns: string): Promise<void> {
    scratchpads.value = []
    locks.value = []
    error.value = ''
    loading.value = true
    try {
      const [spRes, lkRes] = await Promise.all([
        fetch(`/api/coord/${ns}/scratchpads`),
        fetch(`/api/coord/${ns}/locks`),
      ])
      if (!spRes.ok || !lkRes.ok) {
        error.value = `Failed to load coordination data (${!spRes.ok ? spRes.status : lkRes.status})`
        return
      }
      const [spData, lkData] = await Promise.all([spRes.json(), lkRes.json()])
      scratchpads.value = spData.entries ?? []
      locks.value = lkData.locks ?? []
    }
    catch {
      error.value = 'Failed to load coordination data'
    }
    finally {
      loading.value = false
    }
  }

  watch(
    namespace,
    (ns) => {
      if (ns)
        void load(ns)
    },
    { immediate: true },
  )

  return { scratchpads, locks, loading, error }
}
