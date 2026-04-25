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
      const cmd = slashSuggestions.value[selectedIndex.value]
      if (cmd)
        selectSuggestion(cmd)
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
  <div class="relative" :class="variant">
    <div v-if="showSuggestions" class="absolute bottom-full left-0 right-0 bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 border-b-0 rounded-t-md max-h-60 overflow-y-auto z-10">
      <button
        v-for="(cmd, i) in slashSuggestions"
        :key="cmd.name"
        type="button"
        class="flex items-center gap-2.5 w-full px-4 py-2 bg-transparent border-none text-slate-500 dark:text-slate-400 text-[13px] font-mono cursor-pointer text-left hover:bg-slate-100 dark:hover:bg-slate-800"
        :class="{ 'bg-slate-100 dark:bg-slate-800': i === selectedIndex }"
        @mousedown.prevent="selectSuggestion(cmd)"
      >
        <span class="text-blue-600 dark:text-blue-400 font-semibold flex-shrink-0">{{ cmd.name }}</span>
        <span class="text-slate-400 dark:text-slate-600 text-xs">{{ cmd.description }}</span>
      </button>
    </div>
    <div
      class="border-t border-slate-200 dark:border-slate-700 flex items-end"
      :class="variant === 'full' ? 'px-4 py-2.5 gap-2 flex-shrink-0' : 'px-3 py-2 gap-1.5 items-center'"
    >
      <span
        class="text-blue-600 dark:text-blue-400 flex-shrink-0 pb-0.5"
        :class="variant === 'full' ? 'text-[14px]' : 'text-[13px] pb-0'"
      >❯</span>
      <textarea
        v-if="variant === 'full'"
        ref="inputEl"
        v-model="promptInput"
        rows="1"
        placeholder="Enter prompt..."
        :disabled="isSending"
        class="flex-1 bg-transparent border-none text-slate-900 dark:text-slate-100 text-[13px] font-mono outline-none placeholder:text-slate-400 dark:placeholder:text-slate-600 disabled:opacity-50 resize-none leading-snug min-h-[22px] max-h-36 overflow-y-auto"
        @keydown="onKeydown"
        @input="autoResize"
      />
      <input
        v-else
        ref="inputEl"
        v-model="promptInput"
        placeholder="Enter prompt..."
        :disabled="isSending"
        class="flex-1 bg-transparent border-none text-slate-900 dark:text-slate-100 text-[13px] font-mono outline-none placeholder:text-slate-400 dark:placeholder:text-slate-600 disabled:opacity-50"
        @keydown="onKeydown"
      >
      <button
        type="button"
        class="bg-blue-600 text-white border-none rounded font-bold cursor-pointer flex-shrink-0 hover:brightness-110 disabled:opacity-40 disabled:cursor-not-allowed"
        :class="variant === 'full' ? 'px-3.5 py-1.5 text-[14px]' : 'px-2.5 py-1 text-[13px]'"
        :disabled="isSending || promptInput.trim().length === 0"
        @click="handleSend"
      >
        {{ isSending ? '...' : '↵' }}
      </button>
    </div>
    <p
      v-if="sendStatus"
      class="text-[11px]"
      :class="[
        variant === 'full' ? 'px-4 pb-2' : 'px-3 pb-1.5 pt-0.5',
        sendStatus === 'sent' ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400',
      ]"
    >
      {{ sendStatus === 'sent' ? 'Sent' : sendError }}
    </p>
  </div>
</template>
