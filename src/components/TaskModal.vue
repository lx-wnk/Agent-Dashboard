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
</script>

<template>
  <AppModal :open="!!task" :z-index="1000" @close="emit('close')">
    <div v-if="task" class="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-[860px] max-h-[90vh] flex flex-col overflow-hidden">
      <header class="flex items-center justify-between px-5 py-4 border-b border-slate-200 dark:border-slate-700">
        <div class="flex items-center gap-2.5 flex-wrap">
          <span class="stage-badge" :class="`stage-${task.currentStage}`">{{ task.currentStage }}</span>
          <span v-if="isFailedRun(task)" class="text-[10px] px-1.5 py-px rounded uppercase ml-auto font-mono bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400" title="Latest stage run failed">
            RUN FAILED
          </span>
          <span class="task-slug font-mono text-xs text-blue-600 dark:text-blue-400">{{ task.slug }}</span>
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
          <span v-if="pipelineAgent" class="tab-dot" :class="pipelineAgent.status" />
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
          <div v-if="!approvalMeta && latestStageRun" class="latest-run-card">
            <div class="latest-run-head">
              <span class="stage-label-pill">{{ latestStageRun.stage }}</span>
              <span class="iteration-pill">iter {{ latestStageRun.iteration }}</span>
              <span class="text-[10px] px-1.5 py-px rounded uppercase ml-auto font-mono" :class="`status-${latestStageRun.status}`">{{ latestStageRun.status }}</span>
              <span class="latest-run-times">
                {{ formatDate(latestStageRun.startedAt) }}
                <template v-if="latestStageRun.endedAt"> → {{ formatDate(latestStageRun.endedAt) }}</template>
              </span>
            </div>
            <AgentChatStream
              v-if="latestStageRun.status === 'running' && pipelineAgent"
              :agent="pipelineAgent"
              :local-messages="[]"
              class="overview-live-stream"
            />
            <div v-else-if="latestRunAgentMessage" class="agent-message-block">
              <div class="agent-message-label">
                Agent output
              </div>
              <pre class="agent-message-body">{{ latestRunAgentMessage }}</pre>
            </div>
            <details v-else-if="latestStageRun.output" class="latest-run-output">
              <summary>Stage output</summary>
              <StageOutputView :stage="latestStageRun.stage" :output="latestStageRun.output" />
            </details>
          </div>

          <div v-if="approvalMeta" class="approval-preview">
            <h3>{{ approvalMeta.sectionTitle }}</h3>
            <StageOutputView
              v-if="approvalContent"
              :stage="approvalMeta.reviewStage"
              :output="approvalContent"
            />
            <div v-else class="empty-hint">
              No output from stage <code>{{ approvalMeta.reviewStage }}</code> found.
            </div>

            <div v-if="feedbackHistory.length > 0" class="feedback-thread">
              <h4>Feedback History</h4>
              <div
                v-for="fb in feedbackHistory"
                :key="fb.id"
                class="feedback-entry"
                :class="{ resolved: fb.resolvedAt !== null }"
              >
                <div class="feedback-meta">
                  <span class="feedback-iter">#{{ fb.iteration }}</span>
                  <span class="feedback-stage">{{ fb.stage }}</span>
                  <span class="feedback-date">{{ formatDate(fb.createdAt) }}</span>
                  <span v-if="fb.resolvedAt" class="feedback-status resolved">✓ addressed</span>
                  <span v-else class="feedback-status pending">open</span>
                </div>
                <p class="feedback-text">
                  {{ fb.feedback }}
                </p>
              </div>
            </div>

            <div class="feedback-input-block">
              <label for="feedback-textarea">Request Changes</label>
              <textarea
                id="feedback-textarea"
                v-model="feedbackInput"
                class="w-full bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded px-2.5 py-2 text-sm text-slate-900 dark:text-slate-100 placeholder:text-slate-400 dark:placeholder:text-slate-600 focus:outline-none focus:border-blue-500 resize-y leading-snug"
                rows="3"
                placeholder="What should the agent change in the next iteration?"
                :disabled="isActing"
                maxlength="4000"
              />
              <div class="feedback-input-foot">
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
          <dl class="facts">
            <div>
              <dt>CWD</dt><dd class="mono">
                {{ task.cwd }}
              </dd>
            </div>
            <div v-if="task.worktreePath">
              <dt>Worktree</dt><dd class="mono">
                {{ task.worktreePath }}
              </dd>
            </div>
            <div v-if="task.sourceBranch">
              <dt>Source</dt><dd class="mono">
                {{ task.sourceBranch }}
              </dd>
            </div>
            <div v-if="task.targetBranch">
              <dt>Target</dt><dd class="mono">
                {{ task.targetBranch }}
              </dd>
            </div>
            <div>
              <dt>Max Iter</dt><dd>
                {{ task.maxIterations }}
              </dd>
            </div>
            <div v-if="task.tokenBudget">
              <dt>Token Budget</dt><dd>
                {{ task.tokenBudget.toLocaleString() }}
              </dd>
            </div>
            <div>
              <dt>Created</dt><dd>
                {{ formatDate(task.createdAt) }}
              </dd>
            </div>
            <div v-if="task.parentTaskId">
              <dt>Parent</dt><dd class="mono">
                {{ task.parentTaskId }}
              </dd>
            </div>
          </dl>
          <details v-if="task.description" class="description-collapsible">
            <summary>Origin Prompt</summary>
            <div class="description">
              {{ task.description }}
            </div>
          </details>

          <!-- Dependencies section -->
          <section class="mt-4 border-t border-slate-200 dark:border-slate-700 pt-3">
            <h4 class="text-sm font-semibold text-slate-500 dark:text-slate-400 mb-2">
              Abhängigkeiten
            </h4>

            <div v-if="dependencies.length > 0" class="dep-list">
              <p class="dep-subheading">
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
                <span class="dep-action-hint">on cancel: {{ dep.onCancelAction }}</span>
                <button type="button" class="bg-transparent border-none cursor-pointer text-slate-400 dark:text-slate-600 px-1 py-px text-[10px] rounded hover:bg-red-50 dark:hover:bg-red-950/30 hover:text-red-600 dark:hover:text-red-400" title="Remove dependency" @click="handleRemoveDependency(dep.id)">
                  ✕
                </button>
              </div>
            </div>

            <div v-if="dependents.length > 0" class="dep-list">
              <p class="dep-subheading">
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
        <section v-if="activeTab === 'session'" class="tab-content session-tab">
          <div v-if="!task.activeSessionId" class="empty-hint session-empty">
            <p class="session-empty-title">
              {{ sessionEmptyHint.title }}
            </p>
            <p class="session-empty-body">
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
            <div class="session-header">
              <span class="session-label">Active Session</span>
              <code class="session-id">{{ task.activeSessionId.slice(0, 8) }}</code>
              <span v-if="pipelineAgent" class="session-status" :class="pipelineAgent.status">
                {{ pipelineAgent.status }}
              </span>
              <span v-else class="session-status offline">
                offline
              </span>
            </div>
            <AgentChatStream
              ref="sessionChatRef"
              :agent="pipelineAgent"
              :local-messages="sessionLocalMessages"
              class="session-stream"
            />
            <PromptInput
              v-if="pipelineAgent"
              :agent="pipelineAgent"
              variant="full"
              @message-sent="onSessionMessageSent"
            />
            <p v-else class="session-hint">
              The agent is not currently active — you can chat here once a new stage run starts.
            </p>
          </template>
        </section>

        <!-- Stages tab -->
        <section v-if="activeTab === 'stages'" class="tab-content">
          <div v-if="stageRuns.length === 0" class="empty-hint">
            No stage runs yet.
          </div>
          <div v-for="run in stageRuns" v-else :key="run.id" class="px-3 py-2.5 bg-slate-50 dark:bg-slate-950 rounded-md mb-2">
            <div class="flex items-center gap-2.5 mb-1">
              <span class="font-semibold text-xs text-slate-900 dark:text-slate-100">{{ run.stage }}</span>
              <span class="font-mono text-[11px] text-slate-400 dark:text-slate-600">iter {{ run.iteration }}</span>
              <span class="text-[10px] px-1.5 py-px rounded uppercase ml-auto font-mono" :class="`status-${run.status}`">{{ run.status }}</span>
            </div>
            <div v-if="run.sessionName" class="text-[11px] text-slate-400 dark:text-slate-600 mt-0.5">
              session: <code>{{ run.sessionName }}</code>
            </div>
            <div class="text-[11px] text-slate-400 dark:text-slate-600 mt-0.5">
              started {{ formatDate(run.startedAt) }} · ended {{ formatDate(run.endedAt) }}
            </div>
            <details v-if="run.output" class="stage-output">
              <summary>Output</summary>
              <StageOutputView :stage="run.stage" :output="run.output" />
            </details>
          </div>
        </section>

        <!-- Permissions tab -->
        <section v-if="activeTab === 'permissions'" class="tab-content">
          <div v-if="pendingRequests.length > 0" class="pending-section">
            <h3>Pending runtime requests</h3>
            <div v-for="req in pendingRequests" :key="req.id" class="bg-yellow-50/50 dark:bg-yellow-950/20 border border-yellow-300/60 dark:border-yellow-700/40 rounded-md p-3 mb-2">
              <div class="perm-request-head">
                <strong>{{ req.tool }}</strong>
                <span v-if="req.pattern" class="mono">{{ req.pattern }}</span>
              </div>
              <div v-if="req.reason" class="perm-reason">
                {{ req.reason }}
              </div>
              <div class="perm-actions">
                <button
                  type="button"
                  class="border-none rounded px-3.5 py-1.5 text-xs font-semibold cursor-pointer font-sans disabled:opacity-40 disabled:cursor-not-allowed px-2.5 py-1 text-[11px] bg-green-600 text-white hover:brightness-110"
                  :disabled="isActing"
                  @click="onResolve(req, 'granted')"
                >
                  Grant
                </button>
                <button
                  type="button"
                  class="border-none rounded px-3.5 py-1.5 text-xs font-semibold cursor-pointer font-sans disabled:opacity-40 disabled:cursor-not-allowed px-2.5 py-1 text-[11px] bg-red-600 text-white hover:brightness-110"
                  :disabled="isActing"
                  @click="onResolve(req, 'denied')"
                >
                  Deny
                </button>
              </div>
            </div>
          </div>

          <!-- Manual permission grant form -->
          <div class="perm-grant-form">
            <h3>Grant a tool permission</h3>
            <p class="perm-grant-hint">
              Pre-approve a tool before Retry — useful when the agent hit a permission wall.
              Examples: <code>Write</code>, <code>Bash</code> with pattern <code>npm run *</code>
            </p>
            <div class="perm-grant-row">
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
                class="border-none rounded px-3.5 py-1.5 text-xs font-semibold cursor-pointer font-sans disabled:opacity-40 disabled:cursor-not-allowed px-2.5 py-1 text-[11px] bg-green-600 text-white hover:brightness-110"
                :disabled="isGranting || !newPermTool.trim()"
                @click="onGrantPermission"
              >
                Grant
              </button>
            </div>
            <p v-if="permError" class="perm-error">
              {{ permError }}
            </p>
          </div>

          <div v-if="permissions.length > 0">
            <h3>Granted permissions</h3>
            <div v-for="p in permissions" :key="p.id" class="perm-row">
              <span class="perm-tool">{{ p.tool }}</span>
              <span v-if="p.pattern" class="perm-pattern">{{ p.pattern }}</span>
              <span class="perm-type">{{ p.preApproved ? 'pre-approved' : 'runtime' }}</span>
              <span class="perm-decided">{{ p.decidedBy }}</span>
            </div>
          </div>
          <div v-if="permissions.length === 0 && pendingRequests.length === 0" class="empty-hint">
            No permissions granted yet.
          </div>
        </section>

        <!-- Audit tab -->
        <section v-if="activeTab === 'audit'" class="tab-content">
          <div class="empty-hint">
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

