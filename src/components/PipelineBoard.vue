<script setup lang="ts">
import type { PipelineStage, PipelineTask } from '../types'
import { computed } from 'vue'
import { useTasks } from '../composables/useTasks'
import TaskCard from './TaskCard.vue'

defineEmits<{ select: [task: PipelineTask] }>()

const { tasksByStageMap } = useTasks()

interface ColumnDef {
  id: string
  label: string
  // Empty for the virtual "needs-you" column which filters tasks across
  // all stages by their needsUser flag. Agent-group columns list the
  // specific stages they include.
  stages: PipelineStage[]
  group: 'needs-you' | 'active' | 'terminal'
  hint?: string
}

// Consolidated 8 swimlanes. "Needs You" gathers every stage where the user
// must act before progress can continue (approvals + runtime permission
// requests + mid-stage awaiting_user). Agent-driven stages are grouped
// into phases.
const COLUMNS: ColumnDef[] = [
  {
    id: 'needs-you',
    label: 'Needs You',
    stages: [], // virtual column: filters by needsUser flag (see tasksForColumn)
    group: 'needs-you',
    hint: 'User action required',
  },
  { id: 'backlog', label: 'Backlog', stages: ['backlog'], group: 'active' },
  {
    id: 'analysis',
    label: 'Analysis',
    stages: ['pruefung', 'refinement', 'planning'],
    group: 'active',
  },
  { id: 'konzept', label: 'Concept', stages: ['umsetzungskonzept'], group: 'active' },
  {
    id: 'umsetzung',
    label: 'Implementation',
    stages: ['umsetzung', 'selbstreview'],
    group: 'active',
  },
  { id: 'finalisierung', label: 'Completion', stages: ['finalisierung'], group: 'active' },
  { id: 'done', label: 'Done', stages: ['done'], group: 'terminal' },
  // `failed` is no longer a terminal task stage — failed tasks stay on
  // their current stage and appear in "Needs You" via the needsUser flag.
  // Only explicitly cancelled tasks land here now.
  {
    id: 'cancelled',
    label: 'Cancelled',
    stages: ['cancelled'],
    group: 'terminal',
  },
]

function tasksForColumn(col: ColumnDef): PipelineTask[] {
  if (col.id === 'needs-you') {
    // Any task flagged needsUser — including ones whose current_stage is
    // an agent stage but whose latest stage_run is awaiting_user/on_hold.
    const all: PipelineTask[] = []
    for (const stageTasks of Object.values(tasksByStageMap.value)) {
      for (const task of stageTasks || []) {
        if (task.needsUser)
          all.push(task)
      }
    }
    return all
  }
  // Normal stage-based columns exclude needsUser tasks so they don't
  // appear in two columns at once.
  const all: PipelineTask[] = []
  for (const stage of col.stages) {
    const rows = tasksByStageMap.value[stage] || []
    for (const task of rows) {
      if (!task.needsUser)
        all.push(task)
    }
  }
  return all
}

const columnsWithTasks = computed(() =>
  COLUMNS.map(col => ({ col, tasks: tasksForColumn(col) })),
)
</script>

<template>
  <div class="flex gap-3 overflow-x-auto pb-4 min-h-[calc(100vh-200px)]">
    <div
      v-for="{ col, tasks } in columnsWithTasks"
      :key="col.id"
      class="flex-[1_1_260px] min-w-[240px] rounded-lg flex flex-col max-h-[calc(100vh-220px)]"
      :class="col.group === 'needs-you'
        ? 'bg-yellow-50/30 dark:bg-yellow-950/10 border border-yellow-300/60 dark:border-yellow-700/40'
        : col.group === 'terminal'
          ? 'bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700 opacity-70'
          : 'bg-white dark:bg-slate-900 border border-slate-200 dark:border-slate-700'"
    >
      <div class="flex justify-between items-center px-3 py-2.5 border-b border-slate-200 dark:border-slate-700 flex-shrink-0">
        <span class="text-[11px] font-semibold text-slate-500 dark:text-slate-400 uppercase tracking-wider">{{ col.label }}</span>
        <span class="text-[11px] text-slate-400 dark:text-slate-600 bg-slate-50 dark:bg-slate-950 px-2 py-px rounded-full font-mono">{{ tasks.length }}</span>
      </div>
      <div v-if="col.hint" class="text-[10px] text-yellow-600 dark:text-yellow-400 px-3 pb-1.5 uppercase tracking-wider font-semibold">
        {{ col.hint }}
      </div>
      <div class="p-2.5 flex flex-col gap-2 overflow-y-auto">
        <TaskCard
          v-for="task in tasks"
          :key="task.id"
          :task="task"
          @select="(t) => $emit('select', t)"
        />
        <div v-if="!tasks.length" class="text-center text-slate-400 dark:text-slate-600 text-[11px] py-5">
          —
        </div>
      </div>
    </div>
  </div>
</template>
