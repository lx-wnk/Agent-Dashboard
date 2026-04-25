<script setup lang="ts">
import type { Agent, OutputMessage, PermissionRequest, PipelineTask, StageRun, TaskDependency, TaskFeedback, TaskPermission } from '../types'
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useAgents } from '../composables/useAgents'
import {
  addTaskDependency,
  analyzeTask,
  approveTask,
  cancelTask,
  fetchDependencies,
  fetchDependents,
  fetchPendingPermissionRequests,
  fetchStageRuns,
  fetchTaskFeedback,
  fetchTaskPermissions,
  grantTaskPermission,
  progressTask,
  removeTaskDependency,
  requestChanges,
  resolvePermissionRequest,
  retryTask,
} from '../composables/useTasks'
import AgentChatStream from './AgentChatStream.vue'
import CrossLinkBanner from './CrossLinkBanner.vue'
import PromptInput from './PromptInput.vue'
import StageOutputView from './StageOutputView.vue'
import AppModal from './ui/AppModal.vue'

const props = defineProps<{ task: PipelineTask | null }>()
const emit = defineEmits<{ close: [], navigate: [agent: Agent] }>()

const { agents } = useAgents()

type Tab = 'overview' | 'session' | 'stages' | 'permissions' | 'audit'
const activeTab = ref<Tab>('overview')
const stageRuns = ref<StageRun[]>([])
const permissions = ref<TaskPermission[]>([])
const pendingRequests = ref<PermissionRequest[]>([])
const feedbackHistory = ref<TaskFeedback[]>([])
const feedbackInput = ref('')
const actionError = ref('')
const isActing = ref(false)
const newPermTool = ref('')
const newPermPattern = ref('')
const permError = ref('')
const isGranting = ref(false)

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
    depError.value = 'Abhängigkeiten konnten nicht geladen werden'
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
// be, for any running detached claude process), we get a live Agent object
// with channelAvailable/status, which we pass into AgentChatStream +
// PromptInput unchanged.
const pipelineAgent = computed(() => {
  const sid = props.task?.activeSessionId
  if (sid)
    return agents.value.find(a => a.sessionId === sid) ?? null
  const pid = props.task?.activePid
  if (pid)
    return agents.value.find(a => a.pid === pid) ?? null
  return null
})

const sessionLocalMessages = ref<OutputMessage[]>([])
const sessionChatRef = ref<InstanceType<typeof AgentChatStream> | null>(null)

// Stage-aware empty state for the Session tab: explains WHY there's no
// live agent right now and what (if anything) the user should do next.
const sessionEmptyHint = computed<{ title: string, body: string }>(() => {
  const stage = props.task?.currentStage
  if (stage === 'backlog' || stage === 'pruefung')
    return { title: 'No agent started yet', body: 'This task is waiting to be picked up by the pipeline.' }
  if (stage === 'approval1' || stage === 'approval2')
    return { title: 'Waiting for approval', body: 'The previous stage is complete — review the output in the Overview tab and approve or request changes.' }
  if (stage === 'on_hold')
    return { title: 'Task paused', body: 'The task was put on hold and needs a decision before an agent can continue.' }
  if (stage === 'done')
    return { title: 'Task completed', body: 'No more sessions running. Find the history in the Stages tab.' }
  if (stage === 'cancelled')
    return { title: 'Task cancelled', body: 'No sessions — the task was cancelled.' }
  return { title: 'No active session', body: 'Between stage runs — the next agent will start shortly.' }
})

function onSessionMessageSent(msg: OutputMessage) {
  sessionLocalMessages.value.push(msg)
  nextTick(() => sessionChatRef.value?.scrollToBottom())
}

function isTerminal(stage: string | undefined) {
  // `failed` is intentionally excluded — failed tasks are actionable
  // (Retry / Analyze) rather than terminal.
  return stage === 'done' || stage === 'cancelled'
}
function isFailedRun(task: { latestStageRunStatus?: string | null } | null | undefined) {
  return task?.latestStageRunStatus === 'failed'
}

// Per approval-gate, tell the UI which prior stage_run holds the artifact
// to review and what label belongs on the green "Approve" button.
const approvalMeta = computed(() => {
  const stage = props.task?.currentStage
  if (stage === 'approval1')
    return { label: 'Approve Plan', reviewStage: 'planning' as const, sectionTitle: 'Plan for Approval' }
  if (stage === 'approval2')
    return { label: 'Approve Concept', reviewStage: 'umsetzungskonzept' as const, sectionTitle: 'Concept for Approval' }
  return null
})

