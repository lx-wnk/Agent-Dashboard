<script setup lang="ts">
import type { Agent } from '../types'
import { ref } from 'vue'
import { useAgentPrompt } from '../composables/useAgentPrompt'

const props = withDefaults(defineProps<{
  agent: Agent | null
  variant?: 'compact' | 'full'
}>(), {
  variant: 'compact',
})

const { promptInput, isSending, sendStatus, sendError, handleSend } = useAgentPrompt(() => props.agent)

const inputEl = ref<HTMLInputElement | HTMLTextAreaElement | null>(null)

function focus() {
  inputEl.value?.focus()
}

defineExpose({ focus })
</script>

<template>
  <div class="prompt-wrapper" :class="variant">
    <div class="prompt-bar">
      <span class="prompt-cursor">❯</span>
      <textarea
        v-if="variant === 'full'"
        ref="inputEl"
        v-model="promptInput"
        class="prompt-field prompt-textarea"
        rows="1"
        placeholder="Enter prompt..."
        :disabled="isSending"
        @keydown.ctrl.enter.prevent="handleSend"
        @keydown.meta.enter.prevent="handleSend"
      />
      <input
        v-else
        ref="inputEl"
        v-model="promptInput"
        class="prompt-field prompt-input"
        placeholder="Enter prompt..."
        :disabled="isSending"
        @keydown.enter.prevent="handleSend"
      >
      <button
        class="prompt-send"
        :disabled="isSending || promptInput.trim().length === 0"
        @click="handleSend"
      >
        {{ isSending ? '...' : '↵' }}
      </button>
    </div>
    <p v-if="sendStatus" class="prompt-status" :class="sendStatus">
      {{ sendStatus === 'sent' ? 'Sent' : sendError }}
    </p>
  </div>
</template>

<style scoped>
.prompt-bar {
  border-top: 1px solid var(--border);
  display: flex;
  align-items: center;
}
.prompt-wrapper.compact .prompt-bar {
  padding: 8px 12px;
  gap: 6px;
}
.prompt-wrapper.full .prompt-bar {
  padding: 10px 16px;
  gap: 8px;
  flex-shrink: 0;
}
.prompt-cursor {
  color: var(--accent-blue);
  flex-shrink: 0;
}
.prompt-wrapper.compact .prompt-cursor { font-size: 13px; }
.prompt-wrapper.full .prompt-cursor { font-size: 14px; }
.prompt-field {
  flex: 1;
  background: none;
  border: none;
  color: var(--text-primary);
  font-size: 13px;
  font-family: var(--font-mono);
  outline: none;
}
.prompt-field::placeholder { color: var(--text-muted); }
.prompt-field:disabled { opacity: 0.5; }
.prompt-textarea {
  resize: none;
  line-height: 1.4;
}
.prompt-send {
  background: var(--accent-blue);
  color: white;
  border: none;
  border-radius: 4px;
  font-weight: bold;
  cursor: pointer;
  flex-shrink: 0;
}
.prompt-wrapper.compact .prompt-send {
  padding: 4px 10px;
  font-size: 13px;
}
.prompt-wrapper.full .prompt-send {
  padding: 6px 14px;
  font-size: 14px;
}
.prompt-send:disabled { opacity: 0.4; cursor: not-allowed; }
.prompt-send:not(:disabled):hover { filter: brightness(1.15); }
.prompt-status { font-size: 11px; }
.prompt-wrapper.compact .prompt-status { padding: 2px 12px 6px; }
.prompt-wrapper.full .prompt-status { padding: 0 16px 8px; }
.prompt-status.sent { color: var(--accent-green); }
.prompt-status.error { color: var(--accent-red); }
</style>