<style scoped>
.stage-badge {
  font-size: 10px;
  text-transform: uppercase;
  background: theme('colors.slate.100');
  padding: 3px 8px;
  border-radius: 4px;
  font-family: var(--font-mono);
  color: theme('colors.slate.500');
}
:is(.dark) .stage-badge {
  background: theme('colors.slate.800');
  color: theme('colors.slate.400');
}
.stage-badge.stage-on_hold { background: rgba(234, 179, 8, 0.2); color: rgb(234, 179, 8); }
.stage-badge.stage-done { background: rgba(74, 222, 128, 0.2); color: #4ade80; }
.stage-badge.stage-cancelled { background: rgba(248, 113, 113, 0.2); color: #f87171; }

.tab-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  margin-left: 5px;
  vertical-align: middle;
  background: var(--text-muted, #94a3b8);
}
.tab-dot.active { background: #4ade80; animation: pulse 2s ease-in-out infinite; }
.tab-dot.waiting { background: rgb(234, 179, 8); }
.tab-dot.idle { background: var(--text-muted, #94a3b8); }
@keyframes pulse {
  0%, 100% { opacity: 0.5; }
  50% { opacity: 1; }
}

.session-tab {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 0;
  height: 100%;
  min-height: 400px;
}
.session-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 20px 6px;
  border-bottom: 1px solid theme('colors.slate.200');
  flex-shrink: 0;
}
:is(.dark) .session-header { border-bottom-color: theme('colors.slate.700'); }
.session-label {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: theme('colors.slate.400');
}
:is(.dark) .session-label { color: theme('colors.slate.600'); }
.session-id {
  font-family: var(--font-mono);
  font-size: 11px;
  background: theme('colors.slate.100');
  padding: 1px 6px;
  border-radius: 3px;
  color: theme('colors.blue.600');
}
:is(.dark) .session-id { background: theme('colors.slate.800'); color: theme('colors.blue.400'); }
.session-status {
  font-size: 10px;
  text-transform: uppercase;
  padding: 2px 8px;
  border-radius: 4px;
  font-weight: 700;
  margin-left: auto;
}
.session-status.active { background: rgba(74, 222, 128, 0.2); color: #4ade80; }
.session-status.waiting { background: rgba(234, 179, 8, 0.2); color: rgb(234, 179, 8); }
.session-status.idle,
.session-status.offline { background: theme('colors.slate.100'); color: theme('colors.slate.400'); }
:is(.dark) .session-status.idle,
:is(.dark) .session-status.offline { background: theme('colors.slate.800'); color: theme('colors.slate.600'); }
.session-stream {
  flex: 1;
  padding: 12px 20px;
  min-height: 280px;
  max-height: 50vh;
}
.overview-live-stream {
  padding: 12px 0;
  min-height: 200px;
  max-height: 40vh;
  border-top: 1px solid theme('colors.slate.200');
  margin-top: 8px;
}
:is(.dark) .overview-live-stream { border-top-color: theme('colors.slate.700'); }
.session-empty {
  padding: 60px 20px;
}
.session-empty-title {
  font-size: 13px;
  font-weight: 600;
  color: theme('colors.slate.500');
  margin-bottom: 6px;
}
:is(.dark) .session-empty-title { color: theme('colors.slate.400'); }
.session-empty-body {
  font-size: 12px;
  color: theme('colors.slate.400');
  max-width: 420px;
  margin: 0 auto;
  line-height: 1.5;
}
:is(.dark) .session-empty-body { color: theme('colors.slate.600'); }
.session-hint {
  padding: 10px 20px;
  font-size: 12px;
  color: theme('colors.slate.400');
  border-top: 1px solid theme('colors.slate.200');
}
:is(.dark) .session-hint { color: theme('colors.slate.600'); border-top-color: theme('colors.slate.700'); }

.tab-content { padding: 18px 20px; }
.empty-hint { color: theme('colors.slate.400'); font-size: 12px; text-align: center; padding: 32px 0; }
:is(.dark) .empty-hint { color: theme('colors.slate.600'); }

.facts {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 6px 16px;
  font-size: 13px;
  margin-bottom: 16px;
}
.facts > div { display: contents; }
.facts dt {
  color: theme('colors.slate.400');
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
:is(.dark) .facts dt { color: theme('colors.slate.600'); }
.facts dd { color: theme('colors.slate.900'); }
:is(.dark) .facts dd { color: theme('colors.slate.100'); }
.mono { font-family: var(--font-mono); font-size: 12px; }

.description {
  padding: 12px;
  background: theme('colors.slate.50');
  border-radius: 6px;
  font-size: 13px;
  line-height: 1.5;
  white-space: pre-wrap;
  color: theme('colors.slate.600');
}
:is(.dark) .description { background: theme('colors.slate.950'); color: theme('colors.slate.400'); }
.description-collapsible {
  margin-top: 12px;
  font-size: 12px;
}
.description-collapsible > summary {
  cursor: pointer;
  color: theme('colors.slate.400');
  padding: 6px 0;
  user-select: none;
}
:is(.dark) .description-collapsible > summary { color: theme('colors.slate.600'); }
.description-collapsible > summary:hover { color: theme('colors.slate.500'); }
:is(.dark) .description-collapsible > summary:hover { color: theme('colors.slate.400'); }
.description-collapsible[open] > summary { margin-bottom: 6px; }

.latest-run-card {
  background: theme('colors.slate.50');
  border: 1px solid theme('colors.slate.200');
  border-radius: 6px;
  padding: 12px 14px;
  margin-bottom: 16px;
}
:is(.dark) .latest-run-card { background: theme('colors.slate.950'); border-color: theme('colors.slate.700'); }
.latest-run-head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}
.stage-label-pill {
  font-family: var(--font-mono);
  font-size: 10px;
  text-transform: uppercase;
  background: theme('colors.slate.100');
  color: theme('colors.slate.500');
  padding: 2px 8px;
  border-radius: 4px;
  font-weight: 600;
}
:is(.dark) .stage-label-pill { background: theme('colors.slate.800'); color: theme('colors.slate.400'); }
.iteration-pill {
  font-size: 10px;
  color: theme('colors.slate.400');
  font-family: var(--font-mono);
}
:is(.dark) .iteration-pill { color: theme('colors.slate.600'); }
.latest-run-times {
  font-size: 11px;
  color: theme('colors.slate.400');
  margin-left: auto;
}
:is(.dark) .latest-run-times { color: theme('colors.slate.600'); }
.agent-message-block { margin-top: 6px; }
.agent-message-label {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: theme('colors.slate.400');
  margin-bottom: 4px;
}
:is(.dark) .agent-message-label { color: theme('colors.slate.600'); }
.agent-message-body {
  font-family: var(--font-mono);
  font-size: 11px;
  background: theme('colors.white');
  border-radius: 4px;
  padding: 10px 12px;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 300px;
  overflow-y: auto;
  color: theme('colors.slate.600');
  line-height: 1.5;
}
:is(.dark) .agent-message-body { background: theme('colors.slate.900'); color: theme('colors.slate.400'); }
.latest-run-output > summary {
  cursor: pointer;
  font-size: 11px;
  color: theme('colors.slate.400');
  padding: 2px 0;
  user-select: none;
}
:is(.dark) .latest-run-output > summary { color: theme('colors.slate.600'); }
.latest-run-output > summary:hover { color: theme('colors.slate.500'); }
.latest-run-output[open] > summary { margin-bottom: 8px; }

.approval-preview {
  background: rgba(74, 222, 128, 0.08);
  border: 1px solid rgba(74, 222, 128, 0.35);
  border-radius: 6px;
  padding: 12px 14px;
  margin-bottom: 16px;
}
.approval-preview h3 {
  font-size: 11px;
  text-transform: uppercase;
  color: #4ade80;
  letter-spacing: 0.5px;
  margin-bottom: 8px;
}
.approval-preview pre {
  background: theme('colors.slate.50');
  padding: 10px;
  border-radius: 4px;
  font-size: 11px;
  max-height: 360px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
}
:is(.dark) .approval-preview pre { background: theme('colors.slate.950'); }
.approval-preview code {
  font-family: var(--font-mono);
  background: theme('colors.slate.100');
  padding: 1px 4px;
  border-radius: 3px;
}
:is(.dark) .approval-preview code { background: theme('colors.slate.800'); }
.feedback-thread {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid rgba(74, 222, 128, 0.25);
}
.feedback-thread h4 {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: theme('colors.slate.400');
  margin-bottom: 8px;
}
:is(.dark) .feedback-thread h4 { color: theme('colors.slate.600'); }
.feedback-entry {
  background: theme('colors.slate.50');
  border-left: 2px solid rgb(234, 179, 8);
  border-radius: 4px;
  padding: 8px 10px;
  margin-bottom: 6px;
}
:is(.dark) .feedback-entry { background: theme('colors.slate.950'); }
.feedback-entry.resolved {
  border-left-color: #4ade80;
  opacity: 0.7;
}
.feedback-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 10px;
  color: theme('colors.slate.400');
  margin-bottom: 4px;
  flex-wrap: wrap;
}
:is(.dark) .feedback-meta { color: theme('colors.slate.600'); }
.feedback-iter {
  font-family: var(--font-mono);
  background: theme('colors.slate.100');
  padding: 1px 5px;
  border-radius: 3px;
  color: theme('colors.blue.600');
  font-weight: 600;
}
:is(.dark) .feedback-iter { background: theme('colors.slate.800'); color: theme('colors.blue.400'); }
.feedback-stage { font-family: var(--font-mono); }
.feedback-date { margin-left: auto; }
.feedback-status {
  font-weight: 700;
  text-transform: uppercase;
  font-size: 9px;
  padding: 1px 5px;
  border-radius: 3px;
}
.feedback-status.resolved { background: rgba(74, 222, 128, 0.18); color: #4ade80; }
.feedback-status.pending { background: rgba(234, 179, 8, 0.18); color: rgb(234, 179, 8); }
.feedback-text {
  font-size: 12px;
  line-height: 1.5;
  color: theme('colors.slate.600');
  white-space: pre-wrap;
}
:is(.dark) .feedback-text { color: theme('colors.slate.400'); }
.feedback-input-block {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid rgba(74, 222, 128, 0.25);
}
.feedback-input-block label {
  display: block;
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: theme('colors.slate.400');
  margin-bottom: 6px;
}
:is(.dark) .feedback-input-block label { color: theme('colors.slate.600'); }
.feedback-input-foot {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 6px;
}

.status-running { background: rgba(59, 130, 246, 0.2); color: theme('colors.blue.600'); }
.status-done { background: rgba(74, 222, 128, 0.2); color: #4ade80; }
.status-failed { background: rgba(248, 113, 113, 0.2); color: #f87171; }
.status-on_hold { background: rgba(234, 179, 8, 0.2); color: rgb(234, 179, 8); }
:is(.dark) .status-running { color: theme('colors.blue.400'); }

.stage-output { margin-top: 6px; font-size: 11px; }
.stage-output pre {
  background: theme('colors.slate.100');
  padding: 8px;
  border-radius: 4px;
  overflow-x: auto;
  max-height: 180px;
  overflow-y: auto;
}
:is(.dark) .stage-output pre { background: theme('colors.slate.800'); }

.pending-section h3, .tab-content > h3 {
  font-size: 11px;
  text-transform: uppercase;
  color: theme('colors.slate.400');
  margin-bottom: 8px;
  letter-spacing: 0.5px;
}
:is(.dark) .pending-section h3,
:is(.dark) .tab-content > h3 { color: theme('colors.slate.600'); }
.perm-request-head { display: flex; gap: 10px; align-items: baseline; }
.perm-reason { font-size: 11px; color: theme('colors.slate.400'); margin: 4px 0; }
:is(.dark) .perm-reason { color: theme('colors.slate.600'); }
.perm-actions { display: flex; gap: 6px; margin-top: 6px; }

.perm-row {
  display: flex;
  gap: 10px;
  padding: 6px 10px;
  font-size: 12px;
  border-bottom: 1px solid theme('colors.slate.200');
}
:is(.dark) .perm-row { border-bottom-color: theme('colors.slate.700'); }
.perm-tool { font-weight: 600; color: theme('colors.slate.900'); min-width: 80px; }
:is(.dark) .perm-tool { color: theme('colors.slate.100'); }
.perm-pattern { font-family: var(--font-mono); color: theme('colors.slate.400'); flex: 1; }
:is(.dark) .perm-pattern { color: theme('colors.slate.600'); }
.perm-type { font-size: 10px; color: theme('colors.slate.400'); text-transform: uppercase; }
:is(.dark) .perm-type { color: theme('colors.slate.600'); }
.perm-decided { font-size: 10px; color: theme('colors.slate.400'); }
:is(.dark) .perm-decided { color: theme('colors.slate.600'); }

.perm-grant-form {
  border: 1px solid theme('colors.slate.200');
  border-radius: 6px;
  padding: 12px 14px;
  margin-bottom: 12px;
  background: theme('colors.slate.50');
}
:is(.dark) .perm-grant-form { border-color: theme('colors.slate.700'); background: theme('colors.slate.950'); }
.perm-grant-form h3 { margin: 0 0 4px; }
.perm-grant-hint {
  font-size: 11px;
  color: theme('colors.slate.400');
  margin: 0 0 10px;
  line-height: 1.5;
}
:is(.dark) .perm-grant-hint { color: theme('colors.slate.600'); }
.perm-grant-hint code {
  background: theme('colors.slate.100');
  padding: 0 3px;
  border-radius: 3px;
  font-size: 11px;
}
:is(.dark) .perm-grant-hint code { background: theme('colors.slate.800'); }
.perm-grant-row { display: flex; gap: 8px; align-items: center; }
.perm-error { font-size: 11px; color: #f87171; margin: 6px 0 0; }

.dep-subheading {
  font-size: 11px;
  color: theme('colors.slate.400');
  margin: 0 0 4px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.3px;
}
:is(.dark) .dep-subheading { color: theme('colors.slate.600'); }
.dep-list { margin-bottom: 10px; }
.dep-action-hint {
  font-size: 10px;
  color: theme('colors.slate.400');
  font-family: var(--font-mono);
}
:is(.dark) .dep-action-hint { color: theme('colors.slate.600'); }
</style>
