<template>
  <Transition name="modal">
    <div v-if="agent" class="modal-backdrop" @click.self="emit('close')">
      <div class="modal-window">
        <div class="modal-titlebar">
          <div class="modal-title-left">
            <StatusBadge :status="agent.status" />
            <span class="modal-project">{{ agent.projectName }}</span>
            <span class="modal-meta">{{ shortModel(agent.model) }} · {{ formatCost(agent.costEstimate) }} · {{ formatTokens(totalTokens) }} tok · {{ formatUptime(agent.uptime) }}</span>
          </div>
          <div class="modal-title-right">
            <button class="modal-close" @click="emit('close')">✕</button>
          </div>
        </div>

        <div class="modal-output" ref="outputEl">
          <div v-if="isLoadingOutput" class="output-loading">Loading session output...</div>
          <template v-else-if="outputMessages.length > 0">
            <div
              v-for="(msg, i) in outputMessages"
              :key="i"
              class="output-msg"
              :class="msg.role"
            >
              <template v-if="msg.role === 'assistant'">{{ msg.content }}</template>
              <template v-else-if="msg.role === 'tool_call'">
                <span class="tool-divider">── Tool: {{ msg.toolName }}<template v-if="msg.filePath"> {{ msg.filePath }}</template> ──</span>
              </template>
              <template v-else-if="msg.role === 'tool_result'">
                <details class="tool-result-details">
                  <summary>Result (click to expand)</summary>
                  <pre class="tool-result-content">{{ msg.content }}</pre>
                </details>
              </template>
            </div>
          </template>
          <div v-else class="output-empty">No output available for this session.</div>
        </div>

        <div class="modal-prompt">
          <span class="prompt-cursor">❯</span>
          <textarea
            v-model="promptInput"
            class="prompt-textarea"
            rows="1"
            placeholder="Enter prompt..."
            @keydown.ctrl.enter.prevent="handleSend"
            @keydown.meta.enter.prevent="handleSend"
            :disabled="isSending"
            ref="promptEl"
          ></textarea>
          <button
            class="prompt-send"
            :disabled="isSending || promptInput.trim().length === 0"
            @click="handleSend"
          >
            {{ isSending ? '...' : '↵' }}
          </button>
        </div>
        <p v-if="sendStatus" class="modal-send-status" :class="sendStatus">
          {{ sendStatus === 'sent' ? 'Sent' : sendError }}
        </p>
      </div>
    </div>
  </Transition>
</template>

<script setup lang="ts">
import { ref, computed, watch, nextTick, onUnmounted } from 'vue'
import type { Agent, OutputMessage } from '../types'
import { formatTokens, formatCost, formatUptime } from '../utils/format'
import StatusBadge from './StatusBadge.vue'

const props = defineProps<{ agent: Agent | null }>()
const emit = defineEmits<{ close: [] }>()

const outputMessages = ref<OutputMessage[]>([])
const isLoadingOutput = ref(false)
const outputEl = ref<HTMLElement | null>(null)
const promptEl = ref<HTMLTextAreaElement | null>(null)
const promptInput = ref('')
const isSending = ref(false)
const sendStatus = ref<'sent' | 'error' | null>(null)
const sendError = ref('')

const totalTokens = computed(() => {
  if (!props.agent) return 0
  const u = props.agent.tokenUsage
  return u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheCreationTokens
})

function shortModel(model: string | null): string {
  if (!model) return '—'
  return model.replace('claude-', '').replace(/-\d+$/, m => ' ' + m.slice(1))
}

async function fetchOutput(sessionId: string) {
  isLoadingOutput.value = true
  try {
    const res = await fetch(`/api/agents/${sessionId}/output`)
    if (!res.ok) throw new Error('Failed to fetch')
    const data = await res.json()
    outputMessages.value = data.messages
    await nextTick()
    if (outputEl.value) {
      outputEl.value.scrollTop = outputEl.value.scrollHeight
    }
  } catch {
    outputMessages.value = []
  } finally {
    isLoadingOutput.value = false
  }
}

// Fetch output when agent changes
watch(() => props.agent?.sessionId, (sessionId) => {
  if (sessionId) {
    fetchOutput(sessionId)
    nextTick(() => promptEl.value?.focus())
  } else {
    outputMessages.value = []
  }
})

// Close on Escape
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && props.agent) {
    e.preventDefault()
    emit('close')
  }
}

watch(() => props.agent, (agent) => {
  if (agent) {
    window.addEventListener('keydown', onKeydown)
  } else {
    window.removeEventListener('keydown', onKeydown)
  }
}, { immediate: true })

