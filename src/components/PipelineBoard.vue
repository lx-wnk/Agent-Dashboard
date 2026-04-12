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
  stages: PipelineStage[]
  group: 'needs-you' | 'active' | 'terminal'
  hint?: string
}

// Consolidated 8 swimlanes. "Needs You" gathers every stage where the user
// must act before progress can continue (approvals + runtime permission
// requests). Agent-driven stages are grouped into phases.
const COLUMNS: ColumnDef[] = [
  {
    id: 'needs-you',
    label: 'Needs You',
    stages: ['on_hold', 'approval1', 'approval2'],
    group: 'needs-you',
    hint: 'User action required',
  },
  { id: 'backlog', label: 'Backlog', stages: ['backlog'], group: 'active' },
  {
    id: 'analysis',
    label: 'Analyse',
    stages: ['pruefung', 'refinement', 'planning'],
    group: 'active',
  },
  { id: 'konzept', label: 'Konzept', stages: ['umsetzungskonzept'], group: 'active' },
  {
    id: 'umsetzung',
    label: 'Umsetzung',
    stages: ['umsetzung', 'selbstreview'],
    group: 'active',
  },
  { id: 'finalisierung', label: 'Abschluss', stages: ['finalisierung'], group: 'active' },
  { id: 'done', label: 'Done', stages: ['done'], group: 'terminal' },
  {
    id: 'terminated',
    label: 'Terminated',
    stages: ['failed', 'cancelled'],
    group: 'terminal',
  },
]

function tasksForColumn(col: ColumnDef): PipelineTask[] {
  const all: PipelineTask[] = []
  for (const stage of col.stages) {
    const rows = tasksByStageMap.value[stage] || []
    all.push(...rows)
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
