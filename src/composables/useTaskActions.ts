import type { Ref } from 'vue'
import type { SlashCommand } from '../components/TaskSlashCommandMenu.vue'
import type { PermissionRequest, PipelineTask } from '../types'
import type { UseTaskDetails } from './useTaskDetails'
import { onUnmounted, ref } from 'vue'
import {
  analyzeTask,
  bulkResolvePermissionRequests,
  cancelTask,
  fetchTaskPermissions,
  grantTaskPermission,
  progressTask,
  resolvePermissionRequest,
  resumeStageTask,
  retryTask,
} from './useTasks'

export const TASK_SLASH_COMMANDS: SlashCommand[] = [
  { name: '/retry', description: 'Retry the current stage' },
  { name: '/grant', description: 'Grant all pending permissions' },
  { name: '/cancel', description: 'Cancel this task' },
  { name: '/status', description: 'Show current stage status' },
  { name: '/help', description: 'List available commands' },
]

export function useTaskActions(task: Ref<PipelineTask | null>, details: UseTaskDetails) {
  const additionalPrompt = ref('')
  const analysisInfo = ref<{ pid: number, cwd: string } | null>(null)

  const isGranting = ref(false)
  const permError = ref('')

  // Two-step confirm for the irreversible Cancel action.
  const cancelConfirm = ref(false)
  let cancelConfirmTimer: ReturnType<typeof setTimeout> | undefined
  function onCancelClick(): void {
    if (!cancelConfirm.value) {
      cancelConfirm.value = true
      cancelConfirmTimer = setTimeout(() => {
        cancelConfirm.value = false
      }, 5000)
      return
    }
    if (cancelConfirmTimer)
      clearTimeout(cancelConfirmTimer)
    cancelConfirm.value = false
    void details.handleAction(() => cancelTask(task.value!.id))
  }
  onUnmounted(() => {
    if (cancelConfirmTimer)
      clearTimeout(cancelConfirmTimer)
  })

  function onResolve(req: PermissionRequest, outcome: 'granted' | 'denied'): Promise<void> {
    return details.handleAction(() => resolvePermissionRequest(task.value!.id, req.id, outcome))
  }

  function onResolveAll(stageRunId: string, outcome: 'granted' | 'denied'): Promise<void> {
    const group = details.pendingByStageRun.value.find(g => g.stageRunId === stageRunId)
    const ids = group ? group.requests.map(r => r.id) : []
    return details.handleAction(async () => {
      const { errors } = await bulkResolvePermissionRequests(task.value!.id, ids, outcome)
      if (errors?.length)
        throw new Error(`${errors.length} request(s) failed: ${errors.join('; ')}`)
    })
  }

  async function onGrantPermission(tool: string, pattern: string | null): Promise<boolean> {
    if (!tool.trim()) {
      permError.value = 'Tool name is required'
      return false
    }
    isGranting.value = true
    permError.value = ''
    try {
      await grantTaskPermission(task.value!.id, tool.trim(), pattern?.trim() || null)
      details.permissions.value = await fetchTaskPermissions(task.value!.id)
      return true
    }
    catch (e) {
      permError.value = (e as Error).message
      return false
    }
    finally {
      isGranting.value = false
    }
  }

  async function onAnalyze(): Promise<void> {
    if (!task.value)
      return
    analysisInfo.value = null
    await details.handleAction(async () => {
      analysisInfo.value = await analyzeTask(task.value!.id)
    })
  }

  function onResume(): Promise<void> {
    return details.handleAction(
      () => resumeStageTask(task.value!.id, additionalPrompt.value || undefined),
      'Stage re-queued — will run when a slot is free',
    )
  }
  function onRetry(): Promise<void> {
    return details.handleAction(
      () => retryTask(task.value!.id, additionalPrompt.value || undefined),
      'Stage re-queued — will run when a slot is free',
    )
  }
  function onProgress(): Promise<void> {
    return details.handleAction(() => progressTask(task.value!.id))
  }

  async function onSlashSelect(cmd: { name: string }): Promise<void> {
    additionalPrompt.value = ''
    if (!task.value)
      return
    switch (cmd.name) {
      case '/retry':
        await onRetry()
        break
      case '/grant':
        await details.handleAction(async () => {
          for (const group of details.pendingByStageRun.value) {
            const { errors } = await bulkResolvePermissionRequests(task.value!.id, group.requests.map(r => r.id), 'granted')
            if (errors?.length)
              throw new Error(`${errors.length} request(s) failed: ${errors.join('; ')}`)
          }
        })
        break
      case '/cancel':
        await details.handleAction(() => cancelTask(task.value!.id))
        break
    }
  }

  return {
    additionalPrompt,
    analysisInfo,
    isGranting,
    permError,
    cancelConfirm,
    slashCommands: TASK_SLASH_COMMANDS,
    onCancelClick,
    onResolve,
    onResolveAll,
    onGrantPermission,
    onAnalyze,
    onResume,
    onRetry,
    onProgress,
    onSlashSelect,
  }
}

export type UseTaskActions = ReturnType<typeof useTaskActions>