onUnmounted(() => window.removeEventListener('keydown', onKeydown))

async function handleSend() {
  const msg = promptInput.value.trim()
  if (!msg || isSending.value || !props.agent) return

  isSending.value = true
  sendStatus.value = null

  try {
    if (props.agent.channelAvailable && props.agent.status !== 'idle') {
      const res = await fetch(`/api/agents/${props.agent.sessionId}/message`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ message: msg }),
      })
      if (!res.ok) throw new Error((await res.json()).error || 'Send failed')
    } else {
      const res = await fetch('/api/agents/spawn', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          prompt: msg,
          cwd: props.agent.cwd,
          resumeSessionId: props.agent.sessionId,
        }),
      })
      if (!res.ok) throw new Error((await res.json()).error || 'Resume failed')
    }
    sendStatus.value = 'sent'
    promptInput.value = ''
    if (props.agent) {
      setTimeout(() => fetchOutput(props.agent!.sessionId), 2000)
    }
  } catch (err) {
    sendStatus.value = 'error'
    sendError.value = err instanceof Error ? err.message : 'Failed'
  } finally {
    isSending.value = false
    setTimeout(() => { sendStatus.value = null }, 3000)
  }
}
</script>

<style scoped>
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 1000;
  padding: 24px;
}
.modal-window {
  background: var(--bg-secondary);
  border-radius: 10px;
  border: 1px solid var(--bg-tertiary);
  box-shadow: 0 8px 40px rgba(0, 0, 0, 0.5);
  width: 100%;
  max-width: 900px;
  max-height: 80vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
.modal-titlebar {
  background: var(--bg-tertiary);
  padding: 10px 16px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-shrink: 0;
}
.modal-title-left {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.modal-project { font-weight: 600; font-size: 14px; }
.modal-meta { font-size: 11px; color: var(--text-muted); white-space: nowrap; }
.modal-close {
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 16px;
  cursor: pointer;
  padding: 4px 8px;
  border-radius: 4px;
}
.modal-close:hover { background: var(--bg-secondary); color: var(--text-primary); }
.modal-output {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
  font-size: 13px;
  line-height: 1.6;
}
.output-loading, .output-empty {
  color: var(--text-muted);
  text-align: center;
  padding: 48px;
}
.output-msg { margin-bottom: 12px; }
.output-msg.assistant {
  color: var(--text-secondary);
  white-space: pre-wrap;
  word-break: break-word;
}
.output-msg.tool_call { margin: 8px 0; }
.tool-divider { color: var(--text-muted); font-size: 11px; }
.output-msg.tool_result { margin: 4px 0 12px; }
.tool-result-details summary {
  color: var(--text-muted);
  font-size: 11px;
  cursor: pointer;
}
.tool-result-details summary:hover { color: var(--text-secondary); }
.tool-result-content {
  background: var(--bg-primary);
  border-radius: 4px;
  padding: 8px;
  font-size: 11px;
  color: var(--text-secondary);
  max-height: 200px;
  overflow-y: auto;
  margin-top: 4px;
  white-space: pre-wrap;
  word-break: break-word;
}
.modal-prompt {
  border-top: 1px solid var(--border);
  padding: 10px 16px;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.prompt-cursor { color: #3b82f6; font-size: 14px; flex-shrink: 0; }
.prompt-textarea {
  flex: 1;
  background: none;
  border: none;
  color: var(--text-primary);
  font-size: 13px;
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
  outline: none;
  resize: none;
  line-height: 1.4;
}
.prompt-textarea::placeholder { color: var(--text-muted); }
.prompt-textarea:disabled { opacity: 0.5; }
.prompt-send {
  background: #3b82f6;
  color: white;
  border: none;
  border-radius: 4px;
  padding: 6px 14px;
  font-size: 14px;
  font-weight: bold;
  cursor: pointer;
  flex-shrink: 0;
}
.prompt-send:disabled { opacity: 0.4; cursor: not-allowed; }
.prompt-send:not(:disabled):hover { filter: brightness(1.15); }
.modal-send-status { font-size: 11px; padding: 0 16px 8px; }
.modal-send-status.sent { color: var(--accent-green); }
.modal-send-status.error { color: #f87171; }
.modal-enter-active, .modal-leave-active { transition: opacity 0.2s; }
.modal-enter-active .modal-window, .modal-leave-active .modal-window { transition: transform 0.2s; }
.modal-enter-from, .modal-leave-to { opacity: 0; }
.modal-enter-from .modal-window { transform: scale(0.95); }
.modal-leave-to .modal-window { transform: scale(0.95); }
</style>
