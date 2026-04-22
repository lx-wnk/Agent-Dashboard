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
  if (!props.task) return
  const [deps, depts] = await Promise.all([
    fetchDependencies(props.task.id),
    fetchDependents(props.task.id),
  ])
  dependencies.value = deps
  dependents.value = depts
}

async function handleAddDependency(): Promise<void> {
  if (!props.task || !newDepId.value.trim()) return
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
  if (!props.task) return
  try {
    await removeTaskDependency(props.task.id, depId)
    await loadDependencies()
  }
  catch (err) {
    depError.value = (err as Error).message
  }
}

// Live session lookup: the backend enriches tasks with `activeSessionId` of
// the most relevant stage_run. If that session is also discovered by the
// agent scanner (it will be, for any running detached claude process), we
// get a live Agent object with channelAvailable/status, which we pass into
// AgentChatStream + PromptInput unchanged.
const pipelineAgent = computed(() => {
  const sid = props.task?.activeSessionId
  if (!sid)
    return null
  return agents.value.find(a => a.sessionId === sid) ?? null
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
  <Transition name="modal">
    <div v-if="task" class="task-modal-backdrop" @click.self="emit('close')">
      <div class="task-modal">
        <header class="modal-head">
          <div class="head-left">
            <span class="stage-badge" :class="`stage-${task.currentStage}`">{{ task.currentStage }}</span>
            <span v-if="isFailedRun(task)" class="run-status-badge status-failed" title="Latest stage run failed">
              RUN FAILED
            </span>
            <span class="task-slug">{{ task.slug }}</span>
            <h2>{{ task.title }}</h2>
          </div>
          <div class="head-right">
            <button class="close-btn" title="Close (Esc)" @click="emit('close')">
              &times;
            </button>
          </div>
        </header>

        <nav class="tabs">
          <button :class="{ active: activeTab === 'overview' }" @click="activeTab = 'overview'">
            Overview
          </button>
          <button :class="{ active: activeTab === 'session' }" @click="activeTab = 'session'">
            Session
            <span v-if="pipelineAgent" class="tab-dot" :class="pipelineAgent.status" />
          </button>
          <button :class="{ active: activeTab === 'stages' }" @click="activeTab = 'stages'">
            Stages ({{ stageRuns.length }})
          </button>
          <button :class="{ active: activeTab === 'permissions' }" @click="activeTab = 'permissions'">
            Permissions ({{ permissions.length }})
          </button>
          <button :class="{ active: activeTab === 'audit' }" @click="activeTab = 'audit'">
            Audit
          </button>
        </nav>

        <div class="modal-body">
          <!-- Overview tab -->
          <section v-if="activeTab === 'overview'" class="tab-content">
            <!-- Latest stage run summary (non-approval states) -->
            <div v-if="!approvalMeta && latestStageRun" class="latest-run-card">
              <div class="latest-run-head">
                <span class="stage-label-pill">{{ latestStageRun.stage }}</span>
                <span class="iteration-pill">iter {{ latestStageRun.iteration }}</span>
                <span class="stage-status" :class="`status-${latestStageRun.status}`">{{ latestStageRun.status }}</span>
                <span class="latest-run-times">
                  {{ formatDate(latestStageRun.startedAt) }}
                  <template v-if="latestStageRun.endedAt"> → {{ formatDate(latestStageRun.endedAt) }}</template>
                </span>
              </div>
              <div v-if="latestRunAgentMessage" class="agent-message-block">
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
                  rows="3"
                  placeholder="What should the agent change in the next iteration?"
                  :disabled="isActing"
                  maxlength="4000"
                />
                <div class="feedback-input-foot">
                  <span class="char-count">{{ feedbackInput.length }} / 4000</span>
                  <button
                    class="btn btn-red"
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
            <section class="dep-section">
              <h4 class="dep-heading">Abhängigkeiten</h4>

              <div v-if="dependencies.length > 0" class="dep-list">
                <p class="dep-subheading">Wartet auf:</p>
                <div
                  v-for="dep in dependencies"
                  :key="dep.id"
                  class="dep-row"
                >
                  <span class="dep-title">{{ dep.dependsOnTitle }}</span>
                  <span
                    class="meta-chip stage"
                    :class="dep.dependsOnStage === dep.requiredStage ? 'dep-met' : 'dep-unmet'"
                  >{{ dep.dependsOnStage }}</span>
                  <span class="dep-action-hint">on cancel: {{ dep.onCancelAction }}</span>
                  <button class="dep-remove" title="Remove dependency" @click="handleRemoveDependency(dep.id)">✕</button>
                </div>
              </div>

              <div v-if="dependents.length > 0" class="dep-list">
                <p class="dep-subheading">Wird benötigt von:</p>
                <div v-for="dep in dependents" :key="dep.id" class="dep-row">
                  <span class="dep-title">{{ dep.taskTitle || dep.taskId }}</span>
                </div>
              </div>

              <form class="dep-add-form" @submit.prevent="handleAddDependency">
                <input
                  v-model="newDepId"
                  class="dep-input"
                  placeholder="Vorgänger Task-ID"
                  :disabled="isAddingDep"
                />
                <select v-model="newDepStage" class="dep-select">
                  <option value="done">Done</option>
                  <option value="cancelled">Cancelled</option>
                </select>
                <select v-model="newDepCancelAction" class="dep-select">
                  <option value="on_hold">On Hold (bei Cancel)</option>
                  <option value="cancel">Cancel (bei Cancel)</option>
                  <option value="start">Start (bei Cancel)</option>
                </select>
                <button class="dep-add-btn" type="submit" :disabled="isAddingDep || !newDepId.trim()">
                  Hinzufügen
                </button>
              </form>
              <p v-if="depError" class="dep-error">{{ depError }}</p>
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
            <div v-for="run in stageRuns" v-else :key="run.id" class="stage-run">
              <div class="stage-run-head">
                <span class="stage-label">{{ run.stage }}</span>
                <span class="iteration">iter {{ run.iteration }}</span>
                <span class="stage-status" :class="`status-${run.status}`">{{ run.status }}</span>
              </div>
              <div v-if="run.sessionName" class="stage-meta">
                session: <code>{{ run.sessionName }}</code>
              </div>
              <div class="stage-meta">
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
              <div v-for="req in pendingRequests" :key="req.id" class="perm-request">
                <div class="perm-request-head">
                  <strong>{{ req.tool }}</strong>
                  <span v-if="req.pattern" class="mono">{{ req.pattern }}</span>
                </div>
                <div v-if="req.reason" class="perm-reason">
                  {{ req.reason }}
                </div>
                <div class="perm-actions">
                  <button
                    class="btn btn-sm btn-green"
                    :disabled="isActing"
                    @click="onResolve(req, 'granted')"
                  >
                    Grant
                  </button>
                  <button
                    class="btn btn-sm btn-red"
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
                  class="perm-input"
                  placeholder="Tool (e.g. Bash, Write)"
                  @keydown.enter="onGrantPermission"
                >
                <input
                  v-model="newPermPattern"
                  class="perm-input perm-input-pattern"
                  placeholder="Pattern (optional, e.g. npm run *)"
                  @keydown.enter="onGrantPermission"
                >
                <button
                  class="btn btn-sm btn-green"
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

        <footer class="modal-actions">
          <p v-if="actionError" class="action-error">
            {{ actionError }}
          </p>
          <p v-if="analysisInfo" class="action-info">
            Analysis agent spawned · PID <code>{{ analysisInfo.pid }}</code> · look for it in the agents list.
          </p>
          <div class="action-buttons">
            <button
              v-if="isFailedRun(task)"
              class="btn btn-primary"
              :disabled="isActing"
              title="Start a fresh iteration of this stage"
              @click="handleAction(() => retryTask(task!.id))"
            >
              Retry Stage
            </button>
            <button
              v-if="isFailedRun(task)"
              class="btn btn-secondary"
              :disabled="isActing"
              title="Spawn a standalone Claude session with the failure context attached"
              @click="onAnalyze"
            >
              Analyze Failure
            </button>
            <button
              v-if="approvalMeta"
              class="btn btn-green"
              :disabled="isActing"
              :title="`Advance past ${task.currentStage} gate`"
              @click="handleAction(() => approveTask(task!.id))"
            >
              {{ approvalMeta.label }}
            </button>
            <button
              v-else-if="!isTerminal(task.currentStage) && !isOnHoldStage && !isFailedRun(task)"
              class="btn btn-secondary"
              :disabled="isActing"
              title="Manually advance to the next stage (skips approval gates)"
              @click="handleAction(() => progressTask(task!.id))"
            >
              Progress →
            </button>
            <button
              v-if="!isTerminal(task.currentStage)"
              class="btn btn-red"
              :disabled="isActing"
              title="Stop this task and mark it as cancelled"
              @click="handleAction(() => cancelTask(task!.id))"
            >
              Cancel Task
            </button>
          </div>
        </footer>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.task-modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 24px;
}
.task-modal {
  background: var(--bg-secondary);
  border-radius: 10px;
  border: 1px solid var(--bg-tertiary);
  width: 100%;
  max-width: 860px;
  max-height: 86vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.modal-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  padding: 14px 20px;
  background: var(--bg-tertiary);
}
.head-left { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.head-left h2 { font-size: 16px; font-weight: 600; flex-basis: 100%; margin-top: 4px; }
.task-slug {
  font-family: var(--font-mono);
  font-size: 12px;
  color: var(--accent-blue);
}
.stage-badge {
  font-size: 10px;
  text-transform: uppercase;
  background: var(--bg-primary);
  padding: 3px 8px;
  border-radius: 4px;
  font-family: var(--font-mono);
  color: var(--text-secondary);
}
.stage-badge.stage-on_hold { background: rgba(234, 179, 8, 0.2); color: rgb(234, 179, 8); }
.stage-badge.stage-done { background: rgba(74, 222, 128, 0.2); color: var(--accent-green); }
.stage-badge.stage-cancelled { background: rgba(248, 113, 113, 0.2); color: var(--accent-red); }

.head-right { display: flex; align-items: center; gap: 8px; }

.close-btn {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 20px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
}
.close-btn:hover { background: var(--bg-secondary); }

.tabs {
  display: flex;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.tabs button {
  background: none;
  border: none;
  color: var(--text-muted);
  padding: 10px 16px;
  font-size: 12px;
  cursor: pointer;
  border-bottom: 2px solid transparent;
}
.tabs button.active {
  color: var(--text-primary);
  border-bottom-color: var(--accent-blue);
}
.tab-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  margin-left: 5px;
  vertical-align: middle;
  background: var(--text-muted);
}
.tab-dot.active { background: var(--accent-green); animation: pulse 2s ease-in-out infinite; }
.tab-dot.waiting { background: rgb(234, 179, 8); }
.tab-dot.idle { background: var(--text-muted); }
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
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.session-label {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
}
.session-id {
  font-family: var(--font-mono);
  font-size: 11px;
  background: var(--bg-tertiary);
  padding: 1px 6px;
  border-radius: 3px;
  color: var(--accent-blue);
}
.session-status {
  font-size: 10px;
  text-transform: uppercase;
  padding: 2px 8px;
  border-radius: 4px;
  font-weight: 700;
  margin-left: auto;
}
.session-status.active { background: rgba(74, 222, 128, 0.2); color: var(--accent-green); }
.session-status.waiting { background: rgba(234, 179, 8, 0.2); color: rgb(234, 179, 8); }
.session-status.idle { background: var(--bg-tertiary); color: var(--text-muted); }
.session-status.offline { background: var(--bg-tertiary); color: var(--text-muted); }
.session-stream {
  flex: 1;
  padding: 12px 20px;
  min-height: 280px;
  max-height: 50vh;
}
.session-empty {
  padding: 60px 20px;
}
.session-empty-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-secondary);
  margin-bottom: 6px;
}
.session-empty-body {
  font-size: 12px;
  color: var(--text-muted);
  max-width: 420px;
  margin: 0 auto;
  line-height: 1.5;
}
.session-hint {
  padding: 10px 20px;
  font-size: 12px;
  color: var(--text-muted);
  border-top: 1px solid var(--border);
}

