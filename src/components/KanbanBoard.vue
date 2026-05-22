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
  id: string
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
      const card: KanbanCard = { id: `${agent.sessionId}-${task.id}`, task, agent }
      switch (task.status) {
        case 'in_progress':
          inProgress.push(card)
          break
        case 'completed':
          completed.push(card)
          break
        case 'pending':
          pending.push(card)
          break
        default:
          // Unknown status — fall back to pending column so the card stays visible.
          // Go backend emits statuses beyond the typed set (e.g. on_hold, cancelled).
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
      role="group"
      :aria-label="`${col.title} column, ${col.cards.length} task${col.cards.length !== 1 ? 's' : ''}`"
      class="bg-card rounded-lg border border-line flex flex-col min-h-[200px] max-h-[calc(100vh-250px)]"
    >
      <span class="sr-only" aria-live="polite">{{ col.title }} column, {{ col.cards.length }} tasks</span>
      <div
        class="flex items-center gap-2 px-3.5 py-3 border-b border-line flex-shrink-0"
        aria-hidden="true"
      >
        <span
          class="text-[10px] w-3.5 text-center"
          :class="col.key === 'inProgress' ? 'text-yellow-600 dark:text-yellow-400' : col.key === 'completed' ? 'text-green-600 dark:text-green-400' : 'text-fg-mute'"
        >{{ col.icon }}</span>
        <span class="text-xs font-semibold uppercase tracking-wider text-fg-mute">{{ col.title }}</span>
        <span class="ml-auto text-[11px] font-mono text-fg-mute bg-raised px-2 py-0.5 rounded-full">{{ col.cards.length }}</span>
      </div>
      <div class="p-2.5 flex flex-col gap-2 flex-1 min-h-0 overflow-y-auto">
        <div
          v-for="card in col.cards"
          :key="card.id"
          tabindex="0"
          role="button"
          :aria-label="`${card.task.subject} (${card.agent.projectName})`"
          class="bg-app border border-line rounded-md px-3 py-2.5 cursor-pointer transition-all hover:border-slate-400 dark:hover:border-slate-600 hover:shadow-md dark:hover:shadow-[0_2px_8px_rgba(0,0,0,0.3)] focus-visible:outline-2 focus-visible:outline-blue-500 focus-visible:outline-offset-[-2px]"
          @click="$emit('select', card.agent)"
          @keydown.enter="$emit('select', card.agent)"
          @keydown.space.prevent="$emit('select', card.agent)"
        >
          <div class="text-[13px] text-fg leading-snug mb-2 break-words">
            {{ card.task.subject }}
          </div>
          <div class="flex items-center justify-between gap-2">
            <span class="text-[11px] font-mono text-fg-mute whitespace-nowrap overflow-hidden text-ellipsis min-w-0">{{ card.agent.projectName }}</span>
            <AppBadge :variant="card.agent.status" />
          </div>
        </div>
        <div v-if="col.cards.length === 0" class="py-6 text-center text-[13px] text-fg-mute italic">
          No tasks
        </div>
      </div>
    </div>
  </div>
  <p v-else class="text-center py-12 text-fg-mute text-sm">
    No tasks found across agents.
  </p>
</template>
