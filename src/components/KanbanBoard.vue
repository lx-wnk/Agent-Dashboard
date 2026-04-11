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

const columns = computed(() => {
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
      }
    }
  }

  return { pending, inProgress, completed }
})

const totalTasks = computed(() =>
  columns.value.pending.length
  + columns.value.inProgress.length
  + columns.value.completed.length,
)
</script>

<template>
  <div class="kanban-board">
    <div class="kanban-column">
      <div class="column-header">
        <span class="column-icon pending-icon">○</span>
        <span class="column-title">Pending</span>
        <span class="column-count">{{ columns.pending.length }}</span>
      </div>
      <div class="column-body">
        <div
          v-for="card in columns.pending"
          :key="`${card.agent.sessionId}-${card.task.id}`"
          class="kanban-card"
          @click="$emit('select', card.agent)"
        >
          <div class="card-subject">{{ card.task.subject }}</div>
          <div class="card-footer">
            <span class="card-project">{{ card.agent.projectName }}</span>
            <StatusBadge :status="card.agent.status" />
          </div>
        </div>
        <div v-if="columns.pending.length === 0" class="column-empty">
          No tasks
        </div>
      </div>
    </div>

    <div class="kanban-column">
      <div class="column-header">
        <span class="column-icon progress-icon">●</span>
        <span class="column-title">In Progress</span>
        <span class="column-count">{{ columns.inProgress.length }}</span>
      </div>
      <div class="column-body">
        <div
          v-for="card in columns.inProgress"
          :key="`${card.agent.sessionId}-${card.task.id}`"
          class="kanban-card"
          @click="$emit('select', card.agent)"
        >
          <div class="card-subject">{{ card.task.subject }}</div>
          <div class="card-footer">
            <span class="card-project">{{ card.agent.projectName }}</span>
            <StatusBadge :status="card.agent.status" />
          </div>
        </div>
        <div v-if="columns.inProgress.length === 0" class="column-empty">
          No tasks
        </div>
      </div>
    </div>

    <div class="kanban-column">
      <div class="column-header">
        <span class="column-icon completed-icon">✓</span>
        <span class="column-title">Completed</span>
        <span class="column-count">{{ columns.completed.length }}</span>
      </div>
      <div class="column-body">
        <div
          v-for="card in columns.completed"
          :key="`${card.agent.sessionId}-${card.task.id}`"
          class="kanban-card"
          @click="$emit('select', card.agent)"
        >
          <div class="card-subject">{{ card.task.subject }}</div>
          <div class="card-footer">
            <span class="card-project">{{ card.agent.projectName }}</span>
            <StatusBadge :status="card.agent.status" />
          </div>
        </div>
        <div v-if="columns.completed.length === 0" class="column-empty">
          No tasks
        </div>
      </div>
    </div>

    <p v-if="totalTasks === 0" class="board-empty">
      No tasks found across agents.
    </p>
  </div>
</template>

<style scoped>
.kanban-board {
  display: grid;
  grid-template-columns: 1fr;
  gap: 16px;
  position: relative;
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
  grid-column: 1 / -1;
  text-align: center;
  padding: 48px;
  color: var(--text-muted);
  font-size: 14px;
}
</style>
