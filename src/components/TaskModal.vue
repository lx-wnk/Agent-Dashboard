<script setup lang="ts">
import type { Agent, PermissionRequest, PipelineTask, StageRun, TaskDependency, TaskFeedback, TaskPermission } from '../types'
import type { StageCostRow } from './StageCostWaterfall.vue'
import type { SlashCommand } from './TaskSlashCommandMenu.vue'
import { useIntervalFn } from '@vueuse/core'
import { computed, onUnmounted, ref, watch } from 'vue'
import { useAgents } from '../composables/useAgents'
import { useCopyId } from '../composables/useCopyId'
import { usePipelineConfig } from '../composables/usePipelineConfig'
import { useProjects } from '../composables/useProjects'
import { useSpawners } from '../composables/useSpawners'
import {
  addTaskDependency,
  analyzeTask,
  bulkResolvePermissionRequests,
  cancelTask,
  fetchDependencies,
  fetchDependents,
  fetchPendingPermissionRequests,
  fetchStageRunAgentOutput,
  fetchStageRuns,
  fetchTaskFeedback,
  fetchTaskPermissions,
  grantTaskPermission,
  progressTask,
  removeTaskDependency,
  resolvePermissionRequest,
  resumeStageTask,
  retryTask,
} from '../composables/useTasks'
import { secondsUntil } from '../utils/retryCountdown'
import { STAGE_LABELS } from '../utils/stageLabels'
import { runStatusChipClass } from '../utils/statusColors'
import AgentChatStream from './AgentChatStream.vue'
import AuditLogTab from './AuditLogTab.vue'
import DependencyGraph from './DependencyGraph.vue'
import GitStatusPanel from './GitStatusPanel.vue'
import RefineStatusPanel from './RefineStatusPanel.vue'
import StageCostWaterfall from './StageCostWaterfall.vue'
import StageOutputView from './StageOutputView.vue'
import TaskSlashCommandMenu from './TaskSlashCommandMenu.vue'
import AppButton from './ui/AppButton.vue'
import AppInput from './ui/AppInput.vue'
import AppModal from './ui/AppModal.vue'
import WorktreeCommandRunner from './WorktreeCommandRunner.vue'
import WorktreePanel from './WorktreePanel.vue'

const props = defineProps<{ task: PipelineTask | null }>()
const emit = defineEmits<{ close: [], navigate: [agent: Agent], navigateTask: [taskId: string], openChat: [task: PipelineTask], worktreeChanged: [] }>()

const { copy: copyTaskId, copied: modalCopiedId } = useCopyId(() => props.task?.id ?? '')

const { agents } = useAgents()
const { projects } = useProjects()
const { spawners } = useSpawners()

const { maxAutoRetries: modalMaxAutoRetries } = usePipelineConfig()
const modalRetrySecondsLeft = ref(0)
useIntervalFn(() => { modalRetrySecondsLeft.value = secondsUntil(props.task?.nextRetryAt) }, 1000, { immediate: true })

// Project / spawner re-assignment state
const isAssigningProject = ref(false)
const isAssigningSpawner = ref(false)
const assignError = ref<string | null>(null)

const currentProject = computed(() =>
  props.task?.projectId
    ? (projects.value.find(p => p.id === props.task!.projectId) ?? null)
    : null,
)

const effectiveSpawner = computed(() => {
  // Explicit spawner on task takes priority; fall back to project default
  const spawnerId = props.task?.spawnerId
    ?? currentProject.value?.defaultSpawnerId
    ?? null
  if (!spawnerId)
    return null
  return spawners.value.find(s => s.id === spawnerId) ?? null
})

