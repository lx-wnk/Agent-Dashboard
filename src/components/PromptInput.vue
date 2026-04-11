<script setup lang="ts">
import type { Agent, OutputMessage } from '../types'
import { computed, nextTick, ref, watch } from 'vue'
import { useAgentPrompt } from '../composables/useAgentPrompt'

const props = withDefaults(defineProps<{
  agent: Agent | null
  variant?: 'compact' | 'full'
}>(), {
  variant: 'compact',
})

const emit = defineEmits<{ messageSent: [msg: OutputMessage] }>()

const SLASH_COMMANDS = [
  { name: '/btw', description: 'Send interrupt message while agent works' },
  { name: '/compact', description: 'Compact conversation context' },
  { name: '/review', description: 'Review recent changes' },
  { name: '/commit', description: 'Commit staged changes' },
  { name: '/help', description: 'Show available commands' },
  { name: '/clear', description: 'Clear conversation display' },
  { name: '/cost', description: 'Show token usage and costs' },
  { name: '/status', description: 'Show agent status summary' },
]

const { promptInput, isSending, sendStatus, sendError, handleSend } = useAgentPrompt(
  () => props.agent,
  msg => emit('messageSent', msg),
)

const inputEl = ref<HTMLInputElement | HTMLTextAreaElement | null>(null)
const selectedIndex = ref(0)

const slashSuggestions = computed(() => {
  const val = promptInput.value.trim()
  if (!val.startsWith('/'))
    return []
  // Only suggest when the entire input is the slash query (no space yet = still selecting)
  if (val.includes(' '))
    return []
  const query = val.toLowerCase()
  return SLASH_COMMANDS.filter(c => c.name.startsWith(query))
})

const showSuggestions = computed(() => slashSuggestions.value.length > 0)

function selectSuggestion(cmd: typeof SLASH_COMMANDS[0]) {
  promptInput.value = `${cmd.name} `
  nextTick(() => inputEl.value?.focus())
}

function focus() {
  inputEl.value?.focus()
}

function autoResize() {
  const el = inputEl.value as HTMLTextAreaElement | null
  if (!el)
    return
  el.style.height = 'auto'
  el.style.height = `${Math.min(el.scrollHeight, 144)}px`
}

function onKeydown(e: KeyboardEvent) {
  if (showSuggestions.value) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      selectedIndex.value = Math.min(selectedIndex.value + 1, slashSuggestions.value.length - 1)
    }
    else if (e.key === 'ArrowUp') {
      e.preventDefault()
      selectedIndex.value = Math.max(selectedIndex.value - 1, 0)
    }
    else if (e.key === 'Tab' || (e.key === 'Enter' && !e.shiftKey)) {
      e.preventDefault()
      selectSuggestion(slashSuggestions.value[selectedIndex.value])
    }
    else if (e.key === 'Escape') {
      e.preventDefault()
      promptInput.value = ''
    }
  }
  else if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    handleSend()
  }
}

// Reset selected index when suggestions change
watch(slashSuggestions, () => {
  selectedIndex.value = 0
})

// Reset textarea height after send
watch(promptInput, (val) => {
  if (val === '') {
    nextTick(autoResize)
  }
})

defineExpose({ focus })
</script>

<template>
  <div class="prompt-wrapper" :class="variant">
    <div v-if="showSuggestions" class="slash-suggestions">
      <button
        v-for="(cmd, i) in slashSuggestions"
        :key="cmd.name"
        class="slash-item"
        :class="{ selected: i === selectedIndex }"
        @mousedown.prevent="selectSuggestion(cmd)"
      >
        <span class="slash-name">{{ cmd.name }}</span>
        <span class="slash-desc">{{ cmd.description }}</span>
      </button>
    </div>
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
        @keydown="onKeydown"
        @input="autoResize"
      />
      <input
        v-else
        ref="inputEl"
        v-model="promptInput"
        class="prompt-field prompt-input"
        placeholder="Enter prompt..."
        :disabled="isSending"
        @keydown="onKeydown"
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
.prompt-wrapper {
  position: relative;
}
.prompt-bar {
  border-top: 1px solid var(--border);
  display: flex;
  align-items: flex-end;
}
.prompt-wrapper.compact .prompt-bar {
  padding: 8px 12px;
  gap: 6px;
  align-items: center;
}
.prompt-wrapper.full .prompt-bar {
  padding: 10px 16px;
  gap: 8px;
  flex-shrink: 0;
}
.prompt-cursor {
  color: var(--accent-blue);
  flex-shrink: 0;
  padding-bottom: 2px;
}
.prompt-wrapper.compact .prompt-cursor { font-size: 13px; padding-bottom: 0; }
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
  overflow-y: auto;
  min-height: 22px;
  max-height: 144px;
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

/* Slash command suggestions */
.slash-suggestions {
  position: absolute;
  bottom: 100%;
  left: 0;
  right: 0;
  background: var(--bg-primary);
  border: 1px solid var(--border);
  border-bottom: none;
  border-radius: 6px 6px 0 0;
  max-height: 240px;
  overflow-y: auto;
  z-index: 10;
}
.slash-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 16px;
  background: none;
  border: none;
  color: var(--text-secondary);
  font-size: 13px;
  font-family: var(--font-mono);
  cursor: pointer;
  text-align: left;
}
.slash-item:hover,
.slash-item.selected {
  background: var(--bg-tertiary);
}
.slash-name {
  color: var(--accent-blue);
  font-weight: 600;
  flex-shrink: 0;
}
.slash-desc {
  color: var(--text-muted);
  font-size: 12px;
}
</style>
