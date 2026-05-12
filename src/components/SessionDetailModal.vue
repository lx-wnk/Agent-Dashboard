<script setup lang="ts">
import type { OutputMessage } from '../types'
import { ref, watch } from 'vue'
import { formatCost, shortModel } from '../utils/format'
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

const props = defineProps<{
  open: boolean
  session: SessionInfo | null
  homeDir: string
}>()

const emit = defineEmits<{ close: []; resumed: [] }>()

const messages = ref<OutputMessage[]>([])
const isLoading = ref(false)
const resumePrompt = ref('')
const spawning = ref(false)
const statusMsg = ref('')
const statusIsError = ref(false)

function shortenPath(path: string): string {
  if (props.homeDir && path.startsWith(props.homeDir))
    return `~${path.slice(props.homeDir.length)}`
  return path
}

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

async function fetchMessages(sessionId: string) {
  isLoading.value = true
  messages.value = []
  try {
    const res = await fetch(`/api/agents/${sessionId}/output`)
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    const data = await res.json()
    messages.value = (data.messages as OutputMessage[]).filter(m => m.content !== '')
  }
  catch { /* ignore */ }
  finally {
    isLoading.value = false
  }
}

watch(
  () => [props.open, props.session?.sessionId] as const,
  ([open, sessionId]) => {
    if (open && sessionId) {
      resumePrompt.value = ''
      statusMsg.value = ''
      statusIsError.value = false
      fetchMessages(sessionId)
    }
    else {
      messages.value = []
    }
  },
)

async function resumeSession() {
  if (!props.session || !resumePrompt.value.trim() || spawning.value)
    return

  spawning.value = true
  statusMsg.value = ''
  statusIsError.value = false

  try {
    const res = await fetch('/api/agents/spawn', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        prompt: resumePrompt.value.trim(),
        cwd: props.session.projectPath,
        resumeSessionId: props.session.sessionId,
        enableChannel: true,
      }),
    })
    const data = await res.json()
    if (!res.ok)
      throw new Error(data.error || 'Spawn failed')

    statusMsg.value = `PID ${data.pid} spawned`
    statusIsError.value = false
    resumePrompt.value = ''
    emit('resumed')
    setTimeout(() => {
      statusMsg.value = ''
    }, 4000)
  }
  catch (err: unknown) {
    statusIsError.value = true
    statusMsg.value = err instanceof Error ? err.message : 'Failed'
    setTimeout(() => {
      statusMsg.value = ''
    }, 4000)
  }
  finally {
    spawning.value = false
  }
}
</script>

