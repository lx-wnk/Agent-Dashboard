<script setup lang="ts">
import type { TaskInfo } from '../types'

defineProps<{
  tasks: TaskInfo[]
}>()

function statusIcon(status: string): string {
  switch (status) {
    case 'completed': return '✓'
    case 'in_progress': return '●'
    default: return '○'
  }
}
</script>

<template>
  <div v-if="tasks.length > 0" class="task-list">
    <h4>Tasks</h4>
    <ul>
      <li v-for="task in tasks" :key="task.id" class="task-item">
        <span class="task-icon">{{ statusIcon(task.status) }}</span>
        <span class="task-subject">{{ task.subject }}</span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
h4 {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  margin-bottom: 8px;
}

ul {
  list-style: none;
  padding: 0;
}

.task-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 0;
  font-size: 12px;
  color: var(--text-secondary);
}

.task-icon {
  font-size: 10px;
  width: 14px;
  text-align: center;
  color: var(--accent-green);
}
</style>
