<script setup lang="ts">
import type { Agent, PipelineStage, PipelineTask } from '@/types'
import { computed, ref } from 'vue'
import { useProjects } from '@/composables/useProjects'
import SortableTaskList from '@/features/pipeline/components/SortableTaskList.vue'
import TaskCard from '@/features/pipeline/components/TaskCard.vue'
import { byActivityDesc, byRank, useTasks } from '@/features/pipeline/composables/useTasks'
import { triggerDownload } from '@/utils/download'
import { STAGE_LABELS } from '@/utils/stageLabels'

const props = withDefaults(defineProps<{ agents?: Agent[] }>(), { agents: () => [] })

const emit = defineEmits<{
  select: [task: PipelineTask]
  openChat: [task: PipelineTask]
  navigateAgent: [sessionId: string]
}>()

const { tasks: allTasks, tasksByStageMap } = useTasks()
const { projects } = useProjects()

const projectById = computed(() => {
  const map = new Map<string, typeof projects.value[number]>()
  for (const p of projects.value)
    map.set(p.id, p)
  return map
})

// Multi-select project filter: empty set means "show all"
const selectedProjectIds = ref<Set<string>>(new Set())

function toggleProjectFilter(projectId: string) {
  const next = new Set(selectedProjectIds.value)
  if (next.has(projectId))
    next.delete(projectId)
  else
    next.add(projectId)
  selectedProjectIds.value = next
}

function clearProjectFilter() {
  selectedProjectIds.value = new Set()
}

function isFilteredByProject(task: PipelineTask): boolean {
  if (selectedProjectIds.value.size === 0)
    return true
  // Tasks with no projectId are shown when "no project" chip is selected
  if (task.projectId == null)
    return selectedProjectIds.value.has('__none__')
  return selectedProjectIds.value.has(task.projectId)
}

// Projects that actually have tasks (for filter chip display)
const projectsWithTasks = computed(() => {
  const projectIdsInUse = new Set(allTasks.value.map(t => t.projectId).filter(Boolean) as string[])
  const hasUnassigned = allTasks.value.some(t => t.projectId == null)
  return {
    projects: projects.value.filter(p => projectIdsInUse.has(p.id)),
    hasUnassigned,
  }
})

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
  triggerDownload(`/api/tasks/export?format=${format}`, `tasks.${format}`)
}

interface ColumnDef {
  id: string
  label: string
  // Empty for the virtual "needs-you" column which filters tasks across
  // all stages by their needsUser flag. Agent-group columns list the
  // specific stages they include.
  stages: PipelineStage[]
  group: 'needs-you' | 'active' | 'terminal'
  sortable: boolean
}

const COLUMNS: ColumnDef[] = [
  {
    id: 'needs-you',
    label: 'User Action Required',
    stages: [],
    group: 'needs-you',
    sortable: true,
  },
  { id: 'concept', label: STAGE_LABELS.concept, stages: ['concept'], group: 'active', sortable: true },
  { id: 'backlog', label: STAGE_LABELS.backlog, stages: ['backlog'], group: 'active', sortable: true },
  { id: 'plan_review', label: STAGE_LABELS.plan_review, stages: ['plan_review'], group: 'active', sortable: false },
  {
    id: 'implementation',
    label: STAGE_LABELS.implementation,
    stages: ['implementation', 'self_review'],
    group: 'active',
    sortable: false,
  },
  { id: 'finalization', label: STAGE_LABELS.finalization, stages: ['finalization'], group: 'active', sortable: false },
  { id: 'done', label: STAGE_LABELS.done, stages: ['done'], group: 'terminal', sortable: false },
  { id: 'cancelled', label: STAGE_LABELS.cancelled, stages: ['cancelled'], group: 'terminal', sortable: false },
]

