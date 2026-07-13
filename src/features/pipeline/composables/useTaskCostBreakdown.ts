import type { Ref } from 'vue'
import type { StageCostRow } from '@/features/pipeline/components/StageCostWaterfall.vue'
import type { PipelineTask } from '@/types'
import { ref, watch } from 'vue'

export function useTaskCostBreakdown(task: Ref<PipelineTask | null>) {
  const costBreakdown = ref<StageCostRow[]>([])
  const costLoading = ref(false)
  const costError = ref('')

  async function loadCostBreakdown(taskId: string): Promise<void> {
    costBreakdown.value = []
    costError.value = ''
    costLoading.value = true
    try {
      const res = await fetch(`/api/tasks/${taskId}/cost-breakdown`)
      if (res.ok)
        costBreakdown.value = await res.json()
      else
        costError.value = `Failed to load cost breakdown (${res.status})`
    }
    catch {
      costError.value = 'Failed to load cost breakdown'
    }
    finally {
      costLoading.value = false
    }
  }

  watch(
    () => [task.value?.id, task.value?.currentStage, task.value?.currentIteration, task.value?.latestStageRunStatus] as const,
    ([id]) => {
      if (id)
        void loadCostBreakdown(id)
    },
    { immediate: true },
  )

  return { costBreakdown, costLoading, costError }
}
