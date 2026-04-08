<template>
  <div class="channel-panel">
    <h4 class="channel-header">
      Follow-up
    </h4>

    <div class="history-area" v-if="history.length > 0">
      <div v-for="(entry, i) in history" :key="i" class="history-line">
        <span class="history-time">{{ entry.time }}</span>
        <span class="history-status" :class="entry.status">{{ entry.status }}</span>
        <span class="history-msg">{{ entry.message }}</span>
      </div>
    </div>

    <div class="send-row">
      <textarea
        v-model="input"
        class="send-input"
        rows="2"
        placeholder="Send follow-up instruction to this session..."
        @keydown.enter.exact.prevent="handleSend"
      ></textarea>
      <button
        class="send-btn"
        :disabled="isSending || input.trim().length === 0"
        @click="handleSend"
      >
        {{ isSending ? '...' : 'Send' }}
      </button>
    </div>

    <p class="channel-hint">
      Resumes this session with your message as a new prompt.
    </p>
    <p v-if="sendError" class="send-error">{{ sendError }}</p>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import type { Agent } from '../types'

const props = defineProps<{
  agent: Agent
}>()

const input = ref('')
const isSending = ref(false)
const sendError = ref<string | null>(null)

interface HistoryEntry {
  message: string
  time: string
  status: 'sent' | 'error'
}
const history = ref<HistoryEntry[]>([])

function timeNow(): string {
  return new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

async function handleSend() {
  const msg = input.value.trim()
  if (!msg || isSending.value) return

  isSending.value = true
  sendError.value = null

  try {
    const res = await fetch('/api/agents/spawn', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        prompt: msg,
        cwd: props.agent.cwd,
        resumeSessionId: props.agent.sessionId,
        enableChannel: true,
      }),
    })
    const data = await res.json()
    if (!res.ok) throw new Error(data.error || 'Spawn failed')

    history.value.push({ message: msg, time: timeNow(), status: 'sent' })
    input.value = ''
  } catch (err: unknown) {
    const errMsg = err instanceof Error ? err.message : 'Failed'
    sendError.value = errMsg
    history.value.push({ message: msg, time: timeNow(), status: 'error' })
    setTimeout(() => { sendError.value = null }, 5000)
  } finally {
    isSending.value = false
  }
}
</script>

<style scoped>
.channel-panel {
  /* inherits section spacing from parent */
}

.channel-header {
  font-size: 11px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: var(--text-muted);
  margin-bottom: 6px;
}

.history-area {
  max-height: 160px;
  overflow-y: auto;
  background: var(--bg-primary);
  border-radius: 4px;
  padding: 6px 10px;
  margin-bottom: 8px;
}

.history-line {
  display: flex;
  gap: 8px;
  padding: 3px 0;
  font-size: 12px;
  line-height: 1.4;
  border-bottom: 1px solid var(--border);
}

.history-line:last-child {
  border-bottom: none;
}

.history-time {
  color: var(--text-muted);
  font-family: monospace;
  font-size: 10px;
  flex-shrink: 0;
  padding-top: 1px;
}

.history-status {
  font-size: 10px;
  font-weight: 600;
  flex-shrink: 0;
  padding-top: 1px;
}

.history-status.sent { color: var(--accent-green); }
.history-status.error { color: #f87171; }

.history-msg {
  color: var(--text-secondary);
  word-break: break-word;
}

.send-row {
  display: flex;
  gap: 8px;
  align-items: flex-end;
}

.send-input {
  flex: 1;
  background: var(--bg-primary);
  border: 1px solid var(--border);
  border-radius: 4px;
  color: var(--text-primary);
  font-size: 12px;
  font-family: inherit;
  padding: 6px 10px;
  resize: none;
  line-height: 1.4;
}

.send-input::placeholder {
  color: var(--text-muted);
}

.send-input:focus {
  outline: none;
  border-color: var(--accent-green);
}

.send-btn {
  background: var(--accent-green);
  color: var(--bg-primary);
  border: none;
  border-radius: 4px;
  padding: 6px 14px;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  white-space: nowrap;
  height: fit-content;
}

.send-btn:disabled {
  opacity: 0.4;
  cursor: not-allowed;
}

.send-btn:not(:disabled):hover {
  filter: brightness(1.1);
}

.channel-hint {
  font-size: 11px;
  color: var(--text-muted);
  margin-top: 6px;
  line-height: 1.4;
}

.send-error {
  font-size: 11px;
  color: #f87171;
  margin-top: 4px;
}
</style>
