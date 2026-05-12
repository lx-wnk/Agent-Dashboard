<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { formatCost, shortModel } from '../utils/format'
import AppModal from './ui/AppModal.vue'
import SessionDetailModal from './SessionDetailModal.vue'

interface SessionInfo {
  sessionId: string
  projectPath: string
  projectName: string
  lastModified: string
  model: string | null
  firstPrompt: string | null
  lastResponse: string | null
  totalInputTokens: number
  totalOutputTokens: number
  costEstimate: number
  isRunning: boolean
}

const props = defineProps<{ open: boolean, homeDir: string }>()
const emit = defineEmits<{ close: [] }>()

const sessions = ref<SessionInfo[]>([])
const isLoading = ref(false)
const search = ref('')
const selectedSession = ref<SessionInfo | null>(null)

const filtered = computed(() => {
  const q = search.value.toLowerCase()
  // Exclude currently running sessions — they don't need resuming
  const inactive = sessions.value.filter(s => !s.isRunning)
  if (!q)
    return inactive
  return inactive.filter(s =>
    s.projectName.toLowerCase().includes(q)
    || s.projectPath.toLowerCase().includes(q)
    || (s.firstPrompt && s.firstPrompt.toLowerCase().includes(q)),
  )
})

function formatDate(iso: string): string {
  const d = new Date(iso)
  const now = new Date()
  const diffMs = now.getTime() - d.getTime()
  const diffH = diffMs / 3600000

  if (diffH < 1)
    return `${Math.round(diffMs / 60000)}m ago`
  if (diffH < 24)
    return `${Math.round(diffH)}h ago`
  if (diffH < 168)
    return `${Math.round(diffH / 24)}d ago`
  return d.toLocaleDateString()
}

function shortenPath(path: string): string {
  if (props.homeDir && path.startsWith(props.homeDir)) {
    return `~${path.slice(props.homeDir.length)}`
  }
  return path
}

async function loadSessions() {
  isLoading.value = true
  try {
    const res = await fetch('/api/sessions')
    if (res.ok)
      sessions.value = await res.json()
  }
  catch { /* ignore */ }
  isLoading.value = false
}

let refreshInterval: ReturnType<typeof setInterval> | null = null

watch(() => props.open, (isOpen) => {
  if (isOpen) {
    loadSessions()
    refreshInterval = setInterval(loadSessions, 15_000)
  }
  else {
    if (refreshInterval) {
      clearInterval(refreshInterval)
      refreshInterval = null
    }
  }
})

onUnmounted(() => {
  if (refreshInterval)
    clearInterval(refreshInterval)
})

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.open)
    emit('close')
}
onMounted(() => window.addEventListener('keydown', onKeydown))
onUnmounted(() => window.removeEventListener('keydown', onKeydown))
</script>

<template>
  <AppModal :open="open" @close="emit('close')">
    <div class="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-3xl max-h-[80vh] flex flex-col overflow-hidden">
      <header class="flex justify-between items-center px-5 py-4 border-b border-slate-200 dark:border-slate-700 flex-shrink-0">
        <h2 class="text-lg font-semibold text-slate-900 dark:text-slate-100">
          Past Sessions
        </h2>
        <button type="button" class="bg-transparent border-none text-slate-400 dark:text-slate-600 text-2xl cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100" @click="emit('close')">
          &times;
        </button>
      </header>

      <div class="flex-1 overflow-y-auto p-5">
        <div class="mb-3">
          <input
            v-model="search"
            class="w-full bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded text-slate-900 dark:text-slate-100 text-[13px] px-2.5 py-2 focus:outline-none focus:border-green-500 placeholder:text-slate-400 dark:placeholder:text-slate-600"
            type="text"
            placeholder="Filter by project or prompt..."
          >
        </div>

        <p v-if="isLoading" class="text-center py-12 text-slate-400 dark:text-slate-600 text-sm">
          Loading sessions...
        </p>
        <p v-else-if="filtered.length === 0" class="text-center py-12 text-slate-400 dark:text-slate-600 text-sm">
          No sessions found.
        </p>

        <div v-else class="flex flex-col gap-2">
          <div
            v-for="s in filtered"
            :key="s.sessionId"
            class="bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-2.5 cursor-pointer hover:border-slate-400 dark:hover:border-slate-600 transition-colors"
            @click="selectedSession = s"
          >
            <!-- Title: firstPrompt prominent -->
            <p v-if="s.firstPrompt" class="text-sm font-semibold text-slate-900 dark:text-slate-100 line-clamp-2 mb-1 leading-snug">
              {{ s.firstPrompt }}
            </p>
            <span v-else class="text-sm font-semibold text-slate-900 dark:text-slate-100 mb-1 block">{{ s.projectName }}</span>

            <!-- Path -->
            <code class="font-mono text-xs text-slate-500 dark:text-slate-400 truncate block mb-1.5">{{ shortenPath(s.projectPath) }}</code>

            <!-- Last response snippet -->
            <pre v-if="s.lastResponse" class="text-[11px] font-mono text-slate-400 dark:text-slate-600 bg-white dark:bg-slate-900 border-l-2 border-slate-200 dark:border-slate-700 px-2 py-1.5 mb-2 rounded-r leading-relaxed whitespace-pre-wrap break-words max-h-[5.5lh] overflow-y-auto">{{ s.lastResponse }}</pre>

            <!-- Metadata badges -->
            <div class="flex flex-wrap gap-1.5 items-center">
              <span v-if="s.model" class="text-[10px] px-1.5 py-px rounded bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 uppercase tracking-wide font-mono">{{ shortModel(s.model) }}</span>
              <span v-if="s.costEstimate > 0" class="text-[10px] px-1.5 py-px rounded bg-slate-100 dark:bg-slate-800 text-green-600 dark:text-green-400 font-mono">{{ formatCost(s.costEstimate) }}</span>
              <span class="text-[10px] px-1.5 py-px rounded bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 font-mono" :title="s.sessionId">{{ s.sessionId.slice(0, 8) }}</span>
              <span class="ml-auto text-[10px] text-slate-400 dark:text-slate-600">{{ formatDate(s.lastModified) }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </AppModal>

  <SessionDetailModal
    :open="selectedSession !== null"
    :session="selectedSession"
    :home-dir="homeDir"
    @close="selectedSession = null"
    @resumed="selectedSession = null; loadSessions()"
  />
</template>
