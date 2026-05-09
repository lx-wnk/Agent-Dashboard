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
const inputRef = ref<HTMLInputElement | null>(null)
const selectedIdx = ref(0)

interface SearchResults {
  tasks: PipelineTask[]
  agents: Agent[]
}

const results = ref<SearchResults>({ tasks: [], agents: [] })
const loading = ref(false)
let debounceHandle: ReturnType<typeof setTimeout> | null = null

const flatResults = computed((): Array<{ type: 'task', item: PipelineTask } | { type: 'agent', item: Agent }> => {
  return [
    ...results.value.tasks.map(t => ({ type: 'task' as const, item: t })),
    ...results.value.agents.map(a => ({ type: 'agent' as const, item: a })),
  ]
})

async function search(q: string) {
  if (!q.trim()) {
    results.value = { tasks: [], agents: [] }
    return
  }
  loading.value = true
  try {
    const res = await fetch(`/api/search?q=${encodeURIComponent(q)}&type=all&limit=10`)
    results.value = await res.json() as SearchResults
    selectedIdx.value = 0
  }
  finally {
    loading.value = false
  }
}

watch(query, (q) => {
  if (debounceHandle)
    clearTimeout(debounceHandle)
  debounceHandle = setTimeout(search, 200, q)
})

function onKeydown(e: KeyboardEvent) {
  // Open on Cmd+K or Ctrl+K
  if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
    e.preventDefault()
    open.value = !open.value
    if (open.value)
      nextTick(() => inputRef.value?.focus())
    return
  }
  if (!open.value)
    return
  if (e.key === 'Escape') {
    open.value = false
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
    if (selected.type === 'task')
      emit('navigateTask', selected.item)
    else
      emit('navigateAgent', selected.item)
    open.value = false
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <AppModal :open="open" :z-index="2000" @close="open = false">
    <div class="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-2xl w-full max-w-lg overflow-hidden">
      <div class="flex items-center gap-2 px-4 py-3 border-b border-slate-200 dark:border-slate-700">
        <span class="text-slate-400 text-sm">⌘K</span>
        <input
          ref="inputRef"
          v-model="query"
          type="text"
          placeholder="Search tasks and agents…"
          class="flex-1 bg-transparent text-sm text-slate-900 dark:text-slate-100 outline-none placeholder:text-slate-400"
        >
        <span v-if="loading" class="text-xs text-slate-400">Searching…</span>
      </div>
      <div class="max-h-80 overflow-y-auto">
        <template v-if="flatResults.length === 0 && query">
          <p class="px-4 py-3 text-sm text-slate-400">
            No results for "{{ query }}"
          </p>
        </template>
        <template v-else>
          <div v-if="results.tasks.length > 0" class="px-3 pt-2 pb-1 text-[10px] font-semibold uppercase tracking-wide text-slate-400">
            Tasks
          </div>
          <button
            v-for="(item, idx) in flatResults"
            :key="`${item.type}-${item.type === 'task' ? item.item.id : (item.item as Agent).sessionId}`"
            type="button"
            class="w-full text-left px-4 py-2 text-sm flex items-center gap-3 transition-colors"
            :class="selectedIdx === idx
              ? 'bg-blue-50 dark:bg-blue-900/30 text-blue-700 dark:text-blue-300'
              : 'text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-800'"
            @click="() => {
              if (item.type === 'task') { emit('navigateTask', item.item); open = false }
              else { emit('navigateAgent', item.item as Agent); open = false }
            }"
            @mouseenter="selectedIdx = idx"
          >
            <span class="text-[10px] uppercase tracking-wide text-slate-400 w-10 flex-shrink-0">
              {{ item.type === 'task' ? 'Task' : 'Agent' }}
            </span>
            <span class="truncate">
              {{ item.type === 'task' ? (item.item as PipelineTask).title : (item.item as Agent).projectName }}
            </span>
            <span class="ml-auto text-[10px] text-slate-400">
              {{ item.type === 'task' ? (item.item as PipelineTask).currentStage : (item.item as Agent).status }}
            </span>
          </button>
          <div v-if="results.agents.length > 0 && results.tasks.length > 0" class="px-3 pt-2 pb-1 text-[10px] font-semibold uppercase tracking-wide text-slate-400 border-t border-slate-100 dark:border-slate-800 mt-1">
            Agents
          </div>
        </template>
      </div>
      <div class="px-4 py-2 border-t border-slate-100 dark:border-slate-800 flex gap-3 text-[10px] text-slate-400">
        <span>↑↓ navigate</span>
        <span>↵ open</span>
        <span>Esc close</span>
      </div>
    </div>
  </AppModal>
</template>
