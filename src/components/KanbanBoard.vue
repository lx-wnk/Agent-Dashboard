<script setup lang="ts">
import type { Agent, TaskInfo } from '../types'
import { computed } from 'vue'
import AppBadge from './ui/AppBadge.vue'

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
    { key: 'pending', title: 'Pending', icon: '○', cards: pending },
    { key: 'inProgress', title: 'In Progress', icon: '●', cards: inProgress },
    { key: 'completed', title: 'Completed', icon: '✓', cards: completed },
  ]
})

const totalTasks = computed(() =>
  columns.value.reduce((sum, col) => sum + col.cards.length, 0),
)
</script>

<template>
  <div v-if="totalTasks > 0" class="grid grid-cols-1 md:grid-cols-3 gap-4">
    <div
      v-for="col in columns"
      :key="col.key"
      class="bg-white dark:bg-slate-900 rounded-lg border border-slate-200 dark:border-slate-700 flex flex-col min-h-[200px] max-h-[calc(100vh-250px)]"
    >
      <div class="flex items-center gap-2 px-3.5 py-3 border-b border-slate-200 dark:border-slate-700 flex-shrink-0">
        <span
          class="text-[10px] w-3.5 text-center"
          :class="col.key === 'inProgress' ? 'text-yellow-600 dark:text-yellow-400' : col.key === 'completed' ? 'text-green-600 dark:text-green-400' : 'text-slate-400 dark:text-slate-600'"
        >{{ col.icon }}</span>
        <span class="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">{{ col.title }}</span>
        <span class="ml-auto text-[11px] font-mono text-slate-400 dark:text-slate-600 bg-slate-100 dark:bg-slate-800 px-2 py-0.5 rounded-full">{{ col.cards.length }}</span>
      </div>
      <div class="p-2.5 flex flex-col gap-2 flex-1 min-h-0 overflow-y-auto">
        <div
          v-for="card in col.cards"
          :key="`${card.agent.sessionId}-${card.task.id}`"
          class="bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-2.5 cursor-pointer transition-all hover:border-slate-400 dark:hover:border-slate-600 hover:shadow-md dark:hover:shadow-[0_2px_8px_rgba(0,0,0,0.3)]"
          @click="$emit('select', card.agent)"
        >
          <div class="text-[13px] text-slate-900 dark:text-slate-100 leading-snug mb-2 break-words">
            {{ card.task.subject }}
          </div>
          <div class="flex items-center justify-between gap-2">
            <span class="text-[11px] font-mono text-slate-400 dark:text-slate-600 whitespace-nowrap overflow-hidden text-ellipsis min-w-0">{{ card.agent.projectName }}</span>
            <AppBadge :variant="card.agent.status" />
          </div>
        </div>
        <div v-if="col.cards.length === 0" class="py-6 text-center text-[13px] text-slate-400 dark:text-slate-600 italic">
          No tasks
        </div>
      </div>
    </div>
  </div>
  <p v-else class="text-center py-12 text-slate-400 dark:text-slate-600 text-sm">
    No tasks found across agents.
  </p>
</template>
