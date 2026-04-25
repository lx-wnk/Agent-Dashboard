<script setup lang="ts">
import type { PipelineStage, PipelineTask, StageRunStatus } from '../types'

defineProps<{ task: PipelineTask }>()
defineEmits<{ select: [task: PipelineTask] }>()

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
  backlog: 'Backlog',
  pruefung: 'Prüfung',
  refinement: 'Refinement',
  planning: 'Planung',
  approval1: 'Freigabe 1',
  umsetzungskonzept: 'Konzept',
  approval2: 'Freigabe 2',
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
    <div class="flex flex-wrap gap-1 mt-0.5">
      <span
        class="text-[10px] font-mono px-1.5 py-px rounded border"
        :class="{
          'bg-slate-100 dark:bg-slate-800 text-slate-500 dark:text-slate-400 border-slate-200 dark:border-slate-700': !['on_hold', 'approval1', 'approval2', 'umsetzung', 'done', 'cancelled'].includes(task.currentStage),
          'bg-yellow-50 dark:bg-yellow-950/50 text-yellow-600 dark:text-yellow-400 border-yellow-200 dark:border-yellow-800/60': ['on_hold', 'approval1', 'approval2'].includes(task.currentStage),
          'bg-blue-50 dark:bg-blue-950/50 text-blue-600 dark:text-blue-400 border-blue-300 dark:border-blue-700': task.currentStage === 'umsetzung',
          'bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400 border-green-300 dark:border-green-700': task.currentStage === 'done',
          'bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400 border-red-300 dark:border-red-700': task.currentStage === 'cancelled',
        }"
      >{{ stageLabel(task.currentStage) }}</span>
      <span
        v-if="task.latestStageRunStatus"
        class="text-[10px] font-mono font-bold uppercase tracking-wide px-1.5 py-px rounded border"
        :class="{
          'bg-blue-50 dark:bg-blue-950/50 text-blue-600 dark:text-blue-400 border-blue-300 dark:border-blue-600/50': task.latestStageRunStatus === 'running',
          'bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 border-slate-200 dark:border-slate-700': task.latestStageRunStatus === 'pending',
          'bg-yellow-50 dark:bg-yellow-950/50 text-yellow-600 dark:text-yellow-400 border-yellow-200 dark:border-yellow-700/50': ['awaiting_user', 'on_hold'].includes(task.latestStageRunStatus),
          'bg-green-50 dark:bg-green-950/50 text-green-600 dark:text-green-400 border-green-200 dark:border-green-700/50': task.latestStageRunStatus === 'done',
          'bg-red-50 dark:bg-red-950/50 text-red-600 dark:text-red-400 border-red-200 dark:border-red-700/50': task.latestStageRunStatus === 'failed',
        }"
        :title="`Latest stage run: ${runStatusLabel(task.latestStageRunStatus)}`"
      >{{ runStatusLabel(task.latestStageRunStatus) }}</span>
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
