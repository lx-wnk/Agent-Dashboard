<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { shortModel } from '../utils/format'
import AppModal from './ui/AppModal.vue'

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
const resumePrompts = ref<Record<string, string>>({})
const spawning = ref<string | null>(null)
const resumeMsg = ref<Record<string, string>>({})
const resumeError = ref<Record<string, boolean>>({})

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

async function resumeSession(s: SessionInfo) {
  const prompt = resumePrompts.value[s.sessionId]?.trim()
  if (!prompt || spawning.value)
    return

  spawning.value = s.sessionId
  resumeMsg.value[s.sessionId] = ''
  resumeError.value[s.sessionId] = false

  try {
    const res = await fetch('/api/agents/spawn', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        prompt,
        cwd: s.projectPath,
        resumeSessionId: s.sessionId,
        enableChannel: true,
      }),
    })
    const data = await res.json()
    if (!res.ok)
      throw new Error(data.error || 'Spawn failed')

    resumeMsg.value[s.sessionId] = `PID ${data.pid} spawned`
    resumePrompts.value[s.sessionId] = ''
    loadSessions()
    setTimeout(() => {
      resumeMsg.value[s.sessionId] = ''
    }, 4000)
  }
  catch (err: unknown) {
    resumeError.value[s.sessionId] = true
    resumeMsg.value[s.sessionId] = err instanceof Error ? err.message : 'Failed'
    setTimeout(() => {
      resumeMsg.value[s.sessionId] = ''
    }, 4000)
  }
  finally {
    spawning.value = null
  }
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
            class="bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 rounded-md px-3 py-2.5"
          >
            <div class="flex justify-between items-center mb-1">
              <span class="text-sm font-semibold text-slate-900 dark:text-slate-100">{{ s.projectName }}</span>
              <span class="text-[11px] text-slate-400 dark:text-slate-600">{{ formatDate(s.lastModified) }}</span>
            </div>
            <code class="font-mono text-xs text-slate-500 dark:text-slate-400 truncate block mb-1">{{ shortenPath(s.projectPath) }}</code>
            <p v-if="s.firstPrompt" class="text-xs text-slate-600 dark:text-slate-400 line-clamp-2 mb-1.5">
              {{ s.firstPrompt }}
            </p>
            <pre v-if="s.lastResponse" class="text-[11px] font-mono text-slate-400 dark:text-slate-600 bg-white dark:bg-slate-900 border-l-2 border-slate-200 dark:border-slate-700 px-2 py-1.5 mb-1.5 rounded-r leading-relaxed whitespace-pre-wrap break-words max-h-[5.5lh] overflow-y-auto">{{ s.lastResponse }}</pre>
            <div class="flex gap-1.5 mb-2">
              <span v-if="s.model" class="text-[10px] px-1.5 py-px rounded bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 uppercase tracking-wide font-mono">{{ shortModel(s.model) }}</span>
              <span v-if="s.costEstimate > 0" class="text-[10px] px-1.5 py-px rounded bg-slate-100 dark:bg-slate-800 text-green-600 dark:text-green-400 font-mono">${{ s.costEstimate.toFixed(2) }}</span>
              <span class="text-[10px] px-1.5 py-px rounded bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 font-mono" :title="s.sessionId">{{ s.sessionId.slice(0, 8) }}</span>
            </div>
            <div class="flex gap-1.5">
              <input
                v-model="resumePrompts[s.sessionId]"
                class="flex-1 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded text-slate-900 dark:text-slate-100 text-xs px-2 py-1 focus:outline-none focus:border-green-500 placeholder:text-slate-400 dark:placeholder:text-slate-600"
                type="text"
                placeholder="Follow-up prompt..."
                @keydown.enter="resumeSession(s)"
              >
              <button
                type="button"
                class="flex-shrink-0 bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 border border-slate-200 dark:border-slate-700 rounded px-3 py-1 text-xs font-semibold cursor-pointer hover:text-green-600 dark:hover:text-green-400 hover:border-green-500 dark:hover:border-green-500 disabled:opacity-40 disabled:cursor-not-allowed"
                :disabled="!resumePrompts[s.sessionId]?.trim() || spawning === s.sessionId"
                @click="resumeSession(s)"
              >
                {{ spawning === s.sessionId ? '...' : 'Resume' }}
              </button>
            </div>
            <p v-if="resumeMsg[s.sessionId]" class="text-[11px] mt-1" :class="resumeError[s.sessionId] ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'">
              {{ resumeMsg[s.sessionId] }}
            </p>
          </div>
        </div>
      </div>
    </div>
  </AppModal>
</template>
