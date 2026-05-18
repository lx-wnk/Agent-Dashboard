<script setup lang="ts">
import type { Agent, TaskInfo } from '../types'
import { computed } from 'vue'
import AppBadge from './ui/AppBadge.vue'
import type { KanbanColumnKey, MovePayload } from '../composables/useKanbanKeyboard'
import { useKanbanKeyboard } from '../composables/useKanbanKeyboard'

const props = defineProps<{
  agents: Agent[]
}>()

defineEmits<{
  select: [agent: Agent]
}>()

interface KanbanCard {
  /** Stable card id: `${sessionId}-${taskId}` */
  id: string
  task: TaskInfo
  agent: Agent
  /** Column derived from task.status in the data model (before keyboard moves). */
  originalColumn: KanbanColumnKey
}

interface ColumnDef {
  key: KanbanColumnKey
  title: string
  icon: string
  cards: KanbanCard[]
}

// ─── Keyboard navigation ────────────────────────────────────────────────────

/**
 * KanbanBoard task cards are read-only from the server perspective — TaskInfo
 * statuses are set by the Go session parser, not by this UI. The composable
 * maintains local display state; the SSE stream restores server truth on the
 * next broadcast.
 */
function onCommit(_payload: MovePayload) {
  // Intentionally local-only: no writable API endpoint for TaskInfo statuses.
}

const kb = useKanbanKeyboard(onCommit)

// ─── Column data ─────────────────────────────────────────────────────────────

const allCards = computed<KanbanCard[]>(() => {
  const cards: KanbanCard[] = []
  for (const agent of props.agents) {
    for (const task of agent.tasks) {
      let col: KanbanColumnKey
      switch (task.status) {
        case 'in_progress':
          col = 'inProgress'
          break
        case 'completed':
          col = 'completed'
          break
        default:
          col = 'pending'
          break
      }
      cards.push({
        id: `${agent.sessionId}-${task.id}`,
        task,
        agent,
        originalColumn: col,
      })
    }
  }
  return cards
})

const columns = computed<ColumnDef[]>(() => {
  const pending: KanbanCard[] = []
  const inProgress: KanbanCard[] = []
  const completed: KanbanCard[] = []

  for (const card of allCards.value) {
    const effectiveCol = kb.displayColumn(card.id, card.originalColumn)
    switch (effectiveCol) {
      case 'pending':
        pending.push(card)
        break
      case 'inProgress':
        inProgress.push(card)
        break
      case 'completed':
        completed.push(card)
        break
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
    <!-- Screen-reader live region for move mode announcements -->
    <div
      role="status"
      aria-live="assertive"
      aria-atomic="true"
      class="sr-only"
    >
      {{ kb.announcement.value }}
    </div>

    <div
      v-for="col in columns"
      :key="col.key"
      role="group"
      :aria-label="`${col.title} column, ${col.cards.length} task${col.cards.length !== 1 ? 's' : ''}`"
      class="bg-white dark:bg-slate-900 rounded-lg border border-slate-200 dark:border-slate-700 flex flex-col min-h-[200px] max-h-[calc(100vh-250px)]"
    >
      <div
        class="flex items-center gap-2 px-3.5 py-3 border-b border-slate-200 dark:border-slate-700 flex-shrink-0"
        aria-hidden="true"
      >
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
          :key="card.id"
          tabindex="0"
          role="button"
          :aria-label="`${card.task.subject}, ${card.agent.projectName}. ${kb.isPickedUp(card.id) ? 'Picked up. Use arrow keys to move, Enter to drop, Escape to cancel.' : 'Press Enter or Space to pick up and move to another column.'}`"
          :aria-grabbed="kb.isPickedUp(card.id) ? 'true' : 'false'"
          :class="[
            'bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-2.5 cursor-pointer transition-all',
            kb.isPickedUp(card.id)
              ? 'border-blue-500 dark:border-blue-400 ring-2 ring-blue-500 dark:ring-blue-400 shadow-lg'
              : 'hover:border-slate-400 dark:hover:border-slate-600 hover:shadow-md dark:hover:shadow-[0_2px_8px_rgba(0,0,0,0.3)]',
          ]"
          @click="$emit('select', card.agent)"
          @keydown="(e) => kb.handleKeydown(e, card.id, col.key, card.task.subject, e.currentTarget as HTMLElement)"
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