.modal-body { flex: 1; overflow-y: auto; }
.tab-content { padding: 18px 20px; }
.empty-hint { color: var(--text-muted); font-size: 12px; text-align: center; padding: 32px 0; }

.facts {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 6px 16px;
  font-size: 13px;
  margin-bottom: 16px;
}
.facts > div { display: contents; }
.facts dt {
  color: var(--text-muted);
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.facts dd { color: var(--text-primary); }
.mono { font-family: var(--font-mono); font-size: 12px; }

.description {
  padding: 12px;
  background: var(--bg-primary);
  border-radius: 6px;
  font-size: 13px;
  line-height: 1.5;
  white-space: pre-wrap;
  color: var(--text-secondary);
}
.description-collapsible {
  margin-top: 12px;
  font-size: 12px;
}
.description-collapsible > summary {
  cursor: pointer;
  color: var(--text-muted);
  padding: 6px 0;
  user-select: none;
}
.description-collapsible > summary:hover {
  color: var(--text-secondary);
}
.description-collapsible[open] > summary {
  margin-bottom: 6px;
}

.latest-run-card {
  background: var(--bg-primary);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 12px 14px;
  margin-bottom: 16px;
}
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
  background: var(--bg-tertiary);
  color: var(--text-secondary);
  padding: 2px 8px;
  border-radius: 4px;
  font-weight: 600;
}
.iteration-pill {
  font-size: 10px;
  color: var(--text-muted);
  font-family: var(--font-mono);
}
.latest-run-times {
  font-size: 11px;
  color: var(--text-muted);
  margin-left: auto;
}
.agent-message-block {
  margin-top: 6px;
}
.agent-message-label {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  margin-bottom: 4px;
}
.agent-message-body {
  font-family: var(--font-mono);
  font-size: 11px;
  background: var(--bg-secondary);
  border-radius: 4px;
  padding: 10px 12px;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 300px;
  overflow-y: auto;
  color: var(--text-secondary);
  line-height: 1.5;
}
.latest-run-output > summary {
  cursor: pointer;
  font-size: 11px;
  color: var(--text-muted);
  padding: 2px 0;
  user-select: none;
}
.latest-run-output > summary:hover { color: var(--text-secondary); }
.latest-run-output[open] > summary { margin-bottom: 8px; }
.latest-run-head .stage-status { margin-left: 0; }

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
  color: var(--accent-green);
  letter-spacing: 0.5px;
  margin-bottom: 8px;
}
.approval-preview pre {
  background: var(--bg-primary);
  padding: 10px;
  border-radius: 4px;
  font-size: 11px;
  max-height: 360px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-word;
}
.approval-preview code {
  font-family: var(--font-mono);
  background: var(--bg-tertiary);
  padding: 1px 4px;
  border-radius: 3px;
}
.feedback-thread {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid rgba(74, 222, 128, 0.25);
}
.feedback-thread h4 {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  margin-bottom: 8px;
}
.feedback-entry {
  background: var(--bg-primary);
  border-left: 2px solid var(--accent-yellow, rgb(234, 179, 8));
  border-radius: 4px;
  padding: 8px 10px;
  margin-bottom: 6px;
}
.feedback-entry.resolved {
  border-left-color: var(--accent-green);
  opacity: 0.7;
}
.feedback-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 10px;
  color: var(--text-muted);
  margin-bottom: 4px;
  flex-wrap: wrap;
}
.feedback-iter {
  font-family: var(--font-mono);
  background: var(--bg-tertiary);
  padding: 1px 5px;
  border-radius: 3px;
  color: var(--accent-blue);
  font-weight: 600;
}
.feedback-stage {
  font-family: var(--font-mono);
}
.feedback-date {
  margin-left: auto;
}
.feedback-status {
  font-weight: 700;
  text-transform: uppercase;
  font-size: 9px;
  padding: 1px 5px;
  border-radius: 3px;
}
.feedback-status.resolved {
  background: rgba(74, 222, 128, 0.18);
  color: var(--accent-green);
}
.feedback-status.pending {
  background: rgba(234, 179, 8, 0.18);
  color: rgb(234, 179, 8);
}
.feedback-text {
  font-size: 12px;
  line-height: 1.5;
  color: var(--text-secondary);
  white-space: pre-wrap;
}
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
  color: var(--text-muted);
  margin-bottom: 6px;
}
.feedback-input-block textarea {
  width: 100%;
  padding: 8px 10px;
  background: var(--bg-primary);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--text-primary);
  font-family: inherit;
  font-size: 12px;
  line-height: 1.5;
  resize: vertical;
  box-sizing: border-box;
}
.feedback-input-block textarea:focus {
  outline: none;
  border-color: var(--accent-blue);
}
.feedback-input-foot {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 6px;
}
.char-count {
  font-size: 10px;
  color: var(--text-muted);
  font-family: var(--font-mono);
}