<template>
  <AppModal :open="open" @close="emit('close')">
    <div class="bg-white dark:bg-slate-900 rounded-xl border border-slate-200 dark:border-slate-700 shadow-[0_8px_40px_rgba(0,0,0,0.5)] w-full max-w-2xl max-h-[85vh] flex flex-col overflow-hidden">
      <!-- Header -->
      <header class="flex justify-between items-start px-5 py-4 border-b border-slate-200 dark:border-slate-700 flex-shrink-0 gap-3">
        <div class="min-w-0 flex-1">
          <h2 class="text-sm font-semibold text-slate-900 dark:text-slate-100 leading-snug line-clamp-2 mb-1.5">
            {{ session?.firstPrompt ?? session?.projectName ?? 'Session' }}
          </h2>
          <div class="flex flex-wrap items-center gap-1.5">
            <code class="font-mono text-[11px] text-slate-500 dark:text-slate-400 truncate max-w-[280px]">{{ session ? shortenPath(session.projectPath) : '' }}</code>
            <span v-if="session?.model" class="text-[10px] px-1.5 py-px rounded bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 uppercase tracking-wide font-mono">{{ shortModel(session.model) }}</span>
            <span v-if="session && session.costEstimate > 0" class="text-[10px] px-1.5 py-px rounded bg-slate-100 dark:bg-slate-800 text-green-600 dark:text-green-400 font-mono">{{ formatCost(session.costEstimate) }}</span>
            <span v-if="session" class="text-[10px] px-1.5 py-px rounded bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600">{{ formatDate(session.lastModified) }}</span>
            <span v-if="session" class="text-[10px] px-1.5 py-px rounded bg-slate-100 dark:bg-slate-800 text-slate-400 dark:text-slate-600 font-mono" :title="session.sessionId">{{ session.sessionId.slice(0, 8) }}</span>
          </div>
        </div>
        <button
          type="button"
          class="bg-transparent border-none text-slate-400 dark:text-slate-600 text-2xl cursor-pointer px-1 leading-none hover:text-slate-900 dark:hover:text-slate-100 flex-shrink-0"
          @click="emit('close')"
        >
          &times;
        </button>
      </header>

      <!-- Body: message list -->
      <div class="flex-1 overflow-y-auto px-4 py-3">
        <div v-if="isLoading" class="text-center py-12 text-slate-400 dark:text-slate-600 text-sm">
          Loading transcript...
        </div>
        <div v-else-if="messages.length === 0" class="text-center py-12 text-slate-400 dark:text-slate-600 text-sm">
          No messages available.
        </div>
        <template v-else>
          <template v-for="(msg, i) in messages" :key="i">
            <!-- user / human -->
            <div v-if="msg.role === 'human'" class="flex justify-end mb-2">
              <div class="bg-blue-600 text-white text-[13px] px-3 py-2 rounded-lg max-w-[80%] whitespace-pre-wrap break-words">
                {{ msg.content }}
              </div>
            </div>
            <!-- assistant -->
            <div v-else-if="msg.role === 'assistant'" class="flex justify-start mb-2">
              <div class="bg-slate-100 dark:bg-slate-800 text-slate-900 dark:text-slate-100 text-[13px] px-3 py-2 rounded-lg max-w-[80%] whitespace-pre-wrap break-words">
                {{ msg.content }}
              </div>
            </div>
            <!-- tool_call -->
            <div v-else-if="msg.role === 'tool_call'" class="text-[11px] font-mono text-slate-400 dark:text-slate-600 mb-1 pl-1">
              &#9881; {{ msg.toolName ?? 'tool' }}
            </div>
            <!-- tool_result: skip -->
            <!-- other roles: skip -->
          </template>
        </template>
      </div>

      <!-- Footer: resume prompt -->
      <footer class="flex-shrink-0 border-t border-slate-200 dark:border-slate-700 px-4 py-3">
        <div class="flex gap-1.5">
          <input
            v-model="resumePrompt"
            class="flex-1 bg-white dark:bg-slate-800 border border-slate-200 dark:border-slate-700 rounded text-slate-900 dark:text-slate-100 text-xs px-2 py-1.5 focus:outline-none focus:border-green-500 placeholder:text-slate-400 dark:placeholder:text-slate-600"
            type="text"
            placeholder="Follow-up prompt to resume session..."
            @keydown.enter="resumeSession"
          >
          <button
            type="button"
            class="flex-shrink-0 bg-slate-100 dark:bg-slate-800 text-slate-600 dark:text-slate-400 border border-slate-200 dark:border-slate-700 rounded px-3 py-1.5 text-xs font-semibold cursor-pointer hover:text-green-600 dark:hover:text-green-400 hover:border-green-500 dark:hover:border-green-500 disabled:opacity-40 disabled:cursor-not-allowed"
            :disabled="!resumePrompt.trim() || spawning"
            @click="resumeSession"
          >
            {{ spawning ? '...' : 'Resume' }}
          </button>
        </div>
        <p v-if="statusMsg" class="text-[11px] mt-1.5" :class="statusIsError ? 'text-red-600 dark:text-red-400' : 'text-green-600 dark:text-green-400'">
          {{ statusMsg }}
        </p>
      </footer>
    </div>
  </AppModal>
</template>
