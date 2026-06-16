import type { Ref } from 'vue'
import type { PipelineTask, TaskDependency } from '../types'
import { ref, watch } from 'vue'
import { addTaskDependency, fetchDependencies, fetchDependents, removeTaskDependency } from './useTasks'

export function useTaskDependencies(task: Ref<PipelineTask | null>) {
  const dependencies = ref<TaskDependency[]>([])
  const dependents = ref<TaskDependency[]>([])
  const newDepId = ref('')
  const newDepStage = ref<'done' | 'cancelled'>('done')
  const newDepCancelAction = ref<'cancel' | 'start' | 'on_hold'>('on_hold')
  const depError = ref('')
  const isAddingDep = ref(false)

  async function loadDependencies(): Promise<void> {
    if (!task.value)
      return
    try {
      const [deps, depts] = await Promise.all([
        fetchDependencies(task.value.id),
        fetchDependents(task.value.id),
      ])
      dependencies.value = deps
      dependents.value = depts
    }
    catch {
      depError.value = 'Failed to load dependencies'
    }
  }

  async function handleAddDependency(): Promise<void> {
    if (!task.value || !newDepId.value.trim())
      return
    depError.value = ''
    isAddingDep.value = true
    try {
      await addTaskDependency(task.value.id, newDepId.value.trim(), newDepStage.value, newDepCancelAction.value)
      newDepId.value = ''
      await loadDependencies()
    }
    catch (err) {
      depError.value = (err as Error).message
    }
    finally {
      isAddingDep.value = false
    }
  }

  async function handleRemoveDependency(depId: string): Promise<void> {
    if (!task.value)
      return
    try {
      await removeTaskDependency(task.value.id, depId)
      await loadDependencies()
    }
    catch (err) {
      depError.value = (err as Error).message
    }
  }

  watch(
    () => [task.value?.id, task.value?.currentStage, task.value?.currentIteration, task.value?.latestStageRunStatus] as const,
    ([id]) => {
      if (id)
        void loadDependencies()
    },
    { immediate: true },
  )

  return {
    dependencies,
    dependents,
    newDepId,
    newDepStage,
    newDepCancelAction,
    depError,
    isAddingDep,
    handleAddDependency,
    handleRemoveDependency,
  }
}