.stage-run {
  padding: 10px 12px;
  background: var(--bg-primary);
  border-radius: 6px;
  margin-bottom: 8px;
}
.stage-run-head {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 4px;
}
.stage-label {
  font-weight: 600;
  font-size: 12px;
  color: var(--text-primary);
}
.iteration { font-family: var(--font-mono); font-size: 11px; color: var(--text-muted); }
.stage-status {
  font-size: 10px;
  padding: 2px 6px;
  border-radius: 4px;
  text-transform: uppercase;
  margin-left: auto;
  font-family: var(--font-mono);
}
.status-running { background: rgba(59, 130, 246, 0.2); color: var(--accent-blue); }
.status-done { background: rgba(74, 222, 128, 0.2); color: var(--accent-green); }
.status-failed { background: rgba(248, 113, 113, 0.2); color: var(--accent-red); }
.status-on_hold { background: rgba(234, 179, 8, 0.2); color: rgb(234, 179, 8); }
.stage-meta { font-size: 11px; color: var(--text-muted); margin-top: 2px; }
.stage-output { margin-top: 6px; font-size: 11px; }
.stage-output pre {
  background: var(--bg-tertiary);
  padding: 8px;
  border-radius: 4px;
  overflow-x: auto;
  max-height: 180px;
  overflow-y: auto;
}

