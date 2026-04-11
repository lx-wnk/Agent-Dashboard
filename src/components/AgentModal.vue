<script setup lang="ts">
import type { Agent, OutputMessage } from '../types'
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import { useAgentPrompt } from '../composables/useAgentPrompt'
import { formatCost, formatTokens, formatUptime, shortModel, totalTokenCount } from '../utils/format'
import StatusBadge from './StatusBadge.vue'
import SubAgentList from './SubAgentList.vue'
import TaskList from './TaskList.vue'
import ToolTimeline from './ToolTimeline.vue'

const props = defineProps<{ agent: Agent | null }>()
const emit = defineEmits<{ close: [] }>()

const outputMessages = ref<OutputMessage[]>([])
const isLoadingOutput = ref(false)
const outputEl = ref<HTMLElement | null>(null)
const promptEl = ref<HTMLTextAreaElement | null>(null)

const { promptInput, isSending, sendStatus, sendError, handleSend: sendPrompt } = useAgentPrompt(() => props.agent)

const totalTokens = computed(() => props.agent ? totalTokenCount(props.agent.tokenUsage) : 0)

let refreshInterval: ReturnType<typeof setInterval> | null = null

function scrollToBottom() {
  requestAnimationFrame(() => {
    if (outputEl.value) {
      outputEl.value.scrollTop = outputEl.value.scrollHeight
    }
  })
}

async function fetchOutput(sessionId: string) {
  isLoadingOutput.value = true
  try {
    const res = await fetch(`/api/agents/${sessionId}/output`)
    if (!res.ok)
      throw new Error('Failed to fetch')
    const data = await res.json()
    outputMessages.value = data.messages
    await nextTick()
    scrollToBottom()
  }
  catch {
    outputMessages.value = []
  }
  finally {
    isLoadingOutput.value = false
  }
}

async function refreshOutput() {
  const agent = props.agent
  if (!agent || agent.status === 'idle')
    return
  try {
    const res = await fetch(`/api/agents/${agent.sessionId}/output`)
    if (!res.ok)
      return
    const data = await res.json()
    const newMessages: OutputMessage[] = data.messages
    if (newMessages.length > outputMessages.value.length) {
      outputMessages.value = newMessages
      await nextTick()
      scrollToBottom()
    }
  }
  catch { /* ignore refresh errors */ }
}

function startRefresh() {
  stopRefresh()
  if (props.agent && props.agent.status !== 'idle') {
    refreshInterval = setInterval(refreshOutput, 5000)
  }
}

function stopRefresh() {
  if (refreshInterval) {
    clearInterval(refreshInterval)
    refreshInterval = null
  }
}

// Fetch output when agent changes
watch(() => props.agent?.sessionId, (sessionId) => {
  if (sessionId && !props.agent?.machine) {
    fetchOutput(sessionId)
    startRefresh()
    nextTick(() => promptEl.value?.focus())
  }
  else {
    outputMessages.value = []
  }
})

// Restart refresh when agent status changes
watch(() => props.agent?.status, () => {
  if (props.agent)
    startRefresh()
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
  }
  else {
    window.removeEventListener('keydown', onKeydown)
  }
}, { immediate: true })

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown)
  stopRefresh()
})

async function handleSend() {
  await sendPrompt()
}
</script>

