<script setup lang="ts">
import type { PipelineTask } from '../../types'
import { computed, ref, watch } from 'vue'
import { useInjectedTask, useInjectedTaskDetails } from '../../composables/taskModalContext'
import { useTaskAssignment } from '../../composables/useTaskAssignment'
import { runStatusChipClass } from '../../utils/statusColors'
import { activeRuntime, formatCents, formatTaskDate, taskRuntime } from '../../utils/taskFormat'
import AgentChatStream from '../AgentChatStream.vue'
import GitStatusPanel from '../GitStatusPanel.vue'
import RefineStatusPanel from '../RefineStatusPanel.vue'
import StageOutputView from '../StageOutputView.vue'
import AppButton from '../ui/AppButton.vue'
import WorktreeCommandRunner from '../WorktreeCommandRunner.vue'
import WorktreePanel from '../WorktreePanel.vue'
import TaskPendingRequests from './TaskPendingRequests.vue'

const emit = defineEmits<{ openChat: [task: PipelineTask] }>()

const task = useInjectedTask()
const {
  stageRuns,
  pendingRequests,
  latestStageRun,
  latestRunError,
  latestRunAgentMessage,
  sessionAgentText,
  sessionAgentTextLoading,
  pipelineAgent,
  totalTokensUsed,
  totalCostCents,
} = useInjectedTaskDetails()
const {
  projects,
  spawners,
  currentProject,
  effectiveSpawner,
  isAssigningProject,
  isAssigningSpawner,
  assignError,
  onProjectChange,
  onSpawnerChange,
} = useTaskAssignment(task)

const runtime = computed(() => (task.value ? taskRuntime(task.value) : '—'))
const active = computed(() => activeRuntime(stageRuns.value))

const lastRefineOutput = ref('')
const completedRefinePhases = ref<string[]>([])

async function loadLastRefineOutput(taskId: string): Promise<void> {
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
  () => [task.value?.id, task.value?.currentStage, task.value?.refineStatus] as const,
  ([id, stage]) => {
    if (id && stage === 'concept')
      void loadLastRefineOutput(id)
  },
  { immediate: true },
)
</script>

