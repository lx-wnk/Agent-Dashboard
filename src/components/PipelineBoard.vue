<script setup lang="ts">
import type { PipelineStage, PipelineTask } from '../types'
import { computed, ref } from 'vue'
import { useTasks } from '../composables/useTasks'
import TaskCard from './TaskCard.vue'

const emit = defineEmits<{ select: [task: PipelineTask], openChat: [task: PipelineTask] }>()

const { tasks: allTasks, tasksByStageMap } = useTasks()

interface Epic {
  parent: PipelineTask
  children: PipelineTask[]
  doneCount: number
  totalCount: number
  completionPct: number
}

const epics = computed((): Epic[] => {
  const parentIds = new Set(allTasks.value.filter(t => t.parentTaskId).map(t => t.parentTaskId!))
  return [...parentIds].map((parentId) => {
    const parent = allTasks.value.find(t => t.id === parentId)
    if (!parent)
      return null
    const children = allTasks.value.filter(t => t.parentTaskId === parentId)
    const doneCount = children.filter(c => c.currentStage === 'done').length
    return {
      parent,
      children,
      doneCount,
      totalCount: children.length,
      completionPct: children.length > 0 ? Math.round((doneCount / children.length) * 100) : 0,
    }
  }).filter(Boolean) as Epic[]
})

const epicExpanded = ref<Record<string, boolean>>({})
function toggleEpic(id: string) {
  epicExpanded.value[id] = !epicExpanded.value[id]
}

function exportTasks(format: 'json' | 'csv') {
  window.open(`/api/tasks/export?format=${format}`, '_blank')
}

interface ColumnDef {
  id: string
  label: string
  // Empty for the virtual "needs-you" column which filters tasks across
  // all stages by their needsUser flag. Agent-group columns list the
  // specific stages they include.
  stages: PipelineStage[]
  group: 'needs-you' | 'active' | 'terminal'
}

const COLUMNS: ColumnDef[] = [
  {
    id: 'needs-you',
    label: 'User Action Required',
    stages: [],
    group: 'needs-you',
  },
  { id: 'concept', label: 'Concept', stages: ['concept'], group: 'active' },
  { id: 'backlog', label: 'Backlog', stages: ['backlog'], group: 'active' },
  {
    id: 'implementation',
    label: 'Implementation',
    stages: ['implementation', 'self_review'],
    group: 'active',
  },
  { id: 'finalization', label: 'Completion', stages: ['finalization'], group: 'active' },
  { id: 'done', label: 'Done', stages: ['done'], group: 'terminal' },
  { id: 'cancelled', label: 'Cancelled', stages: ['cancelled'], group: 'terminal' },
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
  <div>
    <div class="flex items-center gap-2 mb-3">
      <span class="text-xs text-slate-500 dark:text-slate-400">Export:</span>
      <button
        type="button"
        class="text-xs px-2 py-1 rounded border border-slate-300 dark:border-slate-600 hover:bg-slate-100 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300"
        @click="exportTasks('json')"
      >
        JSON
      </button>
      <button
        type="button"
        class="text-xs px-2 py-1 rounded border border-slate-300 dark:border-slate-600 hover:bg-slate-100 dark:hover:bg-slate-700 text-slate-700 dark:text-slate-300"
        @click="exportTasks('csv')"
      >
        CSV
      </button>
    </div>
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
        <div
          class="flex justify-between items-center px-3 py-2.5 border-b flex-shrink-0"
          :class="col.group === 'needs-you'
            ? 'border-yellow-300/60 dark:border-yellow-700/40'
            : 'border-slate-200 dark:border-slate-700'"
        >
          <span
            class="text-[11px] font-semibold uppercase tracking-wider flex items-center gap-1.5"
            :class="col.group === 'needs-you'
              ? 'text-yellow-700 dark:text-yellow-300'
              : 'text-slate-500 dark:text-slate-400'"
          >
            <span
              v-if="col.group === 'needs-you'"
              class="inline-flex items-center justify-center w-4 h-4 rounded-full bg-yellow-400/20 dark:bg-yellow-400/15 text-yellow-700 dark:text-yellow-300 text-[10px] leading-none ring-1 ring-yellow-400/40 dark:ring-yellow-500/30"
              aria-hidden="true"
            >!</span>
            {{ col.label }}
          </span>
          <span
            class="text-[11px] px-2 py-px rounded-full font-mono"
            :class="col.group === 'needs-you'
              ? 'text-yellow-700 dark:text-yellow-300 bg-yellow-400/15 dark:bg-yellow-400/10 ring-1 ring-yellow-400/30 dark:ring-yellow-500/25'
              : 'text-slate-400 dark:text-slate-600 bg-slate-50 dark:bg-slate-950'"
          >{{ tasks.length }}</span>
        </div>
        <div class="p-2.5 flex flex-col gap-2 overflow-y-auto">
          <template v-for="epic in epics" :key="epic.parent.id">
            <div
              v-if="tasks.some(t => t.parentTaskId === epic.parent.id)"
              class="mb-2 border border-blue-200 dark:border-blue-800 rounded-lg overflow-hidden"
            >
              <button
                type="button"
                class="w-full flex items-center gap-2 px-3 py-2 bg-blue-50 dark:bg-blue-950 text-left"
                @click="toggleEpic(epic.parent.id)"
              >
                <svg width="24" height="24" viewBox="0 0 24 24" class="flex-shrink-0">
                  <circle cx="12" cy="12" r="9" fill="none" stroke="#e2e8f0" stroke-width="3" />
                  <circle
                    cx="12" cy="12" r="9" fill="none"
                    stroke="#3b82f6" stroke-width="3"
                    stroke-dasharray="56.55"
                    :stroke-dashoffset="56.55 * (1 - epic.completionPct / 100)"
                    stroke-linecap="round"
                    transform="rotate(-90 12 12)"
                  />
                </svg>
                <span class="text-xs font-semibold text-slate-700 dark:text-slate-200 truncate flex-1">{{ epic.parent.title }}</span>
                <span class="text-[10px] text-slate-400 flex-shrink-0">{{ epic.doneCount }}/{{ epic.totalCount }} ({{ epic.completionPct }}%)</span>
                <span class="text-xs text-slate-400">{{ epicExpanded[epic.parent.id] ? '▲' : '▼' }}</span>
              </button>
              <div v-if="epicExpanded[epic.parent.id]" class="pl-3 pr-2 pb-2 pt-1 space-y-1.5">
                <TaskCard
                  v-for="child in epic.children.filter(c => tasks.some(t => t.id === c.id))"
                  :key="child.id"
                  :task="child"
                  @select="emit('select', child)"
                  @open-chat="emit('openChat', child)"
                />
              </div>
            </div>
          </template>
          <TaskCard
            v-for="task in tasks.filter(t => !t.parentTaskId || !epics.some(e => e.parent.id === t.parentTaskId))"
            :key="task.id"
            :task="task"
            @select="(t) => emit('select', t)"
            @open-chat="(t) => emit('openChat', t)"
          />
          <div
            v-if="tasks.filter(t => !t.parentTaskId || !epics.some(e => e.parent.id === t.parentTaskId)).length === 0
              && !epics.some(e => tasks.some(t => t.parentTaskId === e.parent.id))"
            class="text-center text-slate-400 dark:text-slate-600 text-[11px] py-5"
          >
            —
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