function tasksForColumn(col: ColumnDef): PipelineTask[] {
  if (col.id === 'needs-you') {
    // Any task flagged needsUser — including ones whose current_stage is
    // an agent stage but whose latest stage_run is awaiting_user/on_hold.
    const all: PipelineTask[] = []
    for (const stageTasks of Object.values(tasksByStageMap.value)) {
      for (const task of stageTasks || []) {
        if (task.needsUser && isFilteredByProject(task))
          all.push(task)
      }
    }
    // Aggregated across stages — re-sort by rank so manual drag order persists.
    return all.sort(byRank)
  }
  // Normal stage-based columns exclude needsUser tasks so they don't
  // appear in two columns at once.
  const all: PipelineTask[] = []
  for (const stage of col.stages) {
    const rows = tasksByStageMap.value[stage] || []
    for (const task of rows) {
      if (!task.needsUser && isFilteredByProject(task))
        all.push(task)
    }
  }
  return col.sortable ? all : [...all].sort(byActivityDesc)
}

const columnsWithTasks = computed(() =>
  COLUMNS.map(col => ({ col, tasks: tasksForColumn(col) })),
)

// O(1) lookup for epic membership — avoids O(N²) .some() in template
const epicParentIds = computed(() => new Set(epics.value.map(e => e.parent.id)))

// taskId → agent currently working that task; prefer active status when multiple agents map to one task
const workingAgentByTask = computed(() => {
  const map = new Map<string, Agent>()
  for (const agent of props.agents) {
    if (!agent.pipelineTaskId)
      continue
    const existing = map.get(agent.pipelineTaskId)
    if (!existing || (agent.status === 'active' && existing.status !== 'active'))
      map.set(agent.pipelineTaskId, agent)
  }
  return map
})

function isHighlightCol(col: ColumnDef): boolean {
  return col.group === 'needs-you'
}
</script>

