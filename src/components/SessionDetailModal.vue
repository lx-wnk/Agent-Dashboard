<script setup lang="ts">
import type { OutputMessage } from '../types'
import { nextTick, onUnmounted, ref, watch } from 'vue'
import { toast } from '../composables/useToast'
import { errorMessage } from '../utils/errorMessage'
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

const emit = defineEmits<{ close: [], resumed: [] }>()

const messages = ref<OutputMessage[]>([])
const isLoading = ref(false)
const resumePrompt = ref('')
const spawning = ref(false)
const statusMsg = ref('')
const statusIsError = ref(false)
const scrollContainer = ref<HTMLElement | null>(null)
let abortCtrl: AbortController | null = null
onUnmounted(() => {
  abortCtrl?.abort()
  abortCtrl = null
})

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
  abortCtrl?.abort()
  abortCtrl = new AbortController()
  const { signal } = abortCtrl

  isLoading.value = true
  messages.value = []

  try {
    const res = await fetch(`/api/agents/${sessionId}/output`, { signal })
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    const data = await res.json()
    if (signal.aborted)
      return
    messages.value = (data.messages as OutputMessage[]).filter(
      m => m.content !== '' || m.role === 'tool_call',
    )
    await nextTick()
    if (scrollContainer.value)
      scrollContainer.value.scrollTop = scrollContainer.value.scrollHeight
  }
  catch (err: unknown) {
    if (signal.aborted)
      return
    toast.error(errorMessage(err, 'Failed to load transcript'))
  }
  finally {
    if (!signal.aborted)
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
      abortCtrl?.abort()
      abortCtrl = null
      messages.value = []
      isLoading.value = false
    }
  },
)

function handleResumeKeydown(e: KeyboardEvent) {
  if (e.isComposing || e.keyCode === 229)
    return // IME composition in progress — do not submit
  resumeSession()
}

async function resumeSession() {
  if (!props.session || spawning.value)
    return
  if (!resumePrompt.value.trim()) {
    statusIsError.value = true
    statusMsg.value = 'Please enter a prompt before resuming.'
    return
  }

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
    statusMsg.value = errorMessage(err, 'Failed')
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
  <AppModal :open="open" labelled-by="session-detail-title" @close="emit('close')">
    <!-- Header -->
    <header class="flex justify-between items-start px-5 py-4 border-b border-line shrink-0 gap-3">
      <div class="min-w-0 flex-1">
        <h2 id="session-detail-title" class="text-sm font-semibold text-fg leading-snug line-clamp-2 mb-1.5">
          {{ session?.firstPrompt ?? session?.projectName ?? 'Session' }}
        </h2>
        <div class="flex flex-wrap items-center gap-1.5">
          <code class="font-mono text-[11px] text-fg-mute truncate max-w-[280px]">{{ session ? shortenPath(session.projectPath) : '' }}</code>
          <span v-if="session?.model" class="text-[10px] px-1.5 py-px rounded bg-raised text-fg-mute uppercase tracking-wide font-mono">{{ shortModel(session.model) }}</span>
          <span v-if="session && session.costEstimate > 0" class="text-[10px] px-1.5 py-px rounded bg-raised text-green-600 dark:text-green-400 font-mono">{{ formatCost(session.costEstimate) }}</span>
          <span v-if="session" class="text-[10px] px-1.5 py-px rounded bg-raised text-fg-mute">{{ formatDate(session.lastModified) }}</span>
          <span v-if="session" class="text-[10px] px-1.5 py-px rounded bg-raised text-fg-mute font-mono" :title="session.sessionId">{{ session.sessionId.slice(0, 8) }}</span>
        </div>
      </div>
      <button
        type="button"
        aria-label="Close"
        class="bg-transparent border-none text-fg-mute text-2xl cursor-pointer px-1 leading-none hover:text-fg flex-shrink-0 min-h-[44px] min-w-[44px] flex items-center justify-center"
        @click="emit('close')"
      >
        &times;
      </button>
    </header>

    <!-- Body: message list -->
    <div ref="scrollContainer" class="flex-1 min-h-0 overflow-y-auto px-4 py-3">
      <div v-if="isLoading" class="text-center py-12 text-fg-mute text-sm">
        Loading transcript...
      </div>
      <div v-else-if="messages.length === 0" class="text-center py-12 text-fg-mute text-sm">
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
            <div class="bg-raised text-fg text-[13px] px-3 py-2 rounded-lg max-w-[80%] whitespace-pre-wrap break-words">
              {{ msg.content }}
            </div>
          </div>
          <!-- tool_call -->
          <div v-else-if="msg.role === 'tool_call'" class="text-[11px] font-mono text-fg-mute mb-1 pl-1">
            &#9881; {{ msg.toolName ?? 'tool' }}
          </div>
          <!-- tool_result: skip -->
          <!-- other roles: skip -->
        </template>
      </template>
    </div>

    <!-- Footer: resume prompt -->
    <footer class="shrink-0 border-t border-line px-4 py-3">
      <div class="flex gap-1.5">
        <input
          v-model="resumePrompt"
          class="flex-1 bg-raised border border-line rounded text-fg text-xs px-2 py-1.5 focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-accent focus-visible:border-accent placeholder:text-fg-faint"
          type="text"
          aria-label="Follow-up prompt"
          placeholder="Follow-up prompt to resume session..."
          @keydown.enter="handleResumeKeydown"
        >
        <button
          type="button"
          class="flex-shrink-0 bg-raised text-fg-mute border border-line rounded px-3 py-1.5 min-h-[44px] min-w-[44px] text-xs font-semibold cursor-pointer hover:text-green-600 dark:hover:text-green-400 hover:border-green-500 dark:hover:border-green-500 disabled:opacity-40 disabled:cursor-not-allowed"
          :disabled="!resumePrompt.trim() || spawning"
          @click="resumeSession"
        >
          {{ spawning ? '...' : 'Resume' }}
        </button>
      </div>
      <p
        role="status"
        aria-live="polite"
        class="text-[11px] mt-1.5"
        :class="statusMsg ? (statusIsError ? 'text-danger-text' : 'text-green-600 dark:text-green-400') : 'sr-only'"
      >
        {{ statusMsg }}
      </p>
    </footer>
  </AppModal>
</template>