<template>
  <Transition name="modal">
    <div v-if="agent" class="modal-backdrop" @click.self="emit('close')">
      <div class="modal-window">
        <div class="modal-titlebar">
          <div class="modal-title-left">
            <StatusBadge :status="agent.status" />
            <span class="modal-project">{{ agent.projectName }}</span>
            <span v-if="agent.machine" class="modal-machine">{{ agent.machine }}</span>
            <span class="modal-meta">{{ shortModel(agent.model) }} · {{ formatCost(agent.costEstimate) }} · {{ formatTokens(totalTokens) }} tok · {{ formatUptime(agent.uptime) }}</span>
          </div>
          <div class="modal-title-right">
            <button class="modal-close" @click="emit('close')">
              ✕
            </button>
          </div>
        </div>

        <div ref="outputEl" class="modal-output">
          <div v-if="agent.machine" class="output-empty">
            Session output is not available for remote agents.
          </div>
          <div v-else-if="isLoadingOutput" class="output-loading">
            Loading session output...
          </div>
          <template v-else-if="outputMessages.length > 0">
            <div
              v-for="(msg, i) in outputMessages"
              :key="i"
              class="output-msg"
              :class="msg.role"
            >
              <template v-if="msg.role === 'assistant'">
                {{ msg.content }}
              </template>
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
          <div v-else class="output-empty">
            No output available for this session.
          </div>
        </div>

        <div v-if="agent.tasks.length > 0 || agent.subagents.length > 0 || agent.lastTools.length > 0" class="modal-details">
          <details>
            <summary class="details-summary">
              Agent Details (Tasks, Tools, Subagents)
            </summary>
            <div class="details-content">
              <ToolTimeline v-if="agent.lastTools.length > 0" :tools="agent.lastTools" />
              <TaskList v-if="agent.tasks.length > 0" :tasks="agent.tasks" />
              <SubAgentList v-if="agent.subagents.length > 0" :subagents="agent.subagents" />
            </div>
          </details>
        </div>

        <div v-if="!agent.machine" class="modal-prompt">
          <span class="prompt-cursor">❯</span>
          <textarea
            ref="promptEl"
            v-model="promptInput"
            class="prompt-textarea"
            rows="1"
            placeholder="Enter prompt..."
            :disabled="isSending"
            @keydown.ctrl.enter.prevent="handleSend"
            @keydown.meta.enter.prevent="handleSend"
          />
          <button
            class="prompt-send"
            :disabled="isSending || promptInput.trim().length === 0"
            @click="handleSend"
          >
            {{ isSending ? '...' : '↵' }}
          </button>
        </div>
        <p v-if="sendStatus && !agent.machine" class="modal-send-status" :class="sendStatus">
          {{ sendStatus === 'sent' ? 'Sent' : sendError }}
        </p>
      </div>
    </div>
  </Transition>
</template>

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
.modal-machine {
  font-size: 10px;
  font-weight: 600;
  color: var(--accent-blue);
  border: 1px solid var(--accent-blue);
  border-radius: 3px;
  padding: 0 5px;
}
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
  font-family: var(--font-mono);
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
.prompt-cursor { color: var(--accent-blue); font-size: 14px; flex-shrink: 0; }
.prompt-textarea {
  flex: 1;
  background: none;
  border: none;
  color: var(--text-primary);
  font-size: 13px;
  font-family: var(--font-mono);
  outline: none;
  resize: none;
  line-height: 1.4;
}
.prompt-textarea::placeholder { color: var(--text-muted); }
.prompt-textarea:disabled { opacity: 0.5; }
.prompt-send {
  background: var(--accent-blue);
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
.modal-send-status.error { color: var(--accent-red); }
.modal-details {
  border-top: 1px solid var(--border);
  flex-shrink: 0;
}
.details-summary {
  padding: 8px 16px;
  font-size: 12px;
  color: var(--text-muted);
  cursor: pointer;
  user-select: none;
}
.details-summary:hover { color: var(--text-secondary); }
.details-content {
  padding: 8px 16px 12px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  max-height: 200px;
  overflow-y: auto;
}
.modal-enter-active, .modal-leave-active { transition: opacity 0.2s; }
.modal-enter-active .modal-window, .modal-leave-active .modal-window { transition: transform 0.2s; }
.modal-enter-from, .modal-leave-to { opacity: 0; }
.modal-enter-from .modal-window { transform: scale(0.95); }
.modal-leave-to .modal-window { transform: scale(0.95); }
</style>