<template>
  <div class="flex flex-col h-full">
    <div class="flex items-center gap-2 mb-3 flex-shrink-0 flex-wrap">
      <span class="text-xs text-fg-mute">Export:</span>
      <button
        type="button"
        class="text-xs px-2 py-1 rounded border border-line-strong hover:bg-raised text-fg-soft"
        @click="exportTasks('json')"
      >
        JSON
      </button>
      <button
        type="button"
        class="text-xs px-2 py-1 rounded border border-line-strong hover:bg-raised text-fg-soft"
        @click="exportTasks('csv')"
      >
        CSV
      </button>
      <!-- Project filter chips -->
      <template v-if="projectsWithTasks.projects.length > 0 || projectsWithTasks.hasUnassigned">
        <span class="text-xs text-fg-mute ml-2">Project:</span>
        <button
          v-for="p in projectsWithTasks.projects"
          :key="p.id"
          type="button"
          class="text-xs px-2 py-1 rounded border transition-colors"
          :class="selectedProjectIds.has(p.id)
            ? 'border-blue-500 bg-blue-50 dark:bg-blue-950/30 text-blue-600 dark:text-blue-400'
            : 'border-line-strong text-fg-soft hover:bg-raised'"
          :style="selectedProjectIds.has(p.id) && p.color ? { borderColor: p.color, backgroundColor: `${p.color}22`, color: p.color } : {}"
          :aria-pressed="selectedProjectIds.has(p.id)"
          @click="toggleProjectFilter(p.id)"
        >
          <span v-if="p.color" class="inline-block w-2 h-2 rounded-full mr-1 align-middle" :style="{ backgroundColor: p.color }" aria-hidden="true" />
          {{ p.name }}
        </button>
        <button
          v-if="projectsWithTasks.hasUnassigned"
          type="button"
          class="text-xs px-2 py-1 rounded border transition-colors"
          :class="selectedProjectIds.has('__none__')
            ? 'border-blue-500 bg-blue-50 dark:bg-blue-950/30 text-blue-600 dark:text-blue-400'
            : 'border-line-strong text-fg-soft hover:bg-raised'"
          :aria-pressed="selectedProjectIds.has('__none__')"
          @click="toggleProjectFilter('__none__')"
        >
          No project
        </button>
        <button
          v-if="selectedProjectIds.size > 0"
          type="button"
          class="text-xs px-2 py-1 rounded text-fg-mute hover:text-slate-700 dark:hover:text-slate-200"
          @click="clearProjectFilter"
        >
          Clear
        </button>
      </template>
    </div>
    <div class="flex gap-3 overflow-x-auto pb-4 flex-1 min-h-0">
      <div
        v-for="{ col, tasks } in columnsWithTasks"
        :key="col.id"
        class="flex-[1_1_260px] min-w-[240px] rounded-lg flex flex-col"
        :class="isHighlightCol(col)
          ? 'bg-warning-soft border border-warning-line'
          : col.group === 'terminal'
            ? 'bg-card border border-line opacity-70'
            : 'bg-card border border-line'"
      >
        <div
          class="flex justify-between items-center px-3 py-2.5 border-b flex-shrink-0"
          :class="isHighlightCol(col)
            ? 'border-warning-line'
            : 'border-line'"
        >
          <span
            class="text-[11px] font-semibold uppercase tracking-wider flex items-center gap-1.5"
            :class="isHighlightCol(col)
              ? 'text-warning-text'
              : 'text-fg-mute'"
          >
            <span
              v-if="isHighlightCol(col)"
              class="inline-flex items-center justify-center w-4 h-4 rounded-full bg-card text-warning-text text-[10px] leading-none border border-warning-line"
              aria-hidden="true"
            >!</span>
            {{ col.label }}
          </span>
          <span
            class="text-[11px] px-2 py-px rounded-full font-mono"
            :class="isHighlightCol(col)
              ? 'text-warning-text bg-card border border-warning-line'
              : 'text-fg-mute bg-app'"
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
                :aria-expanded="!!epicExpanded[epic.parent.id]"
                :aria-controls="`epic-children-${epic.parent.id}`"
                @click="toggleEpic(epic.parent.id)"
              >
                <svg
                  width="24"
                  height="24"
                  viewBox="0 0 24 24"
                  class="flex-shrink-0"
                  aria-hidden="true"
                  focusable="false"
                >
                  <circle cx="12" cy="12" r="9" fill="none" class="stroke-slate-200 dark:stroke-slate-700" stroke-width="3" />
                  <circle
                    cx="12" cy="12" r="9" fill="none"
                    class="stroke-blue-500"
                    stroke-width="3"
                    stroke-dasharray="56.55"
                    :stroke-dashoffset="56.55 * (1 - epic.completionPct / 100)"
                    stroke-linecap="round"
                    transform="rotate(-90 12 12)"
                  />
                </svg>
                <span class="text-xs font-semibold text-fg-soft truncate flex-1">{{ epic.parent.title }}</span>
                <span class="text-[10px] text-slate-400 flex-shrink-0">{{ epic.doneCount }}/{{ epic.totalCount }} ({{ epic.completionPct }}%)</span>
                <span class="text-xs text-slate-400" aria-hidden="true">{{ epicExpanded[epic.parent.id] ? '▲' : '▼' }}</span>
              </button>
              <div
                v-if="epicExpanded[epic.parent.id]"
                :id="`epic-children-${epic.parent.id}`"
                class="pl-3 pr-2 pb-2 pt-1 space-y-1.5"
              >
                <TaskCard
                  v-for="child in epic.children.filter(c => tasks.some(t => t.id === c.id))"
                  :key="child.id"
                  :task="child"
                  :project="child.projectId ? projectById.get(child.projectId) ?? null : null"
                  :working-agent="workingAgentByTask.get(child.id) ?? null"
                  @select="emit('select', child)"
                  @open-chat="emit('openChat', child)"
                  @navigate-agent="(sid) => emit('navigateAgent', sid)"
                />
              </div>
            </div>
          </template>
          <SortableTaskList
            :tasks="tasks.filter(t => !t.parentTaskId || !epicParentIds.has(t.parentTaskId))"
            :project-by-id="projectById"
            :working-agent-by-task="workingAgentByTask"
            :sortable="col.sortable"
            @select="(t) => emit('select', t)"
            @open-chat="(t) => emit('openChat', t)"
            @navigate-agent="(sid) => emit('navigateAgent', sid)"
          />
          <div
            v-if="tasks.filter(t => !t.parentTaskId || !epicParentIds.has(t.parentTaskId)).length === 0
              && !epics.some(e => tasks.some(t => t.parentTaskId === e.parent.id))"
            class="text-center text-fg-mute text-[11px] py-5"
          >
            —
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