async function patchTask(patch: { projectId?: string | null, spawnerId?: string | null }) {
  if (!props.task)
    return
  assignError.value = null
  const res = await fetch(`/api/tasks/${props.task.id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(patch),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    assignError.value = (err as { error?: string }).error ?? 'Failed to update task'
  }
}

async function onProjectChange(e: Event) {
  const value = (e.target as HTMLSelectElement).value
  isAssigningProject.value = true
  try {
    await patchTask({ projectId: value || null })
  }
  finally {
    isAssigningProject.value = false
  }
}

async function onSpawnerChange(e: Event) {
  const value = (e.target as HTMLSelectElement).value
  isAssigningSpawner.value = true
  try {
    await patchTask({ spawnerId: value || null })
  }
  finally {
    isAssigningSpawner.value = false
  }
}

type Tab = 'overview' | 'stages' | 'permissions' | 'audit' | 'graph'
const activeTab = ref<Tab>('overview')
const stageRuns = ref<StageRun[]>([])
const permissions = ref<TaskPermission[]>([])
const pendingRequests = ref<PermissionRequest[]>([])
const actionError = ref('')
const isActing = ref(false)
const additionalPrompt = ref('')
const newPermTool = ref('')
const newPermPattern = ref('')
const permError = ref('')
const isGranting = ref(false)

// Two-step confirm for the destructive "Cancel Task" action (irreversible).
const cancelConfirm = ref(false)
let cancelConfirmTimer: ReturnType<typeof setTimeout> | undefined
function onCancelClick() {
  if (!cancelConfirm.value) {
    cancelConfirm.value = true
    cancelConfirmTimer = setTimeout(() => { cancelConfirm.value = false }, 5000)
    return
  }
  if (cancelConfirmTimer)
    clearTimeout(cancelConfirmTimer)
  cancelConfirm.value = false
  void handleAction(() => cancelTask(props.task!.id))
}

const TASK_SLASH_COMMANDS: SlashCommand[] = [
  { name: '/retry', description: 'Retry the current stage' },
  { name: '/grant', description: 'Grant all pending permissions' },
  { name: '/cancel', description: 'Cancel this task' },
  { name: '/status', description: 'Show current stage status' },
  { name: '/help', description: 'List available commands' },
]

const slashMenuRef = ref<InstanceType<typeof TaskSlashCommandMenu> | null>(null)

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

const dependencies = ref<TaskDependency[]>([])
const dependents = ref<TaskDependency[]>([])
const newDepId = ref('')
const newDepStage = ref<'done' | 'cancelled'>('done')
const newDepCancelAction = ref<'cancel' | 'start' | 'on_hold'>('on_hold')
const depError = ref('')
const isAddingDep = ref(false)

async function loadDependencies(): Promise<void> {
  if (!props.task)
    return
  try {
    const [deps, depts] = await Promise.all([
      fetchDependencies(props.task.id),
      fetchDependents(props.task.id),
    ])
    dependencies.value = deps
    dependents.value = depts
  }
  catch {
    depError.value = 'Failed to load dependencies'
  }
}

async function handleAddDependency(): Promise<void> {
  if (!props.task || !newDepId.value.trim())
    return
  depError.value = ''
  isAddingDep.value = true
  try {
    await addTaskDependency(props.task.id, newDepId.value.trim(), newDepStage.value, newDepCancelAction.value)
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
  if (!props.task)
    return
  try {
    await removeTaskDependency(props.task.id, depId)
    await loadDependencies()
  }
  catch (err) {
    depError.value = (err as Error).message
  }
}

// Live session lookup: match by sessionId first (set after orchestrator attaches
// it via tryAttachSessionId), fall back to activePid so the live stream works
// immediately when the agent starts before session_id is persisted to the DB.
// The backend enriches tasks with `activeSessionId` of the most relevant
// stage_run. If that session is also discovered by the agent scanner (it will
const pipelineAgent = computed(() => {
  const sid = props.task?.activeSessionId
  if (sid)
    return agents.value.find(a => a.sessionId === sid) ?? null
  const pid = props.task?.activePid
  if (pid)
    return agents.value.find(a => a.pid === pid) ?? null
  return null
})

function isTerminal(stage: string | undefined) {
  // `failed` is intentionally excluded — failed tasks are actionable
  // (Retry / Analyze) rather than terminal.
  return stage === 'done' || stage === 'cancelled'
}
function isFailedRun(task: { latestStageRunStatus?: string | null } | null | undefined) {
  return task?.latestStageRunStatus === 'failed'
}

// A stage_run parked at awaiting_user with NO pending permission requests is a
// schema-validation escalation (agent gave up after two malformed outputs and
// the orchestrator cleared its PID for the user to act on). It is not a
// permission gate, so the grant panel never renders — without an explicit
// Resume affordance the user is stuck with only Cancel. Treat it as resumable.
const isResumableAwaitingUser = computed(() =>
  props.task?.latestStageRunStatus === 'awaiting_user' && pendingRequests.value.length === 0,
)

const isOnHoldStage = computed(() => props.task?.currentStage === 'on_hold')

const analysisInfo = ref<{ pid: number, cwd: string } | null>(null)

const latestStageRun = computed(() => {
  if (stageRuns.value.length === 0)
    return null
  return stageRuns.value[stageRuns.value.length - 1]
})

const latestRunAgentMessage = computed<string | null>(() => {
  const out = latestStageRun.value?.output
  if (!out)
    return null
  const msg = (out as Record<string, unknown>).agentMessage
  return typeof msg === 'string' ? msg : null
})

const latestRunError = computed<string | null>(() => {
  const out = latestStageRun.value?.output
  if (!out)
    return null
  const e = (out as Record<string, unknown>).error
  return typeof e === 'string' ? e : null
})

// Session text fetched lazily for failed/timed-out runs that have no agentMessage.
// Also polled every 5s for running stages when no live pipelineAgent is matched.
const sessionAgentText = ref<string | null>(null)
const sessionAgentTextLoading = ref(false)
let runningOutputPoll: ReturnType<typeof setInterval> | null = null

function stopRunningPoll(): void {
  if (runningOutputPoll) {
    clearInterval(runningOutputPoll)
    runningOutputPoll = null
  }
}

const feedbackInput = ref('')
const feedbackHistory = ref<TaskFeedback[]>([])

async function fetchSessionText(run: StageRun) {
  if (!props.task || latestRunAgentMessage.value)
    return
  sessionAgentTextLoading.value = true
  sessionAgentText.value = await fetchStageRunAgentOutput(props.task.id, run.id)
  sessionAgentTextLoading.value = false
}

async function onAnalyze() {
  if (!props.task)
    return
  analysisInfo.value = null
  await handleAction(async () => {
    analysisInfo.value = await analyzeTask(props.task!.id)
  })
}

async function loadDetails() {
  if (!props.task)
    return
  stopRunningPoll()
  sessionAgentText.value = null
  stageRuns.value = await fetchStageRuns(props.task.id)
  permissions.value = await fetchTaskPermissions(props.task.id)
  pendingRequests.value = await fetchPendingPermissionRequests(props.task.id)
  feedbackHistory.value = await fetchTaskFeedback(props.task.id)
  const latest = stageRuns.value[stageRuns.value.length - 1]
  if (latest && (latest.status === 'failed' || latest.status === 'done')) {
    fetchSessionText(latest)
  }
  else if (latest && latest.status === 'running') {
    // Fallback output for running stages: poll the JSONL directly so the
    // overview pane shows something even before the agent scanner links the PID.
    fetchSessionText(latest)
    runningOutputPoll = setInterval(() => {
      void fetchSessionText(latest)
    }, 5000)
  }
}

onUnmounted(stopRunningPoll)

// Reset modal-local state when the user opens a different task.
watch(() => props.task?.id, async (id, prevId) => {
  if (id && id !== prevId) {
    activeTab.value = 'overview'
    actionError.value = ''
    feedbackInput.value = ''
    feedbackHistory.value = []
    void loadDetails()
    void loadDependencies()
    void loadCostBreakdown(id)
  }
})

// Live refresh: when the SSE store pushes updated task fields (stage,
// iteration, run status, active session), re-fetch the dependent details
// so the modal stays in sync with the kanban without re-opening it.
watch(
  () => [
    props.task?.id,
    props.task?.currentStage,
    props.task?.currentIteration,
    props.task?.latestStageRunStatus,
    props.task?.activeSessionId,
  ] as const,
  ([id], [prevId]) => {
    if (!id)
      return
    // First entry into this task is handled by the id-watcher above.
    if (id !== prevId)
      return
    void loadDetails()
    void loadDependencies()
    void loadCostBreakdown(id)
  },
)

async function handleAction(action: () => Promise<void>) {
  if (isActing.value || !props.task)
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

async function onResolve(req: PermissionRequest, outcome: 'granted' | 'denied') {
  await handleAction(() => resolvePermissionRequest(req.id, outcome))
}

// Pending requests grouped by stage_run so the bulk-resolve buttons can
// dispatch one bulk-resolve call per stage_run. In practice every awaiting_user
// run has exactly one group, but the data model permits N pendings across
// multiple runs (e.g. legacy stale entries) so we still iterate cleanly.
const pendingByStageRun = computed<Array<{ stageRunId: string, requests: PermissionRequest[] }>>(() => {
  const groups = new Map<string, PermissionRequest[]>()
  for (const r of pendingRequests.value) {
    const arr = groups.get(r.stageRunId) ?? []
    arr.push(r)
    groups.set(r.stageRunId, arr)
  }
  return Array.from(groups.entries()).map(([stageRunId, requests]) => ({ stageRunId, requests }))
})

async function onResolveAll(stageRunId: string, outcome: 'granted' | 'denied') {
  await handleAction(async () => {
    await bulkResolvePermissionRequests(stageRunId, outcome)
  })
}

async function onGrantPermission() {
  const tool = newPermTool.value.trim()
  if (!tool) {
    permError.value = 'Tool name is required'
    return
  }
  const pattern = newPermPattern.value.trim() || null
  isGranting.value = true
  permError.value = ''
  try {
    await grantTaskPermission(props.task!.id, tool, pattern)
    newPermTool.value = ''
    newPermPattern.value = ''
    permissions.value = await fetchTaskPermissions(props.task!.id)
  }
  catch (e) {
    permError.value = (e as Error).message
  }
  finally {
    isGranting.value = false
  }
}

async function onSlashSelect(cmd: { name: string }) {
  additionalPrompt.value = ''
  switch (cmd.name) {
    case '/retry':
      if (props.task)
        await handleAction(() => retryTask(props.task!.id, undefined))
      break
    case '/grant':
      if (props.task) {
        await handleAction(async () => {
          for (const group of pendingByStageRun.value)
            await bulkResolvePermissionRequests(group.stageRunId, 'granted')
        })
      }
      break
    case '/cancel':
      if (props.task)
        await handleAction(() => cancelTask(props.task!.id))
      break
  }
}

// Escape is handled by AppModal's @keydown.escape on its backdrop — no window listener needed.

function formatDate(iso: string | null): string {
  if (!iso)
    return '—'
  return new Date(iso).toLocaleString()
}

const totalTokensUsed = computed(() =>
  stageRuns.value.reduce((sum, r) => sum + (r.tokensUsed ?? 0), 0),
)

const totalCostCents = computed(() =>
  stageRuns.value.reduce((sum, r) => sum + (r.costCents ?? 0), 0),
)

function formatCost(cents: number): string {
  if (cents === 0)
    return '—'
  if (cents < 100)
    return `${cents}¢`
  return `$${(cents / 100).toFixed(2)}`
}

const runtime = computed(() => {
  if (!props.task)
    return '—'
  const start = new Date(props.task.createdAt).getTime()
  const end = props.task.currentStage === 'done' || props.task.currentStage === 'cancelled'
    ? new Date(props.task.updatedAt).getTime()
    : Date.now()
  const ms = end - start
  const h = Math.floor(ms / 3_600_000)
  const m = Math.floor((ms % 3_600_000) / 60_000)
  const s = Math.floor((ms % 60_000) / 1_000)
  if (h > 0)
    return `${h}h ${m}m`
  if (m > 0)
    return `${m}m ${s}s`
  return `${s}s`
})

// Refinement status panel — last assistant output for concept-stage tasks
const lastRefineOutput = ref('')
const completedRefinePhases = ref<string[]>([])

async function loadLastRefineOutput(taskId: string) {
  try {
    const res = await fetch(`/api/refine/${taskId}/turns`)
    if (!res.ok)
      return
    const turns = await res.json() as Array<{ role: string, content: string, phase?: string | null }>
    const lastAssistant = [...turns].reverse().find(t => t.role === 'assistant')
    lastRefineOutput.value = lastAssistant?.content ?? ''
    completedRefinePhases.value = turns.flatMap(t => (t.phase ? [t.phase] : []))
  }
  catch { /* leave empty */ }
}

watch(
  () => [props.task?.id, props.task?.currentStage, props.task?.refineStatus] as const,
  ([id, stage]) => {
    if (id && stage === 'concept')
      loadLastRefineOutput(id)
  },
  { immediate: true },
)
</script>

<template>
  <AppModal :open="!!task" :z-index="1000" :labelled-by="task ? `task-modal-title-${task.id}` : undefined" @close="emit('close')">
    <template v-if="task">
      <header class="flex items-center justify-between px-5 py-4 border-b border-line">
        <div class="flex items-center gap-2.5 flex-wrap">
          <span
            class="text-[10px] uppercase font-mono px-2 py-[3px] rounded"
            :class="{
              'bg-yellow-500/20 text-yellow-500': task.currentStage === 'on_hold',
              'bg-green-400/20 text-green-400': task.currentStage === 'done',
              'bg-red-400/20 text-red-400': task.currentStage === 'cancelled',
              'bg-raised text-fg-mute': !['on_hold', 'done', 'cancelled'].includes(task.currentStage ?? ''),
            }"
          >{{ task.currentStage ? (STAGE_LABELS[task.currentStage] ?? task.currentStage) : '' }}</span>
          <span v-if="isFailedRun(task)" class="text-[10px] px-1.5 py-px rounded uppercase ml-auto font-mono bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400" title="Latest stage run failed">
            RUN FAILED
          </span>
          <span
            v-if="task.autoRetryCount != null"
            class="text-[10px] px-1.5 py-px rounded uppercase font-mono bg-blue-50 dark:bg-blue-950/50 text-blue-600 dark:text-blue-400"
            :title="`Auto-retry queued (attempt ${task.autoRetryCount} of ${modalMaxAutoRetries})`"
          >Retrying · {{ task.autoRetryCount }}/{{ modalMaxAutoRetries }}{{ modalRetrySecondsLeft > 0 ? ` · ${modalRetrySecondsLeft}s` : '' }}</span>
          <span class="font-mono text-xs text-blue-600 dark:text-blue-400">{{ task.slug }}</span>
          <button
            type="button"
            class="font-mono text-[10px] px-1.5 py-px rounded border bg-raised text-fg-mute border-line hover:text-fg-soft hover:border-fg-mute transition-colors focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-blue-500 flex-shrink-0"
            :aria-label="`Copy task id ${task.id}`"
            :title="modalCopiedId ? 'Copied!' : task.id"
            @click="copyTaskId"
          >
            {{ modalCopiedId ? 'copied' : task.id }}
          </button>
          <h2 :id="`task-modal-title-${task.id}`" class="text-lg font-semibold text-fg">
            {{ task.title }}
          </h2>
        </div>
        <button type="button" class="bg-transparent border-none text-fg-mute text-2xl cursor-pointer px-1 leading-none hover:text-fg" title="Close (Esc)" @click="emit('close')">
          &times;
        </button>
      </header>

      <nav class="flex border-b border-line flex-shrink-0">
        <button
          type="button"
          class="px-4 py-2.5 text-xs font-semibold bg-transparent border-none border-b-2 border-transparent cursor-pointer hover:text-fg-soft transition-colors"
          :class="activeTab === 'overview' ? 'text-blue-600 dark:text-blue-400 border-blue-600 dark:border-blue-400' : 'text-fg-mute'"
          @click="activeTab = 'overview'"
        >
          Overview
        </button>
        <button
          type="button"
          class="px-4 py-2.5 text-xs font-semibold bg-transparent border-none border-b-2 border-transparent cursor-pointer hover:text-fg-soft transition-colors"
          :class="activeTab === 'stages' ? 'text-blue-600 dark:text-blue-400 border-blue-600 dark:border-blue-400' : 'text-fg-mute'"
          @click="activeTab = 'stages'"
        >
          Stages ({{ stageRuns.length }})
        </button>
        <button
          type="button"
          class="px-4 py-2.5 text-xs font-semibold bg-transparent border-none border-b-2 border-transparent cursor-pointer hover:text-fg-soft transition-colors"
          :class="activeTab === 'permissions' ? 'text-blue-600 dark:text-blue-400 border-blue-600 dark:border-blue-400' : 'text-fg-mute'"
          @click="activeTab = 'permissions'"
        >
          Permissions ({{ permissions.length }})
        </button>
        <button
          type="button"
          class="px-4 py-2.5 text-xs font-semibold bg-transparent border-none border-b-2 border-transparent cursor-pointer hover:text-fg-soft transition-colors"
          :class="activeTab === 'audit' ? 'text-blue-600 dark:text-blue-400 border-blue-600 dark:border-blue-400' : 'text-fg-mute'"
          @click="activeTab = 'audit'"
        >
          Audit
        </button>
        <button
          type="button"
          class="px-4 py-2.5 text-xs font-semibold bg-transparent border-none border-b-2 border-transparent cursor-pointer hover:text-fg-soft transition-colors"
          :class="activeTab === 'graph' ? 'text-blue-600 dark:text-blue-400 border-blue-600 dark:border-blue-400' : 'text-fg-mute'"
          @click="activeTab = 'graph'"
        >
          Dependencies
        </button>
      </nav>

      <div class="flex-1 min-h-0 overflow-y-auto">
        <!-- Overview tab -->
        <section v-if="activeTab === 'overview'" class="flex-1 overflow-y-auto p-5 flex flex-col gap-4 min-h-0">
          <!-- Lingering-pending gate banner: orchestrator refuses to spawn
               a fresh run while pendings linger on the previous run. Tells
               the user WHY the task is parked (otherwise looks "stuck"). -->
          <div
            v-if="task.blockedByPendingPermissions"
            class="bg-orange-50 dark:bg-orange-950/30 border border-orange-300 dark:border-orange-700/60 rounded-md px-3.5 py-3"
          >
            <h3 class="text-[11px] uppercase tracking-[0.5px] text-orange-700 dark:text-orange-400 font-semibold mb-1.5 flex items-center gap-1.5">
              <span aria-hidden="true">⏸</span> Respawn blocked
            </h3>
            <p class="text-[12px] text-fg-soft leading-relaxed">
              The previous stage run still has unresolved permission requests. Resolve them below — the orchestrator will spawn a fresh run automatically once the queue is clear.
            </p>
          </div>
          <!-- Pending permission requests banner -->
          <div
            v-if="pendingRequests.length > 0"
            class="bg-yellow-50 dark:bg-yellow-950/30 border border-yellow-300 dark:border-yellow-700/60 rounded-md px-3.5 py-3"
          >
            <h3 class="text-[11px] uppercase tracking-[0.5px] text-yellow-700 dark:text-yellow-400 font-semibold mb-2 flex items-center gap-1.5">
              <span aria-hidden="true">⚠</span> Waiting for Permission ({{ pendingRequests.length }})
            </h3>
            <div
              v-for="group in pendingByStageRun"
              :key="group.stageRunId"
              class="mb-3 last:mb-0"
            >
              <div
                v-if="group.requests.length > 1"
                class="flex gap-1.5 mb-2 pb-2 border-b border-yellow-200 dark:border-yellow-800/50"
              >
                <AppButton
                  variant="primary"
                  size="sm"
                  :disabled="isActing"
                  @click="onResolveAll(group.stageRunId, 'granted')"
                >
                  Grant All ({{ group.requests.length }})
                </AppButton>
                <AppButton
                  variant="danger"
                  size="sm"
                  :disabled="isActing"
                  @click="onResolveAll(group.stageRunId, 'denied')"
                >
                  Deny All ({{ group.requests.length }})
                </AppButton>
              </div>
              <div
                v-for="req in group.requests"
                :key="req.id"
                class="border-t border-yellow-200 dark:border-yellow-800/50 first:border-t-0 first:pt-0 pt-2 mt-2 first:mt-0"
              >
                <div class="flex items-baseline gap-2 flex-wrap">
                  <strong class="text-sm text-fg">{{ req.tool }}</strong>
                  <span
                    v-if="req.pattern"
                    class="font-mono text-xs text-fg-soft bg-yellow-100/60 dark:bg-yellow-900/40 px-1.5 py-px rounded"
                  >{{ req.pattern }}</span>
                  <span
                    v-if="req.reRequestCount && req.reRequestCount > 1"
                    class="ml-1.5 inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-medium bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-400"
                    :title="`Requested ${req.reRequestCount} times`"
                  >
                    {{ req.reRequestCount }}x re-requests
                  </span>
                </div>
                <p v-if="req.reason" class="text-[11px] text-fg-mute mt-1 leading-relaxed">
                  {{ req.reason }}
                </p>
                <div class="flex gap-1.5 mt-2">
                  <AppButton
                    variant="primary"
                    size="sm"
                    :disabled="isActing"
                    @click="onResolve(req, 'granted')"
                  >
                    Grant
                  </AppButton>
                  <AppButton
                    variant="danger"
                    size="sm"
                    :disabled="isActing"
                    @click="onResolve(req, 'denied')"
                  >
                    Deny
                  </AppButton>
                </div>
              </div>
            </div>
          </div>

          <!-- Concept-stage refinement banner -->
          <template v-if="task.currentStage === 'concept'">
            <RefineStatusPanel
              :status="task.refineStatus ?? 'idle'"
              :error="task.refineError ?? null"
              :last-output="lastRefineOutput"
              :completed-phases="completedRefinePhases"
              class="mb-2"
            />
            <div
              class="flex items-center justify-between gap-3 bg-blue-50 dark:bg-blue-950/30 border border-blue-200 dark:border-blue-800 rounded-md px-3.5 py-3 mb-2"
            >
              <div class="flex flex-col gap-0.5">
                <span class="text-xs font-semibold text-blue-700 dark:text-blue-300">This ticket is waiting for refinement</span>
                <span class="text-[11px] text-blue-600/80 dark:text-blue-400/80">Continue the conversation with the refinement assistant</span>
              </div>
              <AppButton variant="primary" size="sm" @click="emit('openChat', task)">
                Continue Chat →
              </AppButton>
            </div>
          </template>

          <!-- 1. Info grid -->
          <dl class="grid grid-cols-[auto_1fr] gap-y-1.5 gap-x-4 text-[13px] mb-2">
            <div class="contents">
              <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
                CWD
              </dt><dd class="font-mono text-xs text-fg truncate">
                {{ task.cwd }}
              </dd>
            </div>
            <div v-if="task.sourceBranch" class="contents">
              <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
                Source
              </dt><dd class="font-mono text-xs text-fg">
                {{ task.sourceBranch }}
              </dd>
            </div>
            <div v-if="task.targetBranch" class="contents">
              <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
                Target
              </dt><dd class="font-mono text-xs text-fg">
                {{ task.targetBranch }}
              </dd>
            </div>
            <div class="contents">
              <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
                Iter
              </dt><dd class="text-fg">
                {{ task.currentIteration ?? 0 }} / {{ task.maxIterations }}
              </dd>
            </div>
            <div class="contents">
              <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
                Created
              </dt><dd class="text-fg">
                {{ formatDate(task.createdAt) }}
              </dd>
            </div>
            <div class="contents">
              <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
                Runtime
              </dt><dd class="text-fg font-mono text-xs">
                {{ runtime }}
              </dd>
            </div>
            <div v-if="totalTokensUsed > 0" class="contents">
              <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
                Tokens
              </dt><dd class="text-fg font-mono text-xs">
                {{ totalTokensUsed.toLocaleString() }}
              </dd>
            </div>
            <div v-if="totalCostCents > 0" class="contents">
              <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
                Cost
              </dt><dd class="text-fg font-mono text-xs">
                {{ formatCost(totalCostCents) }}
              </dd>
            </div>
            <div v-if="task.tokenBudget" class="contents">
              <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
                Token Budget
              </dt><dd class="text-fg">
                {{ task.tokenBudget.toLocaleString() }}
              </dd>
            </div>
            <div v-if="task.parentTaskId" class="contents">
              <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
                Parent
              </dt><dd class="font-mono text-xs text-fg">
                {{ task.parentTaskId }}
              </dd>
            </div>
          </dl>

          <!-- Worktree live status -->
          <WorktreePanel
            :task-id="task.id"
            :worktree-path="task.worktreePath ?? null"
            :active="!!task"
            @change="emit('worktreeChanged')"
          />

          <!-- Project + Spawner assignment row -->
          <section class="border-t border-line pt-3 flex flex-col gap-2.5">
            <h4 class="text-[11px] font-semibold uppercase tracking-[0.5px] text-fg-mute">
              Project &amp; Spawner
            </h4>
            <p v-if="assignError" class="text-[11px] text-red-600 dark:text-red-400">
              {{ assignError }}
            </p>
            <div class="grid grid-cols-2 gap-3">
              <!-- Project -->
              <div>
                <label class="block text-[10px] uppercase tracking-[0.5px] text-fg-mute mb-1" :for="`task-modal-project-${task.id}`">
                  Project
                </label>
                <div class="flex items-center gap-2">
                  <span
                    v-if="currentProject"
                    class="inline-flex items-center gap-1 text-[10px] font-semibold px-1.5 py-px rounded border border-transparent flex-shrink-0"
                    :style="currentProject.color ? { backgroundColor: `${currentProject.color}22`, color: currentProject.color, borderColor: `${currentProject.color}55` } : {}"
                    :class="!currentProject.color ? 'bg-raised text-fg-mute border-line' : ''"
                  >
                    <span aria-hidden="true">◫</span>{{ currentProject.name }}
                  </span>
                  <select
                    :id="`task-modal-project-${task.id}`"
                    :value="task.projectId ?? ''"
                    :disabled="isAssigningProject"
                    class="flex-1 min-w-0 bg-raised border border-line rounded px-2 py-1 text-fg text-xs focus:outline-none focus:border-blue-500 disabled:opacity-50"
                    @change="onProjectChange"
                  >
                    <option value="">
                      None
                    </option>
                    <option v-for="p in projects" :key="p.id" :value="p.id">
                      {{ p.name }}
                    </option>
                  </select>
                </div>
              </div>
              <!-- Spawner -->
              <div>
                <label class="block text-[10px] uppercase tracking-[0.5px] text-fg-mute mb-1" :for="`task-modal-spawner-${task.id}`">
                  Spawner
                </label>
                <div class="flex items-center gap-2">
                  <span
                    v-if="effectiveSpawner"
                    class="inline-flex items-center gap-1 text-[10px] font-mono px-1.5 py-px rounded bg-raised text-fg-mute border border-line flex-shrink-0"
                    :title="effectiveSpawner.description"
                  >{{ effectiveSpawner.name }}</span>
                  <span v-else-if="!task.spawnerId && currentProject?.defaultSpawnerId" class="text-[10px] text-fg-mute italic flex-shrink-0">from project</span>
                  <select
                    :id="`task-modal-spawner-${task.id}`"
                    :value="task.spawnerId ?? ''"
                    :disabled="isAssigningSpawner"
                    class="flex-1 min-w-0 bg-raised border border-line rounded px-2 py-1 text-fg text-xs focus:outline-none focus:border-blue-500 disabled:opacity-50"
                    @change="onSpawnerChange"
                  >
                    <option value="">
                      {{ currentProject?.defaultSpawnerId ? 'Project default' : 'Default' }}
                    </option>
                    <option v-for="s in spawners" :key="s.id" :value="s.id">
                      {{ s.name }}{{ s.builtIn ? ' (built-in)' : '' }}
                    </option>
                  </select>
                </div>
              </div>
            </div>
          </section>

          <!-- Dependencies section -->
          <section class="mb-3 border-t border-line pt-3">
            <h4 class="text-[11px] font-semibold uppercase tracking-[0.5px] text-fg-mute mb-2">
              Dependencies
            </h4>

            <div v-if="dependencies.length > 0" class="mb-2.5">
              <p class="text-[11px] text-fg-mute font-semibold uppercase tracking-[0.3px] mb-1">
                Waiting for:
              </p>
              <div
                v-for="dep in dependencies"
                :key="dep.id"
                class="flex items-center gap-2 py-1 text-xs"
              >
                <span class="flex-1 text-fg">{{ dep.dependsOnTitle }}</span>
                <span
                  class="px-1.5 py-px rounded text-[10px] font-mono"
                  :class="dep.dependsOnStage === dep.requiredStage
                    ? 'bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400 border border-green-300 dark:border-green-700'
                    : 'bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400 border border-red-300 dark:border-red-700/50'"
                >{{ dep.dependsOnStage }}</span>
                <span class="text-[10px] text-fg-mute font-mono">on cancel: {{ dep.onCancelAction }}</span>
                <button type="button" class="bg-transparent border-none cursor-pointer text-fg-mute px-1 py-px text-[10px] rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400" title="Remove dependency" @click="handleRemoveDependency(dep.id)">
                  ✕
                </button>
              </div>
            </div>

            <div v-if="dependents.length > 0" class="mb-2.5">
              <p class="text-[11px] text-fg-mute font-semibold uppercase tracking-[0.3px] mb-1">
                Needed by:
              </p>
              <div v-for="dep in dependents" :key="dep.id" class="flex items-center gap-2 py-1 text-xs">
                <span class="flex-1 text-fg">{{ dep.taskTitle || dep.taskId }}</span>
              </div>
            </div>

            <form class="flex gap-1.5 items-center flex-wrap mt-2" @submit.prevent="handleAddDependency">
              <AppInput
                v-model="newDepId"
                class="flex-1 min-w-0"
                placeholder="Predecessor Task ID"
                :disabled="isAddingDep"
              />
              <select v-model="newDepStage" aria-label="Required stage" class="px-1.5 py-1 border border-line rounded bg-raised text-fg text-[11px]">
                <option value="done">
                  Done
                </option>
                <option value="cancelled">
                  Cancelled
                </option>
              </select>
              <select v-model="newDepCancelAction" aria-label="On cancel action" class="px-1.5 py-1 border border-line rounded bg-raised text-fg text-[11px]">
                <option value="on_hold">
                  On Hold (on cancel)
                </option>
                <option value="cancel">
                  Cancel (on cancel)
                </option>
                <option value="start">
                  Start (on cancel)
                </option>
              </select>
              <button type="submit" class="px-2.5 py-1 bg-blue-600 text-white border-none rounded text-xs cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed hover:brightness-110" :disabled="isAddingDep || !newDepId.trim()">
                Add
              </button>
            </form>
            <p v-if="depError" class="text-[11px] text-red-600 dark:text-red-400 mt-1">
              {{ depError }}
            </p>
          </section>

          <!-- 2. Origin Prompt (collapsed) -->
          <details v-if="task.description" class="mb-3 text-xs border-t border-line pt-3">
            <summary class="cursor-pointer text-fg-mute py-1.5 select-none hover:text-slate-500 dark:hover:text-slate-400">
              Origin Prompt
            </summary>
            <div class="mt-1.5 px-3 py-3 bg-app rounded-md text-[13px] leading-relaxed whitespace-pre-wrap text-fg-mute">
              {{ task.description }}
            </div>
          </details>

          <!-- 3. Current Output -->
          <div v-if="latestStageRun" class="bg-app border border-line rounded-md px-3.5 py-3 border-t border-line pt-3">
            <div class="text-[10px] uppercase tracking-[0.5px] text-fg-mute font-semibold mb-2">
              Current Output
            </div>
            <div class="flex items-center gap-2 flex-wrap mb-2">
              <span class="font-mono text-[10px] uppercase bg-raised text-fg-mute px-2 py-0.5 rounded font-semibold">{{ latestStageRun.stage }}</span>
              <span class="text-[10px] text-fg-mute font-mono">iter {{ latestStageRun.iteration }}</span>
              <span class="text-[10px] px-1.5 py-px rounded uppercase ml-auto font-mono" :class="runStatusChipClass(latestStageRun.status)">{{ latestStageRun.status }}</span>
              <span class="text-[11px] text-fg-mute ml-auto">
                {{ formatDate(latestStageRun.startedAt) }}
                <template v-if="latestStageRun.endedAt"> → {{ formatDate(latestStageRun.endedAt) }}</template>
              </span>
            </div>
            <AgentChatStream
              v-if="latestStageRun.status === 'running' && pipelineAgent"
              :agent="pipelineAgent"
              :local-messages="[]"
              class="border-t border-line mt-2 pt-3 min-h-[200px] max-h-[40vh] px-0 py-3"
            />
            <template v-else>
              <!-- Error banner (timeout, schema failure, etc.) -->
              <div v-if="latestRunError" class="mt-2 rounded-md bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-800/50 px-3 py-2 flex items-start gap-2">
                <span class="text-red-500 dark:text-red-400 text-sm leading-none mt-0.5">✗</span>
                <p class="text-xs text-red-700 dark:text-red-300 font-mono leading-relaxed whitespace-pre-wrap break-words">
                  {{ latestRunError }}
                </p>
              </div>

              <!-- Agent prose captured at completion (e.g. "no json block") -->
              <div v-if="latestRunAgentMessage" class="mt-2">
                <div class="text-[10px] uppercase tracking-[0.5px] text-fg-mute mb-1">
                  Agent output
                </div>
                <pre class="font-mono text-[11px] bg-card rounded px-3 py-2.5 whitespace-pre-wrap break-words max-h-[300px] overflow-y-auto text-fg-mute leading-relaxed">{{ latestRunAgentMessage }}</pre>
              </div>

              <!-- Session text fetched from JSONL (failed/done runs, or running without live pipelineAgent) -->
              <div v-else-if="sessionAgentText || sessionAgentTextLoading" class="mt-2">
                <div class="text-[10px] uppercase tracking-[0.5px] text-fg-mute mb-1">
                  Agent output
                </div>
                <div v-if="sessionAgentTextLoading" class="text-[11px] text-fg-mute animate-pulse">
                  Loading…
                </div>
                <pre v-else class="font-mono text-[11px] bg-card rounded px-3 py-2.5 whitespace-pre-wrap break-words max-h-[400px] overflow-y-auto text-fg-mute leading-relaxed">{{ sessionAgentText }}</pre>
              </div>

              <!-- Running stage: no live stream yet and no session text available -->
              <div v-else-if="latestStageRun.status === 'running'" class="mt-2 text-[11px] text-fg-mute animate-pulse">
                Agent is running…
              </div>

              <!-- Structured stage output (successful run with parsed fields) -->
              <details v-else-if="latestStageRun.output && !latestRunError" class="mt-1.5">
                <summary class="cursor-pointer text-[11px] text-fg-mute py-0.5 select-none hover:text-slate-500">
                  Stage output
                </summary>
                <StageOutputView :stage="latestStageRun.stage" :output="latestStageRun.output" :status="latestStageRun.status" />
              </details>
            </template>
          </div>

          <!-- Cost breakdown section -->
          <section class="mt-2">
            <h3 class="text-[12px] font-semibold uppercase tracking-wider text-fg-mute mb-2">
              Cost breakdown
            </h3>
            <div v-if="costLoading" class="text-sm text-fg-mute">
              Loading...
            </div>
            <div v-else-if="costError" class="text-sm text-red-500 dark:text-red-400">
              {{ costError }}
            </div>
            <StageCostWaterfall v-else :rows="costBreakdown" />
          </section>

          <!-- Git status section -->
          <div class="mt-4">
            <h3 class="text-xs font-semibold text-fg-mute uppercase tracking-wide mb-2">
              Git Status
            </h3>
            <GitStatusPanel :task-id="task.id" />
          </div>

          <!-- Worktree command runner -->
          <WorktreeCommandRunner v-if="task" :task-id="task.id" class="mt-4" />
        </section>

        <!-- Stages tab -->
        <section v-if="activeTab === 'stages'" class="p-5">
          <div v-if="stageRuns.length === 0" class="text-fg-mute text-xs text-center py-8">
            No stage runs yet.
          </div>
          <div v-for="run in stageRuns" v-else :key="run.id" class="px-3 py-2.5 bg-app rounded-md mb-2">
            <div class="flex items-center gap-2.5 mb-1">
              <span class="font-semibold text-xs text-fg">{{ run.stage }}</span>
              <span class="font-mono text-[11px] text-fg-mute">iter {{ run.iteration }}</span>
              <span class="text-[10px] px-1.5 py-px rounded uppercase ml-auto font-mono" :class="runStatusChipClass(run.status)">{{ run.status }}</span>
            </div>
            <div v-if="run.sessionName" class="text-[11px] text-fg-mute mt-0.5">
              session: <code>{{ run.sessionName }}</code>
            </div>
            <div class="text-[11px] text-fg-mute mt-0.5">
              started {{ formatDate(run.startedAt) }} · ended {{ formatDate(run.endedAt) }}
            </div>
            <div v-if="run.output" class="mt-1.5 text-[11px]">
              <StageOutputView :stage="run.stage" :output="run.output" :status="run.status" />
            </div>
          </div>
        </section>

        <!-- Permissions tab -->
        <section v-if="activeTab === 'permissions'" class="p-5">
          <div v-if="pendingRequests.length > 0" class="mb-4">
            <h3 class="text-[11px] uppercase text-fg-mute mb-2 tracking-[0.5px]">
              Pending runtime requests ({{ pendingRequests.length }})
            </h3>
            <div v-for="group in pendingByStageRun" :key="group.stageRunId" class="mb-3 last:mb-0">
              <div
                v-if="group.requests.length > 1"
                class="flex gap-1.5 mb-2"
              >
                <AppButton
                  variant="primary"
                  size="sm"
                  :disabled="isActing"
                  @click="onResolveAll(group.stageRunId, 'granted')"
                >
                  Grant All ({{ group.requests.length }})
                </AppButton>
                <AppButton
                  variant="danger"
                  size="sm"
                  :disabled="isActing"
                  @click="onResolveAll(group.stageRunId, 'denied')"
                >
                  Deny All ({{ group.requests.length }})
                </AppButton>
              </div>
              <div v-for="req in group.requests" :key="req.id" class="bg-yellow-50/50 dark:bg-yellow-950/20 border border-yellow-300/60 dark:border-yellow-700/40 rounded-md p-3 mb-2">
                <div class="flex gap-2.5 items-baseline flex-wrap">
                  <strong>{{ req.tool }}</strong>
                  <span v-if="req.pattern" class="font-mono text-xs text-fg">{{ req.pattern }}</span>
                  <span
                    v-if="req.reRequestCount && req.reRequestCount > 1"
                    class="ml-1.5 inline-flex items-center px-1.5 py-0.5 rounded-full text-[10px] font-medium bg-amber-100 dark:bg-amber-900/40 text-amber-700 dark:text-amber-400"
                    :title="`Requested ${req.reRequestCount} times`"
                  >
                    {{ req.reRequestCount }}x re-requests
                  </span>
                </div>
                <div v-if="req.reason" class="text-[11px] text-fg-mute my-1">
                  {{ req.reason }}
                </div>
                <div class="flex gap-1.5 mt-1.5">
                  <AppButton
                    variant="primary"
                    size="sm"
                    :disabled="isActing"
                    @click="onResolve(req, 'granted')"
                  >
                    Grant
                  </AppButton>
                  <AppButton
                    variant="danger"
                    size="sm"
                    :disabled="isActing"
                    @click="onResolve(req, 'denied')"
                  >
                    Deny
                  </AppButton>
                </div>
              </div>
            </div>
          </div>

          <!-- Manual permission grant form -->
          <div class="border border-line rounded-md px-3.5 py-3 mb-3 bg-app">
            <h3 class="text-[11px] uppercase text-fg-mute mb-1 tracking-[0.5px]">
              Grant a tool permission
            </h3>
            <p class="text-[11px] text-fg-mute mb-2.5 leading-relaxed">
              Pre-approve a tool before Retry — useful when the agent hit a permission wall.
              Examples: <code class="bg-raised px-[3px] rounded text-[11px]">Write</code>, <code class="bg-raised px-[3px] rounded text-[11px]">Bash</code> with pattern <code class="bg-raised px-[3px] rounded text-[11px]">npm run *</code>
            </p>
            <div class="flex gap-2 items-center">
              <label for="perm-tool" class="sr-only">Tool</label>
              <input
                id="perm-tool"
                v-model="newPermTool"
                class="flex-1 min-w-0 bg-raised border border-line rounded px-2 py-1.5 text-fg text-xs focus:outline-none focus:border-blue-500"
                placeholder="Tool (e.g. Bash, Write)"
                @keydown.enter="onGrantPermission"
              >
              <label for="perm-pattern" class="sr-only">Pattern</label>
              <input
                id="perm-pattern"
                v-model="newPermPattern"
                class="flex-1 min-w-0 bg-raised border border-line rounded px-2 py-1.5 text-fg text-xs focus:outline-none focus:border-blue-500"
                placeholder="Pattern (optional, e.g. npm run *)"
                @keydown.enter="onGrantPermission"
              >
              <AppButton
                variant="primary"
                size="sm"
                :disabled="isGranting || !newPermTool.trim()"
                @click="onGrantPermission"
              >
                Grant
              </AppButton>
            </div>
            <p v-if="permError" class="text-[11px] text-red-400 mt-1.5">
              {{ permError }}
            </p>
          </div>

          <div v-if="permissions.length > 0">
            <h3 class="text-[11px] uppercase text-fg-mute mb-2 tracking-[0.5px]">
              Granted permissions
            </h3>
            <div v-for="p in permissions" :key="p.id" class="flex gap-2.5 px-2.5 py-1.5 text-xs border-b border-line">
              <span class="font-semibold text-fg min-w-[80px]">{{ p.tool }}</span>
              <span v-if="p.pattern" class="font-mono text-fg-mute flex-1">{{ p.pattern }}</span>
              <span class="text-[10px] text-fg-mute uppercase">{{ p.preApproved ? 'pre-approved' : 'runtime' }}</span>
              <span class="text-[10px] text-fg-mute">{{ p.decidedBy }}</span>
            </div>
          </div>
          <div v-if="permissions.length === 0 && pendingRequests.length === 0" class="text-fg-mute text-xs text-center py-8">
            No permissions granted yet.
          </div>
        </section>

        <!-- Audit tab -->
        <section v-if="activeTab === 'audit'" class="p-5">
          <AuditLogTab v-if="task" :task-id="task.id" />
        </section>

        <!-- Dependencies graph tab -->
        <section v-if="activeTab === 'graph' && task" class="p-5">
          <DependencyGraph :task-id="task.id" @navigate="id => emit('navigateTask', id)" />
        </section>
      </div>

      <footer class="px-5 py-3 border-t border-line flex-shrink-0">
        <p v-if="actionError" class="text-red-600 dark:text-red-400 text-xs mb-2">
          {{ actionError }}
        </p>
        <p v-if="analysisInfo" class="text-green-600 dark:text-green-400 text-xs mb-2">
          Analysis agent spawned · PID <code>{{ analysisInfo.pid }}</code> · look for it in the agents list.
        </p>
        <!-- Optional instruction for Resume/Retry -->
        <div v-if="isFailedRun(task) || isResumableAwaitingUser" class="mb-2">
          <div class="relative">
            <TaskSlashCommandMenu
              ref="slashMenuRef"
              v-model="additionalPrompt"
              :commands="TASK_SLASH_COMMANDS"
              @select="onSlashSelect"
            />
            <textarea
              v-model="additionalPrompt"
              rows="2"
              class="w-full bg-raised border border-line rounded px-2.5 py-1.5 text-fg text-xs resize-none focus:outline-none focus:border-blue-500 placeholder:text-fg-faint"
              placeholder="Optional instruction for Resume / Retry (e.g. logic change or hint)…"
              @keydown="slashMenuRef?.onKeydown($event)"
            />
          </div>
        </div>
        <div class="flex gap-2 justify-end">
          <AppButton
            v-if="(isFailedRun(task) && latestStageRun?.sessionId) || isResumableAwaitingUser"
            variant="secondary"
            :disabled="isActing"
            :title="isResumableAwaitingUser
              ? 'Re-run this stage — the agent stopped without a passing result'
              : 'Continue the agent\'s last session from where it stopped'"
            @click="handleAction(() => resumeStageTask(task!.id, additionalPrompt || undefined))"
          >
            {{ isResumableAwaitingUser && !latestStageRun?.sessionId ? 'Resume Stage' : 'Resume Session' }}
          </AppButton>
          <AppButton
            v-if="isFailedRun(task)"
            variant="info"
            :disabled="isActing"
            title="Start a fresh iteration of this stage"
            @click="handleAction(() => retryTask(task!.id, additionalPrompt || undefined))"
          >
            Retry Stage
          </AppButton>
          <AppButton
            v-if="isFailedRun(task) || isResumableAwaitingUser"
            variant="secondary"
            :disabled="isActing"
            title="Spawn a standalone Claude session with the failure context attached"
            @click="onAnalyze"
          >
            Analyze Failure
          </AppButton>
          <AppButton
            v-if="!isTerminal(task.currentStage) && !isOnHoldStage && !isFailedRun(task)"
            variant="secondary"
            :disabled="isActing"
            title="Manually advance to the next stage (skips approval gates)"
            @click="handleAction(() => progressTask(task!.id))"
          >
            Progress →
          </AppButton>
          <AppButton
            v-if="!isTerminal(task.currentStage)"
            variant="danger"
            :disabled="isActing"
            :title="cancelConfirm ? 'Click again to confirm — this stops the task and marks it cancelled' : 'Stop this task and mark it as cancelled'"
            @click="onCancelClick"
          >
            {{ cancelConfirm ? 'Confirm Cancel?' : 'Cancel Task' }}
          </AppButton>
        </div>
      </footer>
    </template>
  </AppModal>
</template>
