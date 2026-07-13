import type { InjectionKey, Ref } from 'vue'
import type { UseTaskActions } from '@/features/pipeline/composables/useTaskActions'
import type { UseTaskDetails } from '@/features/pipeline/composables/useTaskDetails'
import type { PipelineTask } from '@/types'
import { inject } from 'vue'

export const TaskRefKey: InjectionKey<Ref<PipelineTask | null>> = Symbol('taskRef')
export const TaskDetailsKey: InjectionKey<UseTaskDetails> = Symbol('taskDetails')
export const TaskActionsKey: InjectionKey<UseTaskActions> = Symbol('taskActions')

export function useInjectedTask(): Ref<PipelineTask | null> {
  const task = inject(TaskRefKey)
  if (!task)
    throw new Error('useInjectedTask must be used within a TaskModal')
  return task
}

export function useInjectedTaskDetails(): UseTaskDetails {
  const details = inject(TaskDetailsKey)
  if (!details)
    throw new Error('useInjectedTaskDetails must be used within a TaskModal')
  return details
}

export function useInjectedTaskActions(): UseTaskActions {
  const actions = inject(TaskActionsKey)
  if (!actions)
    throw new Error('useInjectedTaskActions must be used within a TaskModal')
  return actions
}
