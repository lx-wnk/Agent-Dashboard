import type { Ref } from 'vue'
import type { Agent, PermissionRequest, PipelineTask, StageRun, TaskFeedback, TaskPermission } from '../types'
import { computed, onUnmounted, ref, watch } from 'vue'
import { useAgents } from './useAgents'
import {
  fetchPendingPermissionRequests,
  fetchStageRunAgentOutput,
  fetchStageRuns,
  fetchTaskFeedback,
  fetchTaskPermissions,
} from './useTasks'

export interface PendingGroup { stageRunId: string, requests: PermissionRequest[] }

export function useTaskDetails(task: Ref<PipelineTask | null>) {
  const { agents } = useAgents()

  const stageRuns = ref<StageRun[]>([])
  const permissions = ref<TaskPermission[]>([])
  const pendingRequests = ref<PermissionRequest[]>([])
  const feedbackHistory = ref<TaskFeedback[]>([])

  const sessionAgentText = ref<string | null>(null)
  const sessionAgentTextLoading = ref(false)
  let runningOutputPoll: ReturnType<typeof setInterval> | null = null

  const isActing = ref(false)
  const actionError = ref('')

  // Match the live agent by sessionId first (set once the orchestrator attaches
  // it), then fall back to activePid so the stream works before session_id is
  // persisted to the DB.
  const pipelineAgent = computed<Agent | null>(() => {
    const sid = task.value?.activeSessionId
    if (sid)
      return agents.value.find(a => a.sessionId === sid) ?? null
    const pid = task.value?.activePid
    if (pid)
      return agents.value.find(a => a.pid === pid) ?? null
    return null
  })

  const latestStageRun = computed<StageRun | null>(() =>
    stageRuns.value.length === 0 ? null : stageRuns.value[stageRuns.value.length - 1],
  )

  const latestRunAgentMessage = computed<string | null>(() => {
    const msg = (latestStageRun.value?.output as Record<string, unknown> | undefined)?.agentMessage
    return typeof msg === 'string' ? msg : null
  })

  const latestRunError = computed<string | null>(() => {
    const e = (latestStageRun.value?.output as Record<string, unknown> | undefined)?.error
    return typeof e === 'string' ? e : null
  })

  const isFailedRun = computed(() => task.value?.latestStageRunStatus === 'failed')

  // `failed` is intentionally not terminal — failed tasks stay actionable (Retry / Analyze).
  const isTerminal = computed(() =>
    task.value?.currentStage === 'done' || task.value?.currentStage === 'cancelled',
  )

  const isOnHoldStage = computed(() => task.value?.currentStage === 'on_hold')

  // awaiting_user with NO pending requests is a schema-validation escalation, not
  // a permission gate — surface a Resume affordance instead of only Cancel.
  const isResumableAwaitingUser = computed(() =>
    task.value?.latestStageRunStatus === 'awaiting_user' && pendingRequests.value.length === 0,
  )

  const pendingByStageRun = computed<PendingGroup[]>(() => {
    const groups = new Map<string, PermissionRequest[]>()
    for (const r of pendingRequests.value) {
      const arr = groups.get(r.stageRunId) ?? []
      arr.push(r)
      groups.set(r.stageRunId, arr)
    }
    return Array.from(groups.entries()).map(([stageRunId, requests]) => ({ stageRunId, requests }))
  })

  const totalTokensUsed = computed(() =>
    stageRuns.value.reduce((sum, r) => sum + (r.tokensUsed ?? 0), 0),
  )
  const totalCostCents = computed(() =>
    stageRuns.value.reduce((sum, r) => sum + (r.costCents ?? 0), 0),
  )

  function stopRunningPoll(): void {
    if (runningOutputPoll) {
      clearInterval(runningOutputPoll)
      runningOutputPoll = null
    }
  }

  async function fetchSessionText(run: StageRun): Promise<void> {
    if (!task.value || latestRunAgentMessage.value)
      return
    sessionAgentTextLoading.value = true
    sessionAgentText.value = await fetchStageRunAgentOutput(task.value.id, run.id)
    sessionAgentTextLoading.value = false
  }

  async function loadDetails(): Promise<void> {
    if (!task.value)
      return
    stopRunningPoll()
    sessionAgentText.value = null
    stageRuns.value = await fetchStageRuns(task.value.id)
    permissions.value = await fetchTaskPermissions(task.value.id)
    pendingRequests.value = await fetchPendingPermissionRequests(task.value.id)
    feedbackHistory.value = await fetchTaskFeedback(task.value.id)
    const latest = stageRuns.value[stageRuns.value.length - 1]
    if (latest && (latest.status === 'failed' || latest.status === 'done')) {
      void fetchSessionText(latest)
    }
    else if (latest && latest.status === 'running') {
      // Poll the JSONL directly so the pane shows output before the agent
      // scanner links the PID to a live stream.
      void fetchSessionText(latest)
      runningOutputPoll = setInterval(() => { void fetchSessionText(latest) }, 5000)
    }
  }

  async function handleAction(action: () => Promise<void>): Promise<void> {
    if (isActing.value || !task.value)
      return
    isActing.value = true
    actionError.value = ''
    try {
      await action()
      await loadDetails()
    }
    catch (err) {
      actionError.value = (err as Error).message
    }
    finally {
      isActing.value = false
    }
  }

  onUnmounted(stopRunningPoll)

  // Single refresh trigger: a new task id OR an SSE push to a tracked field
  // (stage / iteration / run status / active session) re-fetches detail state.
  // loadDetails() resets poll + session text internally, so both cases share it.
  watch(
    () => [
      task.value?.id,
      task.value?.currentStage,
      task.value?.currentIteration,
      task.value?.latestStageRunStatus,
      task.value?.activeSessionId,
    ] as const,
    (curr, prev) => {
      const id = curr[0]
      if (!id)
        return
      if (id !== prev?.[0])
        actionError.value = ''
      void loadDetails()
    },
    { immediate: true },
  )

  return {
    stageRuns,
    permissions,
    pendingRequests,
    feedbackHistory,
    sessionAgentText,
    sessionAgentTextLoading,
    isActing,
    actionError,
    pipelineAgent,
    latestStageRun,
    latestRunAgentMessage,
    latestRunError,
    isFailedRun,
    isTerminal,
    isOnHoldStage,
    isResumableAwaitingUser,
    pendingByStageRun,
    totalTokensUsed,
    totalCostCents,
    loadDetails,
    handleAction,
  }
}

export type UseTaskDetails = ReturnType<typeof useTaskDetails>
