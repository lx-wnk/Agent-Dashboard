<script setup lang="ts">
import type { Agent, TaskInfo } from '../types'
import { computed } from 'vue'
import StatusBadge from './StatusBadge.vue'

const props = defineProps<{
  agents: Agent[]
}>()

defineEmits<{
  select: [agent: Agent]
}>()

interface KanbanCard {
  task: TaskInfo
  agent: Agent
}

interface ColumnDef {
  key: string
  title: string
  icon: string
  iconClass: string
  cards: KanbanCard[]
}

const columns = computed<ColumnDef[]>(() => {
  const pending: KanbanCard[] = []
  const inProgress: KanbanCard[] = []
  const completed: KanbanCard[] = []

  for (const agent of props.agents) {
    for (const task of agent.tasks) {
      const card: KanbanCard = { task, agent }
      switch (task.status) {
        case 'pending':
          pending.push(card)
          break
        case 'in_progress':
          inProgress.push(card)
          break
        case 'completed':
          completed.push(card)
          break
        default:
          // Unknown status: treat as pending
          pending.push(card)
          break
      }
    }
  }

  return [
    { key: 'pending', title: 'Pending', icon: '○', iconClass: 'pending-icon', cards: pending },
    { key: 'inProgress', title: 'In Progress', icon: '●', iconClass: 'progress-icon', cards: inProgress },
    { key: 'completed', title: 'Completed', icon: '✓', iconClass: 'completed-icon', cards: completed },
  ]
})

const totalTasks = computed(() =>
  columns.value.reduce((sum, col) => sum + col.cards.length, 0),
)
</script>

<template>
  <div v-if="totalTasks > 0" class="kanban-board">
    <div v-for="col in columns" :key="col.key" class="kanban-column">
      <div class="column-header">
        <span class="column-icon" :class="col.iconClass">{{ col.icon }}</span>
        <span class="column-title">{{ col.title }}</span>
        <span class="column-count">{{ col.cards.length }}</span>
      </div>
      <div class="column-body">
        <div
          v-for="card in col.cards"
          :key="`${card.agent.sessionId}-${card.task.id}`"
          class="kanban-card"
          @click="$emit('select', card.agent)"
        >
          <div class="card-subject">
            {{ card.task.subject }}
          </div>
          <div class="card-footer">
            <span class="card-project">{{ card.agent.projectName }}</span>
            <StatusBadge :status="card.agent.status" />
          </div>
        </div>
        <div v-if="col.cards.length === 0" class="column-empty">
          No tasks
        </div>
      </div>
    </div>
  </div>
  <p v-else class="board-empty">
    No tasks found across agents.
  </p>
</template>

<style scoped>
.kanban-board {
  display: grid;
  grid-template-columns: 1fr;
  gap: 16px;
}

@media (min-width: 768px) {
  .kanban-board {
    grid-template-columns: repeat(3, 1fr);
  }
}

.kanban-column {
  background: var(--bg-secondary);
  border-radius: 8px;
  border: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  min-height: 200px;
  max-height: calc(100vh - 250px);
}

.column-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 14px;
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}

.column-icon {
  font-size: 10px;
  width: 14px;
  text-align: center;
}

.pending-icon {
  color: var(--text-muted);
}

.progress-icon {
  color: var(--accent-yellow);
}

.completed-icon {
  color: var(--accent-green);
}

.column-title {
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-secondary);
}

.column-count {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--text-muted);
  background: var(--bg-tertiary);
  padding: 1px 7px;
  border-radius: 10px;
  margin-left: auto;
}

.column-body {
  padding: 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.kanban-card {
  background: var(--bg-primary);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 10px 12px;
  cursor: pointer;
  transition: border-color 0.15s, box-shadow 0.15s;
}

.kanban-card:hover {
  border-color: var(--bg-tertiary);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
}

.card-subject {
  font-size: 13px;
  color: var(--text-primary);
  line-height: 1.4;
  margin-bottom: 8px;
  word-break: break-word;
}

.card-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.card-project {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
}

.column-empty {
  padding: 24px 12px;
  text-align: center;
  font-size: 13px;
  color: var(--text-muted);
  font-style: italic;
}

.board-empty {
  text-align: center;
  padding: 48px;
  color: var(--text-muted);
  font-size: 14px;
}
</style>
