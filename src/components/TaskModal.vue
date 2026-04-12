<script setup lang="ts">
import type { PermissionRequest, PipelineTask, StageRun, TaskPermission } from '../types'
import { onUnmounted, ref, watch } from 'vue'
import { useRole } from '../composables/useRole'
import {
  approveTask,
  cancelTask,
  fetchPendingPermissionRequests,
  fetchStageRuns,
  fetchTaskPermissions,
  progressTask,
  resolvePermissionRequest,
} from '../composables/useTasks'

const props = defineProps<{ task: PipelineTask | null }>()
const emit = defineEmits<{ close: [] }>()

const { role } = useRole()

type Tab = 'overview' | 'stages' | 'permissions' | 'audit'
const activeTab = ref<Tab>('overview')
const stageRuns = ref<StageRun[]>([])
const permissions = ref<TaskPermission[]>([])
const pendingRequests = ref<PermissionRequest[]>([])
const actionError = ref('')
const isActing = ref(false)

const isApprovalStage = (stage: string | undefined) => stage === 'approval1' || stage === 'approval2'
const isOnHold = (stage: string | undefined) => stage === 'on_hold'
function isTerminal(stage: string | undefined) {
  return stage === 'done' || stage === 'failed' || stage === 'cancelled'
}

async function loadDetails() {
  if (!props.task)
    return
  stageRuns.value = await fetchStageRuns(props.task.id)
  permissions.value = await fetchTaskPermissions(props.task.id)
  pendingRequests.value = await fetchPendingPermissionRequests(props.task.id)
}

watch(() => props.task?.id, (id) => {
  if (id) {
    activeTab.value = 'overview'
    actionError.value = ''
    void loadDetails()
  }
})

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

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.task) {
    e.preventDefault()
    emit('close')
  }
}
watch(() => props.task, (t) => {
  if (t)
    window.addEventListener('keydown', onKeydown)
  else
    window.removeEventListener('keydown', onKeydown)
}, { immediate: true })
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
            <span class="task-slug">{{ task.slug }}</span>
            <h2>{{ task.title }}</h2>
          </div>
          <div class="head-right">
            <span class="role-chip" :class="`role-${role}`">{{ role }}</span>
            <button class="close-btn" @click="emit('close')">
              &times;
            </button>
          </div>
        </header>

        <nav class="tabs">
          <button :class="{ active: activeTab === 'overview' }" @click="activeTab = 'overview'">
            Overview
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
            <div v-if="task.description" class="description">
              {{ task.description }}
            </div>
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
                <pre>{{ JSON.stringify(run.output, null, 2) }}</pre>
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
            <h3 v-if="permissions.length > 0">
              Granted permissions
            </h3>
            <div v-for="p in permissions" :key="p.id" class="perm-row">
              <span class="perm-tool">{{ p.tool }}</span>
              <span v-if="p.pattern" class="perm-pattern">{{ p.pattern }}</span>
              <span class="perm-type">{{ p.preApproved ? 'pre-approved' : 'runtime' }}</span>
              <span class="perm-decided">{{ p.decidedBy }}</span>
            </div>
            <div v-if="permissions.length === 0 && pendingRequests.length === 0" class="empty-hint">
              No permissions yet.
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
          <div class="action-buttons">
            <button
              v-if="role === 'reviewer' && isApprovalStage(task.currentStage)"
              class="btn btn-primary"
              :disabled="isActing"
              @click="handleAction(() => approveTask(task!.id))"
            >
              Approve
            </button>
            <button
              v-if="role === 'requester' && !isTerminal(task.currentStage) && !isOnHold(task.currentStage)"
              class="btn btn-secondary"
              :disabled="isActing"
              @click="handleAction(() => progressTask(task!.id))"
            >
              Progress →
            </button>
            <button
              v-if="!isTerminal(task.currentStage)"
              class="btn btn-red"
              :disabled="isActing"
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
.stage-badge.stage-failed { background: rgba(248, 113, 113, 0.2); color: var(--accent-red); }

.head-right { display: flex; align-items: center; gap: 8px; }
.role-chip {
  font-size: 10px;
  text-transform: uppercase;
  padding: 3px 8px;
  border-radius: 4px;
  font-weight: 600;
}
.role-chip.role-requester { background: rgba(59, 130, 246, 0.2); color: var(--accent-blue); }
.role-chip.role-reviewer { background: rgba(74, 222, 128, 0.2); color: var(--accent-green); }

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
</style>
