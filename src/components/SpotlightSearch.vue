<script setup lang="ts">
import type { Agent, PipelineTask } from '../types'
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import AppModal from './ui/AppModal.vue'

const emit = defineEmits<{
  navigateTask: [task: PipelineTask]
  navigateAgent: [agent: Agent]
}>()

const open = ref(false)
const query = ref('')
const dialogRef = ref<HTMLDivElement | null>(null)
const inputRef = ref<HTMLInputElement | null>(null)
const selectedIdx = ref(0)

// Focus management: store the element that triggered the dialog so we can restore focus on close
let previouslyFocusedElement: Element | null = null

interface SearchResults {
  tasks: PipelineTask[]
  agents: Agent[]
}

const results = ref<SearchResults>({ tasks: [], agents: [] })
const loading = ref(false)
let debounceHandle: ReturnType<typeof setTimeout> | null = null
let abortController: AbortController | null = null

const flatResults = computed((): Array<{ type: 'task', item: PipelineTask } | { type: 'agent', item: Agent }> => {
  return [
    ...results.value.tasks.map(t => ({ type: 'task' as const, item: t })),
    ...results.value.agents.map(a => ({ type: 'agent' as const, item: a })),
  ]
})

// Clamp selectedIdx when results shrink to avoid out-of-bounds
watch(flatResults, (newResults) => {
  selectedIdx.value = Math.min(selectedIdx.value, Math.max(0, newResults.length - 1))
})

async function search(q: string) {
  abortController?.abort()
  abortController = new AbortController()

  if (!q.trim()) {
    results.value = { tasks: [], agents: [] }
    return
  }
  loading.value = true
  try {
    const res = await fetch(`/api/search?q=${encodeURIComponent(q)}&type=all&limit=10`, {
      signal: abortController.signal,
    })
    if (!res.ok) {
      results.value = { tasks: [], agents: [] }
      return
    }
    results.value = await res.json() as SearchResults
    selectedIdx.value = 0
  }
  catch (e) {
    if (e instanceof Error && e.name === 'AbortError')
      return // ignore aborted requests
    results.value = { tasks: [], agents: [] }
  }
  finally {
    loading.value = false
  }
}

function activate(result: typeof flatResults.value[number]) {
  if (result.type === 'task')
    emit('navigateTask', result.item)
  else
    emit('navigateAgent', result.item)
  closeDialog()
}

function openDialog() {
  previouslyFocusedElement = document.activeElement
  open.value = true
  void nextTick(() => inputRef.value?.focus())
}

function closeDialog() {
  open.value = false
  query.value = ''
  results.value = { tasks: [], agents: [] }
  // Restore focus to the element that was focused before the dialog opened
  if (previouslyFocusedElement instanceof HTMLElement) {
    previouslyFocusedElement.focus()
  }
}

watch(query, (q) => {
  if (debounceHandle)
    clearTimeout(debounceHandle)
  debounceHandle = setTimeout(() => {
    search(q).catch(() => {
      results.value = { tasks: [], agents: [] }
    })
  }, 200)
})

// Focus trap: cycle focus among interactive elements within the dialog
function trapFocus(e: KeyboardEvent) {
  if (e.key !== 'Tab' || !dialogRef.value)
    return

  const focusable = dialogRef.value.querySelectorAll<HTMLElement>(
    'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])',
  )
  const first = focusable[0]
  const last = focusable[focusable.length - 1]

  if (e.shiftKey) {
    if (document.activeElement === first) {
      e.preventDefault()
      last?.focus()
    }
  }
  else {
    if (document.activeElement === last) {
      e.preventDefault()
      first?.focus()
    }
  }
}

function onKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
    e.preventDefault()
    if (open.value)
      closeDialog()
    else
      openDialog()
    return
  }
  if (!open.value)
    return
  if (e.key === 'Escape') {
    closeDialog()
    return
  }
  if (e.key === 'Tab') {
    trapFocus(e)
    return
  }
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    selectedIdx.value = Math.min(selectedIdx.value + 1, flatResults.value.length - 1)
    return
  }
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    selectedIdx.value = Math.max(selectedIdx.value - 1, 0)
    return
  }
  if (e.key === 'Enter') {
    e.preventDefault()
    const selected = flatResults.value[selectedIdx.value]
    if (!selected)
      return
    activate(selected)
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <AppModal :open="open" :z-index="2000" @close="closeDialog">
    <div
      ref="dialogRef"
      role="dialog"
      aria-modal="true"
      aria-label="Quick search"
      class="bg-card rounded-xl border border-line shadow-2xl w-full max-w-lg overflow-hidden"
    >
      <!-- Live region for result count -->
      <div aria-live="polite" class="sr-only">
        {{ flatResults.length }} results
      </div>
      <div class="flex items-center gap-2 px-4 py-3 border-b border-line">
        <span class="text-slate-400 text-sm" aria-hidden="true">⌘K</span>
        <input
          ref="inputRef"
          v-model="query"
          type="text"
          role="combobox"
          :aria-expanded="flatResults.length > 0"
          aria-controls="spotlight-listbox"
          aria-autocomplete="list"
          :aria-activedescendant="selectedIdx >= 0 && flatResults.length > 0 ? `spotlight-opt-${selectedIdx}` : undefined"
          placeholder="Search tasks and agents…"
          class="flex-1 bg-transparent text-sm text-fg outline-none placeholder:text-slate-400"
        >
        <span v-if="loading" class="text-xs text-slate-400">Searching…</span>
      </div>
      <div
        id="spotlight-listbox"
        role="listbox"
        :aria-busy="loading"
        class="max-h-80 overflow-y-auto"
      >
        <template v-if="flatResults.length === 0 && query">
          <p class="px-4 py-3 text-sm text-slate-400">
            No results for "{{ query }}"
          </p>
        </template>
        <template v-else>
          <!-- Tasks section -->
          <template v-if="results.tasks.length > 0">
            <div class="px-3 pt-2 pb-1 text-[10px] font-semibold uppercase tracking-wide text-slate-400">
              Tasks
            </div>
            <button
              v-for="(task, idx) in results.tasks"
              :id="`spotlight-opt-${idx}`"
              :key="`task-${task.id}`"
              type="button"
              role="option"
              :aria-selected="selectedIdx === idx"
              class="w-full text-left px-4 py-2 text-sm flex items-center gap-3 transition-colors"
              :class="selectedIdx === idx
                ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300'
                : 'text-fg-soft hover:bg-slate-50 dark:hover:bg-slate-800'"
              @click="activate({ type: 'task', item: task })"
              @mouseenter="selectedIdx = idx"
            >
              <span class="text-[10px] uppercase tracking-wide text-slate-400 w-10 flex-shrink-0">Task</span>
              <span class="truncate">{{ task.title }}</span>
              <span class="ml-auto text-[10px] text-slate-400">{{ task.currentStage }}</span>
            </button>
          </template>

          <!-- Agents section -->
          <template v-if="results.agents.length > 0">
            <div
              class="px-3 pt-2 pb-1 text-[10px] font-semibold uppercase tracking-wide text-slate-400"
              :class="{ 'border-t border-line mt-1': results.tasks.length > 0 }"
            >
              Agents
            </div>
            <button
              v-for="(agent, idx) in results.agents"
              :id="`spotlight-opt-${results.tasks.length + idx}`"
              :key="`agent-${agent.sessionId}`"
              type="button"
              role="option"
              :aria-selected="selectedIdx === (results.tasks.length + idx)"
              class="w-full text-left px-4 py-2 text-sm flex items-center gap-3 transition-colors"
              :class="selectedIdx === (results.tasks.length + idx)
                ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300'
                : 'text-fg-soft hover:bg-slate-50 dark:hover:bg-slate-800'"
              @click="activate({ type: 'agent', item: agent })"
              @mouseenter="selectedIdx = results.tasks.length + idx"
            >
              <span class="text-[10px] uppercase tracking-wide text-slate-400 w-10 flex-shrink-0">Agent</span>
              <span class="truncate">{{ agent.projectName }}</span>
              <span class="ml-auto text-[10px] text-slate-400">{{ agent.status }}</span>
            </button>
          </template>
        </template>
      </div>
      <div class="px-4 py-2 border-t border-line flex gap-3 text-[10px] text-slate-400">
        <span>↑↓ navigate</span>
        <span>↵ open</span>
        <span>Esc close</span>
      </div>
    </div>
  </AppModal>
</template>

<style scoped>
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border-width: 0;
}
</style>
