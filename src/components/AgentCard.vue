<template>
  <div class="agent-card" @click="$emit('select', agent)">
    <div class="card-titlebar">
      <div class="card-title-left">
        <StatusBadge :status="agent.status" />
        <span class="card-project">{{ agent.projectName }}</span>
        <span class="card-meta">{{ shortModel(agent.model) }} · {{ formatCost(agent.costEstimate) }}</span>
      </div>
      <div class="card-title-right">
        <span class="card-meta">{{ formatTokens(totalTokens) }} tok · {{ formatUptime(agent.uptime) }}</span>
      </div>
    </div>
    <div class="card-output">
      <template v-if="agent.lastOutput">{{ agent.lastOutput }}</template>
      <span v-else class="card-no-output">No output yet</span>
    </div>
    <div class="card-prompt" @click.stop>
      <span class="prompt-cursor">❯</span>
      <input
        v-model="promptInput"
        class="prompt-input"
        placeholder="Enter prompt..."
        @keydown.enter.prevent="handleSend"
        :disabled="isSending"
      />
      <button
        class="prompt-send"
        :disabled="isSending || promptInput.trim().length === 0"
        @click="handleSend"
      >
        {{ isSending ? '...' : '↵' }}
      </button>
    </div>
    <p v-if="sendStatus" class="card-send-status" :class="sendStatus">
      {{ sendStatus === 'sent' ? 'Sent' : sendError }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { Agent } from '../types'
import { formatTokens, formatCost, formatUptime, shortModel } from '../utils/format'
import { useAgentPrompt } from '../composables/useAgentPrompt'
import StatusBadge from './StatusBadge.vue'

const props = defineProps<{ agent: Agent }>()
defineEmits<{ select: [agent: Agent] }>()

const { promptInput, isSending, sendStatus, sendError, handleSend } = useAgentPrompt(() => props.agent)

const totalTokens = computed(() => {
  const u = props.agent.tokenUsage
  return u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheCreationTokens
})
</script>

<style scoped>
.agent-card {
  background: var(--bg-primary);
  border-radius: 8px;
  overflow: hidden;
  cursor: pointer;
  border: 1px solid var(--border);
  transition: border-color 0.15s, box-shadow 0.15s;
}
.agent-card:hover {
  border-color: var(--bg-tertiary);
  box-shadow: 0 2px 12px rgba(0, 0, 0, 0.3);
}
.card-titlebar {
  background: var(--bg-secondary);
  padding: 8px 12px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 8px;
}
.card-title-left {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.card-title-right { flex-shrink: 0; }
.card-project {
  font-weight: 600;
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.card-meta {
  font-size: 11px;
  color: var(--text-muted);
  white-space: nowrap;
}
.card-output {
  padding: 12px;
  font-size: 13px;
  line-height: 1.5;
  color: var(--text-secondary);
  max-height: 120px;
  overflow: hidden;
  position: relative;
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
}
.card-output::after {
  content: '';
  position: absolute;
  bottom: 0;
  left: 0;
  right: 0;
  height: 32px;
  background: linear-gradient(transparent, var(--bg-primary));
  pointer-events: none;
}
.card-no-output {
  color: var(--text-muted);
  font-style: italic;
}
.card-prompt {
  border-top: 1px solid var(--border);
  padding: 8px 12px;
  display: flex;
  align-items: center;
  gap: 6px;
}
.prompt-cursor {
  color: #3b82f6;
  font-size: 13px;
  flex-shrink: 0;
}
.prompt-input {
  flex: 1;
  background: none;
  border: none;
  color: var(--text-primary);
  font-size: 13px;
  font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
  outline: none;
}
.prompt-input::placeholder { color: var(--text-muted); }
.prompt-input:disabled { opacity: 0.5; }
.prompt-send {
  background: #3b82f6;
  color: white;
  border: none;
  border-radius: 4px;
  padding: 4px 10px;
  font-size: 13px;
  font-weight: bold;
  cursor: pointer;
  flex-shrink: 0;
}
.prompt-send:disabled { opacity: 0.4; cursor: not-allowed; }
.prompt-send:not(:disabled):hover { filter: brightness(1.15); }
.card-send-status {
  font-size: 11px;
  padding: 2px 12px 6px;
}
.card-send-status.sent { color: var(--accent-green); }
.card-send-status.error { color: #f87171; }
</style>