<template>
  <section v-if="task" class="flex-1 overflow-y-auto p-5 flex flex-col gap-4 min-h-0">
    <!-- Lingering-pending gate: orchestrator refuses to respawn while pendings linger. -->
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

    <div
      v-if="pendingRequests.length > 0"
      class="bg-yellow-50 dark:bg-yellow-950/30 border border-yellow-300 dark:border-yellow-700/60 rounded-md px-3.5 py-3"
    >
      <h3 class="text-[11px] uppercase tracking-[0.5px] text-yellow-700 dark:text-yellow-400 font-semibold mb-2 flex items-center gap-1.5">
        <span aria-hidden="true">⚠</span> Waiting for Permission ({{ pendingRequests.length }})
      </h3>
      <TaskPendingRequests />
    </div>

    <template v-if="task.currentStage === 'concept'">
      <RefineStatusPanel
        :status="task.refineStatus ?? 'idle'"
        :error="task.refineError ?? null"
        :last-output="lastRefineOutput"
        :completed-phases="completedRefinePhases"
        class="mb-2"
      />
      <div class="flex items-center justify-between gap-3 bg-blue-50 dark:bg-blue-950/30 border border-blue-200 dark:border-blue-800 rounded-md px-3.5 py-3 mb-2">
        <div class="flex flex-col gap-0.5">
          <span class="text-xs font-semibold text-blue-700 dark:text-blue-300">This ticket is waiting for refinement</span>
          <span class="text-[11px] text-blue-600/80 dark:text-blue-400/80">Continue the conversation with the refinement assistant</span>
        </div>
        <AppButton variant="primary" size="sm" @click="emit('openChat', task)">
          Continue Chat →
        </AppButton>
      </div>
    </template>

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
          {{ formatTaskDate(task.createdAt) }}
        </dd>
      </div>
      <div class="contents">
        <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
          Runtime
        </dt><dd class="text-fg font-mono text-xs">
          {{ runtime }}
        </dd>
      </div>
      <div v-if="active !== '—'" class="contents">
        <dt class="text-fg-mute text-[11px] uppercase tracking-[0.5px]">
          Active Runtime
        </dt><dd class="text-fg font-mono text-xs">
          {{ active }}
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
          {{ formatCents(totalCostCents) }}
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

    <WorktreePanel
      v-if="task.worktreePath"
      :task-id="task.id"
      :worktree-path="task.worktreePath"
      :active="!!task"
    />

    <section class="border-t border-line pt-3 flex flex-col gap-2.5">
      <h4 class="text-[11px] font-semibold uppercase tracking-[0.5px] text-fg-mute">
        Project &amp; Spawner
      </h4>
      <p v-if="assignError" class="text-[11px] text-red-600 dark:text-red-400">
        {{ assignError }}
      </p>
      <div class="grid grid-cols-2 gap-3">
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

    <details v-if="task.description" class="mb-1 text-xs border-t border-line pt-3">
      <summary class="cursor-pointer text-fg-mute py-1.5 select-none hover:text-slate-500 dark:hover:text-slate-400">
        Origin Prompt
      </summary>
      <div class="mt-1.5 px-3 py-3 bg-app rounded-md text-[13px] leading-relaxed whitespace-pre-wrap text-fg-mute">
        {{ task.description }}
      </div>
    </details>

    <div v-if="latestStageRun" class="bg-app border border-line rounded-md px-3.5 py-3 border-t border-line pt-3">
      <div class="text-[10px] uppercase tracking-[0.5px] text-fg-mute font-semibold mb-2">
        Current Output
      </div>
      <div class="flex items-center gap-2 flex-wrap mb-2">
        <span class="font-mono text-[10px] uppercase bg-raised text-fg-mute px-2 py-0.5 rounded font-semibold">{{ latestStageRun.stage }}</span>
        <span class="text-[10px] text-fg-mute font-mono">iter {{ latestStageRun.iteration }}</span>
        <span class="text-[10px] px-1.5 py-px rounded uppercase ml-auto font-mono" :class="runStatusChipClass(latestStageRun.status)">{{ latestStageRun.status }}</span>
        <span class="text-[11px] text-fg-mute ml-auto">
          {{ formatTaskDate(latestStageRun.startedAt) }}
          <template v-if="latestStageRun.endedAt"> → {{ formatTaskDate(latestStageRun.endedAt) }}</template>
        </span>
      </div>
      <AgentChatStream
        v-if="latestStageRun.status === 'running' && pipelineAgent"
        :agent="pipelineAgent"
        :local-messages="[]"
        class="border-t border-line mt-2 pt-3 min-h-[200px] max-h-[40vh] px-0 py-3"
      />
      <template v-else>
        <div v-if="latestRunError" class="mt-2 rounded-md bg-red-50 dark:bg-red-950/30 border border-red-200 dark:border-red-800/50 px-3 py-2 flex items-start gap-2">
          <span class="text-red-500 dark:text-red-400 text-sm leading-none mt-0.5">✗</span>
          <p class="text-xs text-red-700 dark:text-red-300 font-mono leading-relaxed whitespace-pre-wrap break-words">
            {{ latestRunError }}
          </p>
        </div>

        <div v-if="latestRunAgentMessage" class="mt-2">
          <div class="text-[10px] uppercase tracking-[0.5px] text-fg-mute mb-1">
            Agent output
          </div>
          <pre class="font-mono text-[11px] bg-card rounded px-3 py-2.5 whitespace-pre-wrap break-words max-h-[300px] overflow-y-auto text-fg-mute leading-relaxed">{{ latestRunAgentMessage }}</pre>
        </div>

        <div v-else-if="sessionAgentText || sessionAgentTextLoading" class="mt-2">
          <div class="text-[10px] uppercase tracking-[0.5px] text-fg-mute mb-1">
            Agent output
          </div>
          <div v-if="sessionAgentTextLoading" class="text-[11px] text-fg-mute animate-pulse">
            Loading…
          </div>
          <pre v-else class="font-mono text-[11px] bg-card rounded px-3 py-2.5 whitespace-pre-wrap break-words max-h-[400px] overflow-y-auto text-fg-mute leading-relaxed">{{ sessionAgentText }}</pre>
        </div>

        <div v-else-if="latestStageRun.status === 'running'" class="mt-2 text-[11px] text-fg-mute animate-pulse">
          Agent is running…
        </div>

        <details v-else-if="latestStageRun.output && !latestRunError" class="mt-1.5">
          <summary class="cursor-pointer text-[11px] text-fg-mute py-0.5 select-none hover:text-slate-500">
            Stage output
          </summary>
          <StageOutputView :stage="latestStageRun.stage" :output="latestStageRun.output" :status="latestStageRun.status" />
        </details>
      </template>
    </div>

    <div class="mt-2">
      <h3 class="text-xs font-semibold text-fg-mute uppercase tracking-wide mb-2">
        Git Status
      </h3>
      <GitStatusPanel :task-id="task.id" />
    </div>

    <WorktreeCommandRunner :task-id="task.id" class="mt-2" />
  </section>
</template>
