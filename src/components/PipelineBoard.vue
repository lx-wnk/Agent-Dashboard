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
  <div class="pipeline-board">
    <div
      v-for="{ col, tasks } in columnsWithTasks"
      :key="col.id"
      class="swimlane"
      :class="`swimlane-${col.group}`"
    >
      <div class="swimlane-header">
        <span class="swimlane-label">{{ col.label }}</span>
        <span class="swimlane-count">{{ tasks.length }}</span>
      </div>
      <div v-if="col.hint" class="swimlane-hint">
        {{ col.hint }}
      </div>
      <div class="swimlane-body">
        <TaskCard
          v-for="task in tasks"
          :key="task.id"
          :task="task"
          @select="(t) => $emit('select', t)"
        />
        <div v-if="!tasks.length" class="empty-hint">
          —
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.pipeline-board {
  display: flex;
  gap: 12px;
  overflow-x: auto;
  padding-bottom: 16px;
  min-height: calc(100vh - 200px);
}
.swimlane {
  flex: 1 1 260px;
  min-width: 240px;
  background: var(--bg-secondary);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  max-height: calc(100vh - 220px);
}
.swimlane-needs-you {
  border: 1px solid rgba(234, 179, 8, 0.6);
  background: rgba(234, 179, 8, 0.08);
  flex-basis: 300px;
}
.swimlane-terminal {
  opacity: 0.7;
}
.swimlane-hint {
  font-size: 10px;
  color: rgb(234, 179, 8);
  padding: 0 12px 6px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  font-weight: 600;
}
.swimlane-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 10px 12px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.swimlane-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.swimlane-count {
  font-size: 11px;
  color: var(--text-muted);
  background: var(--bg-primary);
  padding: 1px 8px;
  border-radius: 10px;
  font-family: var(--font-mono);
}
.swimlane-body {
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  overflow-y: auto;
}
.empty-hint {
  text-align: center;
  color: var(--text-muted);
  font-size: 11px;
  padding: 20px 0;
}
</style>