const approvalContent = computed(() => {
  if (!approvalMeta.value)
    return null
  const reviewStage = approvalMeta.value.reviewStage
  // Walk newest → oldest for the last successful run of the reviewed stage.
  // stageRuns.value is ordered ascending by start time from fetchStageRuns.
  for (let i = stageRuns.value.length - 1; i >= 0; i--) {
    const run = stageRuns.value[i]
    if (run.stage === reviewStage && run.status === 'done' && run.output)
      return run.output
  }
  return null
})

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
  stageRuns.value = await fetchStageRuns(props.task.id)
  permissions.value = await fetchTaskPermissions(props.task.id)
  pendingRequests.value = await fetchPendingPermissionRequests(props.task.id)
  feedbackHistory.value = await fetchTaskFeedback(props.task.id)
}

async function onRequestChanges() {
  const text = feedbackInput.value.trim()
  if (!text || !props.task)
    return
  await handleAction(async () => {
    await requestChanges(props.task!.id, text)
    feedbackInput.value = ''
  })
}

// Reset modal-local state when the user opens a different task.
watch(() => props.task?.id, (id, prevId) => {
  if (id && id !== prevId) {
    activeTab.value = 'overview'
    actionError.value = ''
    feedbackInput.value = ''
    feedbackHistory.value = []
    sessionLocalMessages.value = []
    void loadDetails()
    void loadDependencies()
  }
})

