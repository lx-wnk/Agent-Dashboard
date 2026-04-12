<script setup lang="ts">
import type { PipelineStage, PipelineTask } from '../types'
import { useTasks } from '../composables/useTasks'
import TaskCard from './TaskCard.vue'

defineEmits<{ select: [task: PipelineTask] }>()

const { tasksByStageMap } = useTasks()

interface ColumnDef {
  stage: PipelineStage
  label: string
  group: 'active' | 'review' | 'terminal' | 'special'
}

const COLUMNS: ColumnDef[] = [
  { stage: 'on_hold', label: 'ON HOLD', group: 'special' },
  { stage: 'backlog', label: 'Backlog', group: 'active' },
  { stage: 'pruefung', label: 'Prüfung', group: 'active' },
  { stage: 'refinement', label: 'Refinement', group: 'active' },
  { stage: 'planning', label: 'Planning', group: 'active' },
  { stage: 'approval1', label: 'Freigabe 1', group: 'review' },
  { stage: 'umsetzungskonzept', label: 'Konzept', group: 'active' },
  { stage: 'approval2', label: 'Freigabe 2', group: 'review' },
  { stage: 'umsetzung', label: 'Umsetzung', group: 'active' },
  { stage: 'selbstreview', label: 'Selbstreview', group: 'active' },
  { stage: 'finalisierung', label: 'Finalisierung', group: 'active' },
  { stage: 'done', label: 'Done', group: 'terminal' },
  { stage: 'failed', label: 'Failed', group: 'terminal' },
  { stage: 'cancelled', label: 'Cancelled', group: 'terminal' },
]
</script>

<template>
  <div class="pipeline-board">
    <div
      v-for="col in COLUMNS"
      :key="col.stage"
      class="swimlane"
      :class="`swimlane-${col.group}`"
    >
      <div class="swimlane-header">
        <span class="swimlane-label">{{ col.label }}</span>
        <span class="swimlane-count">{{ (tasksByStageMap[col.stage] || []).length }}</span>
      </div>
      <div class="swimlane-body">
        <TaskCard
          v-for="task in tasksByStageMap[col.stage] || []"
          :key="task.id"
          :task="task"
          @select="(t) => $emit('select', t)"
        />
        <div v-if="!(tasksByStageMap[col.stage] || []).length" class="empty-hint">
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
  flex: 0 0 240px;
  background: var(--bg-secondary);
  border-radius: 8px;
  display: flex;
  flex-direction: column;
  max-height: calc(100vh - 220px);
}
.swimlane-special {
  border: 1px solid rgba(234, 179, 8, 0.5);
  background: rgba(234, 179, 8, 0.08);
}
.swimlane-review {
  border: 1px solid var(--accent-blue);
}
.swimlane-terminal {
  opacity: 0.7;
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
