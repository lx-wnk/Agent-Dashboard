<script setup lang="ts">
import type { TaskInfo } from '../types'

defineProps<{ tasks: TaskInfo[] }>()

function statusIcon(status: string): string {
  switch (status) {
    case 'completed': return '✓'
    case 'in_progress': return '●'
    default: return '○'
  }
}
</script>

<template>
  <div v-if="tasks.length > 0">
    <h4 class="text-[10px] font-semibold uppercase tracking-wider text-slate-400 dark:text-slate-600 mb-2">
      Tasks
    </h4>
    <ul class="list-none p-0 m-0">
      <li
        v-for="task in tasks"
        :key="task.id"
        class="flex items-center gap-2 py-1 text-xs text-slate-500 dark:text-slate-400"
      >
        <span class="text-[10px] w-3.5 text-center text-green-600 dark:text-green-400">{{ statusIcon(task.status) }}</span>
        <span>{{ task.subject }}</span>
      </li>
    </ul>
  </div>
</template>