// Auto-switch to session tab when a live agent appears for this task.
// Only fires once per task open (guarded by the activeTab reset above).
watch(pipelineAgent, (agent, prev) => {
  if (agent && !prev && activeTab.value === 'overview')
    activeTab.value = 'session'
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

function onKeydown(e: KeyboardEvent) {
  if (!props.task)
    return
  if (e.key === 'Escape') {
    e.preventDefault()
    emit('close')
  }
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))

function formatDate(iso: string | null): string {
  if (!iso)
    return '—'
  return new Date(iso).toLocaleString()
}

function stageRunStatusClass(status: string): string {
  switch (status) {
    case 'running': return 'bg-blue-50 dark:bg-blue-950/50 text-blue-600 dark:text-blue-400'
    case 'done': return 'bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400'
    case 'failed': return 'bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400'
    case 'on_hold': return 'bg-yellow-50 dark:bg-yellow-950/50 text-yellow-600 dark:text-yellow-400'
    default: return 'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400'
  }
}

function sessionStatusClass(status: string): string {
  switch (status) {
    case 'active': return 'bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400'
    case 'waiting': return 'bg-yellow-50 dark:bg-yellow-950/50 text-yellow-600 dark:text-yellow-400'
    default: return 'bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600'
  }
}
</script>

<template>
  <AppModal :open="!!task" :z-index="1000" @close="emit('close')">
    <div v-if="task" class="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-[860px] max-h-[90vh] flex flex-col overflow-hidden">
      <header class="flex items-center justify-between px-5 py-4 border-b border-slate-200 dark:border-slate-700">
        <div class="flex items-center gap-2.5 flex-wrap">
          <span
            class="text-[10px] uppercase font-mono px-2 py-[3px] rounded"
            :class="{
              'bg-yellow-500/20 text-yellow-500': task.currentStage === 'on_hold',
              'bg-green-400/20 text-green-400': task.currentStage === 'done',
              'bg-red-400/20 text-red-400': task.currentStage === 'cancelled',
              'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400': !['on_hold', 'done', 'cancelled'].includes(task.currentStage ?? ''),
            }"
          >{{ task.currentStage }}</span>
          <span v-if="isFailedRun(task)" class="text-[10px] px-1.5 py-px rounded uppercase ml-auto font-mono bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400" title="Latest stage run failed">
            RUN FAILED
          </span>
          <span class="font-mono text-xs text-blue-600 dark:text-blue-400">{{ task.slug }}</span>
          <h2 class="text-lg font-semibold text-slate-900 dark:text-slate-100">
            {{ task.title }}
          </h2>
        </div>
        <button type="button" class="bg-transparent border-none text-slate-400 dark:text-slate-600 text-2xl cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100" title="Close (Esc)" @click="emit('close')">
          &times;
        </button>
      </header>

      <nav class="flex border-b border-slate-200 dark:border-slate-700 flex-shrink-0">
        <button
          type="button"
          class="px-4 py-2.5 text-xs font-semibold bg-transparent border-none border-b-2 border-transparent cursor-pointer hover:text-slate-700 dark:hover:text-slate-300 transition-colors"
          :class="activeTab === 'overview' ? 'text-blue-600 dark:text-blue-400 border-blue-600 dark:border-blue-400' : 'text-slate-400 dark:text-slate-600'"
          @click="activeTab = 'overview'"
        >
          Overview
        </button>
        <button
          type="button"
          class="px-4 py-2.5 text-xs font-semibold bg-transparent border-none border-b-2 border-transparent cursor-pointer hover:text-slate-700 dark:hover:text-slate-300 transition-colors"
          :class="activeTab === 'session' ? 'text-blue-600 dark:text-blue-400 border-blue-600 dark:border-blue-400' : 'text-slate-400 dark:text-slate-600'"
          @click="activeTab = 'session'"
        >
          Session
          <span
            v-if="pipelineAgent"
            class="inline-block w-1.5 h-1.5 rounded-full ml-[5px] align-middle"
            :class="{
              'bg-green-400 animate-[pulse_2s_ease-in-out_infinite]': pipelineAgent.status === 'active',
              'bg-yellow-500': pipelineAgent.status === 'waiting',
              'bg-slate-400 dark:bg-slate-500': pipelineAgent.status !== 'active' && pipelineAgent.status !== 'waiting',
            }"
          />
        </button>
        <button
          type="button"
          class="px-4 py-2.5 text-xs font-semibold bg-transparent border-none border-b-2 border-transparent cursor-pointer hover:text-slate-700 dark:hover:text-slate-300 transition-colors"
          :class="activeTab === 'stages' ? 'text-blue-600 dark:text-blue-400 border-blue-600 dark:border-blue-400' : 'text-slate-400 dark:text-slate-600'"
          @click="activeTab = 'stages'"
        >
          Stages ({{ stageRuns.length }})
        </button>
        <button
          type="button"
          class="px-4 py-2.5 text-xs font-semibold bg-transparent border-none border-b-2 border-transparent cursor-pointer hover:text-slate-700 dark:hover:text-slate-300 transition-colors"
          :class="activeTab === 'permissions' ? 'text-blue-600 dark:text-blue-400 border-blue-600 dark:border-blue-400' : 'text-slate-400 dark:text-slate-600'"
          @click="activeTab = 'permissions'"
        >
          Permissions ({{ permissions.length }})
        </button>
        <button
          type="button"
          class="px-4 py-2.5 text-xs font-semibold bg-transparent border-none border-b-2 border-transparent cursor-pointer hover:text-slate-700 dark:hover:text-slate-300 transition-colors"
          :class="activeTab === 'audit' ? 'text-blue-600 dark:text-blue-400 border-blue-600 dark:border-blue-400' : 'text-slate-400 dark:text-slate-600'"
          @click="activeTab = 'audit'"
        >
          Audit
        </button>
      </nav>

      <div class="flex-1 overflow-y-auto">
        <!-- Overview tab -->
        <section v-if="activeTab === 'overview'" class="flex-1 overflow-y-auto p-5 flex flex-col gap-4 min-h-0">
          <!-- Latest stage run summary (non-approval states) -->
          <div v-if="!approvalMeta && latestStageRun" class="bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded-md px-3.5 py-3 mb-4">
            <div class="flex items-center gap-2 flex-wrap mb-2">
              <span class="font-mono text-[10px] uppercase bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 px-2 py-0.5 rounded font-semibold">{{ latestStageRun.stage }}</span>
              <span class="text-[10px] text-slate-400 dark:text-slate-600 font-mono">iter {{ latestStageRun.iteration }}</span>
              <span class="text-[10px] px-1.5 py-px rounded uppercase ml-auto font-mono" :class="stageRunStatusClass(latestStageRun.status)">{{ latestStageRun.status }}</span>
              <span class="text-[11px] text-slate-400 dark:text-slate-600 ml-auto">
                {{ formatDate(latestStageRun.startedAt) }}
                <template v-if="latestStageRun.endedAt"> → {{ formatDate(latestStageRun.endedAt) }}</template>
              </span>
            </div>
            <AgentChatStream
              v-if="latestStageRun.status === 'running' && pipelineAgent"
              :agent="pipelineAgent"
              :local-messages="[]"
              class="border-t border-slate-200 dark:border-slate-700 mt-2 pt-3 min-h-[200px] max-h-[40vh] px-0 py-3"
            />
            <div v-else-if="latestRunAgentMessage" class="mt-1.5">
              <div class="text-[10px] uppercase tracking-[0.5px] text-slate-400 dark:text-slate-600 mb-1">
                Agent output
              </div>
              <pre class="font-mono text-[11px] bg-white dark:bg-slate-900 rounded px-3 py-2.5 whitespace-pre-wrap break-words max-h-[300px] overflow-y-auto text-slate-600 dark:text-slate-400 leading-relaxed">{{ latestRunAgentMessage }}</pre>
            </div>
            <details v-else-if="latestStageRun.output">
              <summary class="cursor-pointer text-[11px] text-slate-400 dark:text-slate-600 py-0.5 select-none hover:text-slate-500">
                Stage output
              </summary>
              <StageOutputView :stage="latestStageRun.stage" :output="latestStageRun.output" />
            </details>
          </div>

          <div v-if="approvalMeta" class="bg-green-400/[.08] border border-green-400/35 rounded-md px-3.5 py-3 mb-4">
            <h3 class="text-[11px] uppercase text-green-400 tracking-[0.5px] mb-2">
              {{ approvalMeta.sectionTitle }}
            </h3>
            <StageOutputView
              v-if="approvalContent"
              :stage="approvalMeta.reviewStage"
              :output="approvalContent"
            />
            <div v-else class="text-slate-400 dark:text-slate-600 text-xs text-center py-8">
              No output from stage <code>{{ approvalMeta.reviewStage }}</code> found.
            </div>

            <div v-if="feedbackHistory.length > 0" class="mt-3.5 pt-3 border-t border-green-400/25">
              <h4 class="text-[11px] uppercase tracking-[0.5px] text-slate-400 dark:text-slate-600 mb-2">
                Feedback History
              </h4>
              <div
                v-for="fb in feedbackHistory"
                :key="fb.id"
                class="rounded px-2.5 py-2 mb-1.5"
                :class="fb.resolvedAt !== null ? 'border-l-2 border-green-400 opacity-70 bg-slate-50 dark:bg-slate-950' : 'border-l-2 border-yellow-500 bg-slate-50 dark:bg-slate-950'"
              >
                <div class="flex items-center gap-2 text-[10px] text-slate-400 dark:text-slate-600 mb-1 flex-wrap">
                  <span class="font-mono bg-slate-100 dark:bg-slate-800 px-[5px] py-px rounded text-blue-600 dark:text-blue-400 font-semibold">#{{ fb.iteration }}</span>
                  <span class="font-mono">{{ fb.stage }}</span>
                  <span class="ml-auto">{{ formatDate(fb.createdAt) }}</span>
                  <span v-if="fb.resolvedAt" class="text-[9px] font-bold uppercase px-[5px] py-px rounded bg-green-400/[.18] text-green-400">✓ addressed</span>
                  <span v-else class="text-[9px] font-bold uppercase px-[5px] py-px rounded bg-yellow-500/[.18] text-yellow-500">open</span>
                </div>
                <p class="text-xs leading-relaxed text-slate-600 dark:text-slate-400 whitespace-pre-wrap">
                  {{ fb.feedback }}
                </p>
              </div>
            </div>

            <div class="mt-3.5 pt-3 border-t border-green-400/25">
              <label for="feedback-textarea" class="block text-[11px] uppercase tracking-[0.5px] text-slate-400 dark:text-slate-600 mb-1.5">Request Changes</label>
              <textarea
                id="feedback-textarea"
                v-model="feedbackInput"
                class="w-full bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-2 text-sm text-slate-900 dark:text-slate-100 placeholder:text-slate-400 dark:placeholder:text-slate-600 focus:outline-none focus:border-blue-500 resize-y leading-snug"
                rows="3"
                placeholder="What should the agent change in the next iteration?"
                :disabled="isActing"
                maxlength="4000"
              />
              <div class="flex justify-between items-center mt-1.5">
                <span class="text-[10px] text-slate-400 dark:text-slate-600 font-mono">{{ feedbackInput.length }} / 4000</span>
                <button
                  type="button"
                  class="border-none rounded px-3.5 py-1.5 text-xs font-semibold cursor-pointer font-sans disabled:opacity-40 disabled:cursor-not-allowed bg-red-600 text-white hover:brightness-110"
                  :disabled="isActing || feedbackInput.trim().length === 0"
                  @click="onRequestChanges"
                >
                  Request Changes
                </button>
              </div>
            </div>
          </div>
          <dl class="grid grid-cols-[auto_1fr] gap-y-1.5 gap-x-4 text-[13px] mb-4">
            <div class="contents">
              <dt class="text-slate-400 dark:text-slate-600 text-[11px] uppercase tracking-[0.5px]">
                CWD
              </dt><dd class="font-mono text-xs text-slate-900 dark:text-slate-100">
                {{ task.cwd }}
              </dd>
            </div>
            <div v-if="task.worktreePath" class="contents">
              <dt class="text-slate-400 dark:text-slate-600 text-[11px] uppercase tracking-[0.5px]">
                Worktree
              </dt><dd class="font-mono text-xs text-slate-900 dark:text-slate-100">
                {{ task.worktreePath }}
              </dd>
            </div>
            <div v-if="task.sourceBranch" class="contents">
              <dt class="text-slate-400 dark:text-slate-600 text-[11px] uppercase tracking-[0.5px]">
                Source
              </dt><dd class="font-mono text-xs text-slate-900 dark:text-slate-100">
                {{ task.sourceBranch }}
              </dd>
            </div>
            <div v-if="task.targetBranch" class="contents">
              <dt class="text-slate-400 dark:text-slate-600 text-[11px] uppercase tracking-[0.5px]">
                Target
              </dt><dd class="font-mono text-xs text-slate-900 dark:text-slate-100">
                {{ task.targetBranch }}
              </dd>
            </div>
            <div class="contents">
              <dt class="text-slate-400 dark:text-slate-600 text-[11px] uppercase tracking-[0.5px]">
                Max Iter
              </dt><dd class="text-slate-900 dark:text-slate-100">
                {{ task.maxIterations }}
              </dd>
            </div>
            <div v-if="task.tokenBudget" class="contents">
              <dt class="text-slate-400 dark:text-slate-600 text-[11px] uppercase tracking-[0.5px]">
                Token Budget
              </dt><dd class="text-slate-900 dark:text-slate-100">
                {{ task.tokenBudget.toLocaleString() }}
              </dd>
            </div>
            <div class="contents">
              <dt class="text-slate-400 dark:text-slate-600 text-[11px] uppercase tracking-[0.5px]">
                Created
              </dt><dd class="text-slate-900 dark:text-slate-100">
                {{ formatDate(task.createdAt) }}
              </dd>
            </div>
            <div v-if="task.parentTaskId" class="contents">
              <dt class="text-slate-400 dark:text-slate-600 text-[11px] uppercase tracking-[0.5px]">
                Parent
              </dt><dd class="font-mono text-xs text-slate-900 dark:text-slate-100">
                {{ task.parentTaskId }}
              </dd>
            </div>
          </dl>
          <details v-if="task.description" class="mt-3 text-xs">
            <summary class="cursor-pointer text-slate-400 dark:text-slate-600 py-1.5 select-none hover:text-slate-500 dark:hover:text-slate-400">
              Origin Prompt
            </summary>
            <div class="mt-1.5 px-3 py-3 bg-slate-50 dark:bg-slate-950 rounded-md text-[13px] leading-relaxed whitespace-pre-wrap text-slate-600 dark:text-slate-400">
              {{ task.description }}
            </div>
          </details>

          <!-- Dependencies section -->
          <section class="mt-4 border-t border-slate-200 dark:border-slate-700 pt-3">
            <h4 class="text-sm font-semibold text-slate-500 dark:text-slate-400 mb-2">
              Abhängigkeiten
            </h4>

            <div v-if="dependencies.length > 0" class="mb-2.5">
              <p class="text-[11px] text-slate-400 dark:text-slate-600 font-semibold uppercase tracking-[0.3px] mb-1">
                Wartet auf:
              </p>
              <div
                v-for="dep in dependencies"
                :key="dep.id"
                class="flex items-center gap-2 py-1 text-xs"
              >
                <span class="flex-1 text-slate-900 dark:text-slate-100">{{ dep.dependsOnTitle }}</span>
                <span
                  class="px-1.5 py-px rounded text-[10px] font-mono"
                  :class="dep.dependsOnStage === dep.requiredStage
                    ? 'bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400 border border-green-300 dark:border-green-700'
                    : 'bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400 border border-red-300 dark:border-red-700/50'"
                >{{ dep.dependsOnStage }}</span>
                <span class="text-[10px] text-slate-400 dark:text-slate-600 font-mono">on cancel: {{ dep.onCancelAction }}</span>
                <button type="button" class="bg-transparent border-none cursor-pointer text-slate-400 dark:text-slate-600 px-1 py-px text-[10px] rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400" title="Remove dependency" @click="handleRemoveDependency(dep.id)">
                  ✕
                </button>
              </div>
            </div>

            <div v-if="dependents.length > 0" class="mb-2.5">
              <p class="text-[11px] text-slate-400 dark:text-slate-600 font-semibold uppercase tracking-[0.3px] mb-1">
                Wird benötigt von:
              </p>
              <div v-for="dep in dependents" :key="dep.id" class="flex items-center gap-2 py-1 text-xs">
                <span class="flex-1 text-slate-900 dark:text-slate-100">{{ dep.taskTitle || dep.taskId }}</span>
              </div>
            </div>

            <form class="flex gap-1.5 items-center flex-wrap mt-2" @submit.prevent="handleAddDependency">
              <input
                v-model="newDepId"
                class="flex-1 min-w-0 px-2 py-1 border border-slate-200 dark:border-slate-700 rounded bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100 text-xs"
                placeholder="Vorgänger Task-ID"
                :disabled="isAddingDep"
              >
              <select v-model="newDepStage" class="px-1.5 py-1 border border-slate-200 dark:border-slate-700 rounded bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100 text-[11px]">
                <option value="done">
                  Done
                </option>
                <option value="cancelled">
                  Cancelled
                </option>
              </select>
              <select v-model="newDepCancelAction" class="px-1.5 py-1 border border-slate-200 dark:border-slate-700 rounded bg-white dark:bg-slate-800 text-slate-900 dark:text-slate-100 text-[11px]">
                <option value="on_hold">
                  On Hold (bei Cancel)
                </option>
                <option value="cancel">
                  Cancel (bei Cancel)
                </option>
                <option value="start">
                  Start (bei Cancel)
                </option>
              </select>
              <button type="submit" class="px-2.5 py-1 bg-blue-600 text-white border-none rounded text-xs cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed hover:brightness-110" :disabled="isAddingDep || !newDepId.trim()">
                Hinzufügen
              </button>
            </form>
            <p v-if="depError" class="text-[11px] text-red-600 dark:text-red-400 mt-1">
              {{ depError }}
            </p>
          </section>
        </section>

        <!-- Session tab: live chat stream against the active stage-run's agent -->
        <section v-if="activeTab === 'session'" class="flex flex-col gap-2 p-0 h-full min-h-[400px]">
          <div v-if="!task.activeSessionId" class="text-slate-400 dark:text-slate-600 text-xs text-center px-5 py-[60px]">
            <p class="text-[13px] font-semibold text-slate-500 dark:text-slate-400 mb-1.5">
              {{ sessionEmptyHint.title }}
            </p>
            <p class="text-xs text-slate-400 dark:text-slate-600 max-w-[420px] mx-auto leading-relaxed">
              {{ sessionEmptyHint.body }}
            </p>
          </div>
          <template v-else>
            <CrossLinkBanner
              v-if="pipelineAgent"
              label="Running as session in"
              :target-name="pipelineAgent.projectName"
              button-text="Open session →"
              @click="emit('navigate', pipelineAgent)"
            />
            <div class="flex items-center gap-2.5 px-5 py-2.5 border-b border-slate-200 dark:border-slate-700 flex-shrink-0">
              <span class="text-[10px] uppercase tracking-[0.5px] text-slate-400 dark:text-slate-600">Active Session</span>
              <code class="font-mono text-[11px] bg-slate-100 dark:bg-slate-800 px-1.5 py-px rounded text-blue-600 dark:text-blue-400">{{ task.activeSessionId.slice(0, 8) }}</code>
              <span
                v-if="pipelineAgent"
                class="text-[10px] uppercase px-2 py-0.5 rounded font-bold ml-auto"
                :class="sessionStatusClass(pipelineAgent.status)"
              >
                {{ pipelineAgent.status }}
              </span>
              <span v-else class="text-[10px] uppercase px-2 py-0.5 rounded font-bold ml-auto bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600">
                offline
              </span>
            </div>
            <AgentChatStream
              ref="sessionChatRef"
              :agent="pipelineAgent"
              :local-messages="sessionLocalMessages"
              class="flex-1 px-5 py-3 min-h-[280px] max-h-[50vh]"
            />
            <PromptInput
              v-if="pipelineAgent"
              :agent="pipelineAgent"
              variant="full"
              @message-sent="onSessionMessageSent"
            />
            <p v-else class="px-5 py-2.5 text-xs text-slate-400 dark:text-slate-600 border-t border-slate-200 dark:border-slate-700">
              The agent is not currently active — you can chat here once a new stage run starts.
            </p>
          </template>
        </section>

        <!-- Stages tab -->
        <section v-if="activeTab === 'stages'" class="p-5">
          <div v-if="stageRuns.length === 0" class="text-slate-400 dark:text-slate-600 text-xs text-center py-8">
            No stage runs yet.
          </div>
          <div v-for="run in stageRuns" v-else :key="run.id" class="px-3 py-2.5 bg-slate-50 dark:bg-slate-950 rounded-md mb-2">
            <div class="flex items-center gap-2.5 mb-1">
              <span class="font-semibold text-xs text-slate-900 dark:text-slate-100">{{ run.stage }}</span>
              <span class="font-mono text-[11px] text-slate-400 dark:text-slate-600">iter {{ run.iteration }}</span>
              <span class="text-[10px] px-1.5 py-px rounded uppercase ml-auto font-mono" :class="stageRunStatusClass(run.status)">{{ run.status }}</span>
            </div>
            <div v-if="run.sessionName" class="text-[11px] text-slate-400 dark:text-slate-600 mt-0.5">
              session: <code>{{ run.sessionName }}</code>
            </div>
            <div class="text-[11px] text-slate-400 dark:text-slate-600 mt-0.5">
              started {{ formatDate(run.startedAt) }} · ended {{ formatDate(run.endedAt) }}
            </div>
            <details v-if="run.output" class="mt-1.5 text-[11px]">
              <summary class="cursor-pointer text-slate-400 dark:text-slate-600 py-0.5 select-none hover:text-slate-500">
                Output
              </summary>
              <StageOutputView :stage="run.stage" :output="run.output" />
            </details>
          </div>
        </section>

        <!-- Permissions tab -->
        <section v-if="activeTab === 'permissions'" class="p-5">
          <div v-if="pendingRequests.length > 0" class="mb-4">
            <h3 class="text-[11px] uppercase text-slate-400 dark:text-slate-600 mb-2 tracking-[0.5px]">
              Pending runtime requests
            </h3>
            <div v-for="req in pendingRequests" :key="req.id" class="bg-yellow-50/50 dark:bg-yellow-950/20 border border-yellow-300/60 dark:border-yellow-700/40 rounded-md p-3 mb-2">
              <div class="flex gap-2.5 items-baseline">
                <strong>{{ req.tool }}</strong>
                <span v-if="req.pattern" class="font-mono text-xs text-slate-900 dark:text-slate-100">{{ req.pattern }}</span>
              </div>
              <div v-if="req.reason" class="text-[11px] text-slate-400 dark:text-slate-600 my-1">
                {{ req.reason }}
              </div>
              <div class="flex gap-1.5 mt-1.5">
                <button
                  type="button"
                  class="border-none rounded px-2.5 py-1 text-[11px] font-semibold cursor-pointer font-sans disabled:opacity-40 disabled:cursor-not-allowed bg-green-600 text-white hover:brightness-110"
                  :disabled="isActing"
                  @click="onResolve(req, 'granted')"
                >
                  Grant
                </button>
                <button
                  type="button"
                  class="border-none rounded px-2.5 py-1 text-[11px] font-semibold cursor-pointer font-sans disabled:opacity-40 disabled:cursor-not-allowed bg-red-600 text-white hover:brightness-110"
                  :disabled="isActing"
                  @click="onResolve(req, 'denied')"
                >
                  Deny
                </button>
              </div>
            </div>
          </div>

          <!-- Manual permission grant form -->
          <div class="border border-slate-200 dark:border-slate-700 rounded-md px-3.5 py-3 mb-3 bg-slate-50 dark:bg-slate-950">
            <h3 class="text-[11px] uppercase text-slate-400 dark:text-slate-600 mb-1 tracking-[0.5px]">
              Grant a tool permission
            </h3>
            <p class="text-[11px] text-slate-400 dark:text-slate-600 mb-2.5 leading-relaxed">
              Pre-approve a tool before Retry — useful when the agent hit a permission wall.
              Examples: <code class="bg-slate-100 dark:bg-slate-800 px-[3px] rounded text-[11px]">Write</code>, <code class="bg-slate-100 dark:bg-slate-800 px-[3px] rounded text-[11px]">Bash</code> with pattern <code class="bg-slate-100 dark:bg-slate-800 px-[3px] rounded text-[11px]">npm run *</code>
            </p>
            <div class="flex gap-2 items-center">
              <input
                v-model="newPermTool"
                class="flex-1 min-w-0 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded px-2 py-1.5 text-slate-900 dark:text-slate-100 text-xs focus:outline-none focus:border-blue-500"
                placeholder="Tool (e.g. Bash, Write)"
                @keydown.enter="onGrantPermission"
              >
              <input
                v-model="newPermPattern"
                class="flex-1 min-w-0 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded px-2 py-1.5 text-slate-900 dark:text-slate-100 text-xs focus:outline-none focus:border-blue-500"
                placeholder="Pattern (optional, e.g. npm run *)"
                @keydown.enter="onGrantPermission"
              >
              <button
                type="button"
                class="border-none rounded px-2.5 py-1 text-[11px] font-semibold cursor-pointer font-sans disabled:opacity-40 disabled:cursor-not-allowed bg-green-600 text-white hover:brightness-110"
                :disabled="isGranting || !newPermTool.trim()"
                @click="onGrantPermission"
              >
                Grant
              </button>
            </div>
            <p v-if="permError" class="text-[11px] text-red-400 mt-1.5">
              {{ permError }}
            </p>
          </div>

          <div v-if="permissions.length > 0">
            <h3 class="text-[11px] uppercase text-slate-400 dark:text-slate-600 mb-2 tracking-[0.5px]">
              Granted permissions
            </h3>
            <div v-for="p in permissions" :key="p.id" class="flex gap-2.5 px-2.5 py-1.5 text-xs border-b border-slate-200 dark:border-slate-700">
              <span class="font-semibold text-slate-900 dark:text-slate-100 min-w-[80px]">{{ p.tool }}</span>
              <span v-if="p.pattern" class="font-mono text-slate-400 dark:text-slate-600 flex-1">{{ p.pattern }}</span>
              <span class="text-[10px] text-slate-400 dark:text-slate-600 uppercase">{{ p.preApproved ? 'pre-approved' : 'runtime' }}</span>
              <span class="text-[10px] text-slate-400 dark:text-slate-600">{{ p.decidedBy }}</span>
            </div>
          </div>
          <div v-if="permissions.length === 0 && pendingRequests.length === 0" class="text-slate-400 dark:text-slate-600 text-xs text-center py-8">
            No permissions granted yet.
          </div>
        </section>

        <!-- Audit tab -->
        <section v-if="activeTab === 'audit'" class="p-5">
          <div class="text-slate-400 dark:text-slate-600 text-xs text-center py-8">
            Audit log viewer — Phase 6.
          </div>
        </section>
      </div>

      <footer class="px-5 py-3 border-t border-slate-200 dark:border-slate-700 flex-shrink-0">
        <p v-if="actionError" class="text-red-600 dark:text-red-400 text-xs mb-2">
          {{ actionError }}
        </p>
        <p v-if="analysisInfo" class="text-green-600 dark:text-green-400 text-xs mb-2">
          Analysis agent spawned · PID <code>{{ analysisInfo.pid }}</code> · look for it in the agents list.
        </p>
        <div class="flex gap-2 justify-end">
          <button
            v-if="isFailedRun(task)"
            type="button"
            class="border-none rounded px-3.5 py-1.5 text-xs font-semibold cursor-pointer font-sans disabled:opacity-40 disabled:cursor-not-allowed bg-blue-600 text-white hover:brightness-110"
            :disabled="isActing"
            title="Start a fresh iteration of this stage"
            @click="handleAction(() => retryTask(task!.id))"
          >
            Retry Stage
          </button>
          <button
            v-if="isFailedRun(task)"
            type="button"
            class="border-none rounded px-3.5 py-1.5 text-xs font-semibold cursor-pointer font-sans disabled:opacity-40 disabled:cursor-not-allowed bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 hover:brightness-110"
            :disabled="isActing"
            title="Spawn a standalone Claude session with the failure context attached"
            @click="onAnalyze"
          >
            Analyze Failure
          </button>
          <button
            v-if="approvalMeta"
            type="button"
            class="border-none rounded px-3.5 py-1.5 text-xs font-semibold cursor-pointer font-sans disabled:opacity-40 disabled:cursor-not-allowed bg-green-600 text-white hover:brightness-110"
            :disabled="isActing"
            :title="`Advance past ${task.currentStage} gate`"
            @click="handleAction(() => approveTask(task!.id))"
          >
            {{ approvalMeta.label }}
          </button>
          <button
            v-else-if="!isTerminal(task.currentStage) && !isOnHoldStage && !isFailedRun(task)"
            type="button"
            class="border-none rounded px-3.5 py-1.5 text-xs font-semibold cursor-pointer font-sans disabled:opacity-40 disabled:cursor-not-allowed bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 hover:brightness-110"
            :disabled="isActing"
            title="Manually advance to the next stage (skips approval gates)"
            @click="handleAction(() => progressTask(task!.id))"
          >
            Progress →
          </button>
          <button
            v-if="!isTerminal(task.currentStage)"
            type="button"
            class="border-none rounded px-3.5 py-1.5 text-xs font-semibold cursor-pointer font-sans disabled:opacity-40 disabled:cursor-not-allowed bg-red-600 text-white hover:brightness-110"
            :disabled="isActing"
            title="Stop this task and mark it as cancelled"
            @click="handleAction(() => cancelTask(task!.id))"
          >
            Cancel Task
          </button>
        </div>
      </footer>
    </div>
  </AppModal>
</template>
