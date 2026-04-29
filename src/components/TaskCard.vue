<script setup lang="ts">
import type { PipelineStage, PipelineTask, StageRunStatus } from '../types'
import { runStatusChipClass, stageChipClass } from '../utils/statusColors'

defineProps<{ task: PipelineTask }>()
const emit = defineEmits<{ select: [task: PipelineTask], openChat: [task: PipelineTask] }>()

function shortDate(iso: string): string {
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

const RUN_STATUS_LABELS: Record<StageRunStatus, string> = {
  pending: 'Pending',
  running: 'Running',
  awaiting_user: 'Waiting',
  on_hold: 'On Hold',
  done: 'Done',
  failed: 'Failed',
}

function runStatusLabel(status: StageRunStatus): string {
  return RUN_STATUS_LABELS[status] ?? status
}

const STAGE_LABELS: Record<PipelineStage, string> = {
  konzept: 'Konzept',
  backlog: 'Backlog',
  umsetzung: 'Umsetzung',
  selbstreview: 'Selbstreview',
  finalisierung: 'Finalisierung',
  done: 'Done',
  on_hold: 'Permission',
  cancelled: 'Cancelled',
}

function stageLabel(stage: PipelineStage): string {
  return STAGE_LABELS[stage] || stage
}
</script>

<template>
  <div
    class="bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-2.5 cursor-pointer transition-all flex flex-col gap-1.5"
    :class="task.isBlocked ? 'opacity-60 hover:opacity-85' : 'hover:border-blue-500 dark:hover:border-blue-400 hover:-translate-y-px'"
    tabindex="0"
    role="button"
    :aria-label="`Open task ${task.title}`"
    @click="$emit('select', task)"
    @keydown.enter="$emit('select', task)"
    @keydown.space.prevent="$emit('select', task)"
  >
    <div class="flex justify-between items-baseline gap-2">
      <span class="font-mono text-[11px] text-blue-600 dark:text-blue-400 font-semibold overflow-hidden text-ellipsis whitespace-nowrap">{{ task.slug }}</span>
      <span class="text-[10px] text-slate-400 dark:text-slate-600">{{ shortDate(task.createdAt) }}</span>
    </div>
    <div class="text-[13px] font-semibold text-slate-900 dark:text-slate-100 leading-tight line-clamp-2">
      {{ task.title }}
    </div>
    <div v-if="task.description" class="text-[11px] text-slate-400 dark:text-slate-600 leading-snug line-clamp-2">
      {{ task.description }}
    </div>
    <button
      v-if="task.currentStage === 'konzept'"
      class="self-start text-[11px] font-semibold px-2 py-0.5 rounded border border-blue-300/60 dark:border-blue-700/60 bg-blue-50 dark:bg-blue-950/30 text-blue-600 dark:text-blue-400 hover:bg-blue-100 dark:hover:bg-blue-900/40 hover:border-blue-500 dark:hover:border-blue-400 transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-blue-500"
      @click.stop="emit('openChat', task)"
    >
      Chat fortsetzen →
    </button>
    <div class="flex flex-wrap gap-1 mt-0.5">
      <span
        class="text-[10px] font-mono px-1.5 py-px rounded border"
        :class="stageChipClass(task.currentStage)"
      >{{ stageLabel(task.currentStage) }}</span>
      <span
        v-if="task.latestStageRunStatus"
        class="text-[10px] font-mono font-bold uppercase tracking-wide px-1.5 py-px rounded border"
        :class="runStatusChipClass(task.latestStageRunStatus)"
        :title="`Latest stage run: ${runStatusLabel(task.latestStageRunStatus)}`"
      >{{ runStatusLabel(task.latestStageRunStatus) }}</span>
      <span
        v-if="task.needsUser && task.latestStageRunStatus === 'awaiting_user'"
        class="text-[10px] font-mono font-bold uppercase tracking-wide px-1.5 py-px rounded border bg-yellow-100 dark:bg-yellow-950/40 text-yellow-700 dark:text-yellow-400 border-yellow-300 dark:border-yellow-700/60"
        title="Agent is paused and waiting for a permission grant"
      >⚠ Needs Permission</span>
      <span v-if="task.worktreePath" class="text-[10px] font-mono px-1.5 py-px rounded border bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 border-slate-200 dark:border-slate-700" title="Has worktree">WT</span>
      <span v-if="task.sourceBranch" class="text-[10px] font-mono px-1.5 py-px rounded border bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 border-slate-200 dark:border-slate-700">{{ task.sourceBranch }}</span>
      <span v-if="task.parentTaskId" class="text-[10px] font-mono px-1.5 py-px rounded border bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 border-slate-200 dark:border-slate-700" title="Follow-up task">↳</span>
      <span v-if="task.isUnsatisfiable" class="text-[10px] font-mono px-1.5 py-px rounded border bg-yellow-50 dark:bg-yellow-950/30 text-yellow-600 dark:text-yellow-400 border-yellow-200 dark:border-yellow-800/50" title="Unsatisfiable dep">⚠ Unsatisfiable dep</span>
      <span v-else-if="task.isBlocked" class="text-[10px] font-mono px-1.5 py-px rounded border bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 border-slate-200 dark:border-slate-700/50" title="Waiting for prerequisite">🔒 Blocked</span>
      <span v-if="task.currentStage === 'umsetzung'" class="text-[10px] font-mono px-1.5 py-px rounded border bg-yellow-50 dark:bg-yellow-950/30 text-yellow-600 dark:text-yellow-400 border-yellow-200 dark:border-yellow-800/50">
        max iter {{ task.maxIterations }}
      </span>
    </div>
  </div>
</template>