.pending-section h3, .tab-content > h3 {
  font-size: 11px;
  text-transform: uppercase;
  color: var(--text-muted);
  margin-bottom: 8px;
  letter-spacing: 0.5px;
}
.perm-request {
  background: rgba(234, 179, 8, 0.1);
  border: 1px solid rgba(234, 179, 8, 0.4);
  border-radius: 6px;
  padding: 10px 12px;
  margin-bottom: 8px;
}
.perm-request-head { display: flex; gap: 10px; align-items: baseline; }
.perm-reason { font-size: 11px; color: var(--text-muted); margin: 4px 0; }
.perm-actions { display: flex; gap: 6px; margin-top: 6px; }

.perm-row {
  display: flex;
  gap: 10px;
  padding: 6px 10px;
  font-size: 12px;
  border-bottom: 1px solid var(--border);
}
.perm-tool { font-weight: 600; color: var(--text-primary); min-width: 80px; }
.perm-pattern { font-family: var(--font-mono); color: var(--text-muted); flex: 1; }
.perm-type { font-size: 10px; color: var(--text-muted); text-transform: uppercase; }
.perm-decided { font-size: 10px; color: var(--text-muted); }

.perm-grant-form {
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 12px 14px;
  margin-bottom: 12px;
  background: var(--bg-primary);
}
.perm-grant-form h3 { margin: 0 0 4px; }
.perm-grant-hint {
  font-size: 11px;
  color: var(--text-muted);
  margin: 0 0 10px;
  line-height: 1.5;
}
.perm-grant-hint code {
  background: rgba(255,255,255,0.07);
  padding: 0 3px;
  border-radius: 3px;
  font-size: 11px;
}
.perm-grant-row { display: flex; gap: 8px; align-items: center; }
.perm-input {
  flex: 1;
  min-width: 0;
  background: var(--bg-secondary);
  border: 1px solid var(--border);
  border-radius: 5px;
  color: var(--text-primary);
  font-size: 12px;
  padding: 5px 9px;
}
.perm-input-pattern { flex: 1.5; }
.perm-input:focus { outline: none; border-color: var(--accent-blue); }
.perm-error { font-size: 11px; color: var(--accent-red, #f87171); margin: 6px 0 0; }

.modal-actions {
  padding: 12px 20px;
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}
.action-error {
  color: var(--accent-red);
  font-size: 12px;
  margin-bottom: 8px;
}
.action-info {
  color: var(--accent-green);
  font-size: 12px;
  margin-bottom: 8px;
}
.action-info code {
  font-family: var(--font-mono);
  background: var(--bg-tertiary);
  padding: 1px 4px;
  border-radius: 3px;
}
.run-status-badge {
  font-size: 9px;
  font-weight: 700;
  padding: 3px 7px;
  border-radius: 4px;
  font-family: var(--font-mono);
  letter-spacing: 0.5px;
}
.run-status-badge.status-failed {
  background: rgba(248, 113, 113, 0.2);
  color: var(--accent-red);
  border: 1px solid rgba(248, 113, 113, 0.4);
}
.action-buttons { display: flex; gap: 8px; justify-content: flex-end; }

.btn {
  border: none;
  border-radius: 4px;
  padding: 6px 14px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  font-family: inherit;
}
.btn-sm { padding: 4px 10px; font-size: 11px; }
.btn:disabled { opacity: 0.4; cursor: not-allowed; }
.btn-primary { background: var(--accent-blue); color: white; }
.btn-secondary { background: var(--bg-tertiary); color: var(--text-secondary); }
.btn-green { background: var(--accent-green); color: var(--bg-primary); }
.btn-red { background: var(--accent-red); color: white; }
.btn:hover:not(:disabled) { filter: brightness(1.1); }

.modal-enter-active, .modal-leave-active { transition: opacity 0.2s; }
.modal-enter-from, .modal-leave-to { opacity: 0; }

.dep-section {
  margin-top: 16px;
  border-top: 1px solid var(--border);
  padding-top: 12px;
}
.dep-heading {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-secondary);
  margin: 0 0 8px;
}
.dep-subheading {
  font-size: 11px;
  color: var(--text-muted);
  margin: 0 0 4px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.3px;
}
.dep-list {
  margin-bottom: 10px;
}
.dep-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: 12px;
}
.dep-title {
  flex: 1;
  color: var(--text-primary);
}
.dep-action-hint {
  font-size: 10px;
  color: var(--text-muted);
  font-family: var(--font-mono);
}
.dep-met {
  background: rgba(74, 222, 128, 0.15);
  color: var(--accent-green);
  border: 1px solid var(--accent-green);
}
.dep-unmet {
  background: rgba(248, 113, 113, 0.15);
  color: var(--accent-red);
  border: 1px solid rgba(248, 113, 113, 0.5);
}
.dep-remove {
  background: none;
  border: none;
  cursor: pointer;
  color: var(--text-muted);
  padding: 2px 4px;
  font-size: 10px;
  border-radius: 3px;
}
.dep-remove:hover {
  background: rgba(248, 113, 113, 0.15);
  color: var(--accent-red);
}
.dep-add-form {
  display: flex;
  gap: 6px;
  align-items: center;
  flex-wrap: wrap;
  margin-top: 8px;
}
.dep-input {
  flex: 1;
  min-width: 0;
  padding: 4px 8px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 12px;
}
.dep-select {
  padding: 4px 6px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--bg-secondary);
  color: var(--text-primary);
  font-size: 11px;
}
.dep-add-btn {
  padding: 4px 10px;
  background: var(--accent-blue);
  color: white;
  border: none;
  border-radius: 4px;
  font-size: 12px;
  cursor: pointer;
}
.dep-add-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.dep-error {
  font-size: 11px;
  color: var(--accent-red);
  margin: 4px 0 0;
}
</style>
