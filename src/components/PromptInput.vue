<script setup lang="ts">
import type { Agent, OutputMessage } from '../types'
import type { SlashCommandDef } from '../composables/useSlashCommands'
import { computed, nextTick, ref, useId, watch } from 'vue'
import { fetchDynamicCommands, SLASH_COMMAND_DEFS } from '../composables/useSlashCommands'
import { useAgentPrompt } from '../composables/useAgentPrompt'

const props = withDefaults(defineProps<{
  agent: Agent | null
  variant?: 'compact' | 'full'
}>(), {
  variant: 'compact',
})

const emit = defineEmits<{ messageSent: [msg: OutputMessage] }>()

// UX-10: unique ID per component instance so multiple PromptInput cards don't share the same DOM id
const hintId = useId()

const { promptInput, isSending, sendStatus, sendError, handleSend } = useAgentPrompt(
  () => props.agent,
  msg => emit('messageSent', msg),
  {
    get taskId() { return props.agent?.pipelineTaskId },
    get cwd() { return props.agent?.cwd },
  },
)

const inputEl = ref<HTMLInputElement | HTMLTextAreaElement | null>(null)
const selectedIndex = ref(0)
const dynamicCommands = ref<SlashCommandDef[]>([])

watch(() => props.agent?.cwd, async (cwd) => {
  if (cwd)
    dynamicCommands.value = await fetchDynamicCommands(cwd)
}, { immediate: true })

const slashSuggestions = computed(() => {
  const val = promptInput.value.trim()
  if (!val.startsWith('/'))
    return []
  if (val.includes(' '))
    return []
  const query = val.toLowerCase()

  const dashboardCmds = SLASH_COMMAND_DEFS
    .filter(c => c.name.startsWith(query))
    .map(c => ({
      ...c,
      disabled: !!c.requiresTask && !props.agent?.pipelineTaskId,
    }))

  const seen = new Set(dashboardCmds.map(c => c.name))
  const sessionCmds = dynamicCommands.value
    .filter(c => c.name.startsWith(query) && !seen.has(c.name))
    .map(c => ({ ...c, disabled: false }))

  return [...dashboardCmds, ...sessionCmds]
})

const showSuggestions = computed(() => slashSuggestions.value.length > 0)

function selectSuggestion(cmd: { name: string, disabled?: boolean }) {
  if (cmd.disabled)
    return
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
    <div
      v-if="showSuggestions"
      id="slash-listbox"
      role="listbox"
      class="absolute bottom-full left-0 right-0 bg-slate-50 dark:bg-slate-950 border border-slate-200 dark:border-slate-700 border-b-0 rounded-t-md max-h-60 overflow-y-auto z-10"
    >
      <button
        v-for="(cmd, i) in slashSuggestions"
        :key="cmd.name"
        type="button"
        role="option"
        :aria-selected="i === selectedIndex"
        :disabled="cmd.disabled"
        class="flex items-center gap-2.5 w-full px-4 py-2 bg-transparent border-none text-slate-500 dark:text-slate-400 text-[13px] font-mono cursor-pointer text-left hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-40 disabled:cursor-not-allowed"
        :class="{ 'bg-slate-100 dark:bg-slate-800': i === selectedIndex }"
        @mousedown.prevent="selectSuggestion(cmd)"
      >
        <span class="text-blue-600 dark:text-blue-400 font-semibold flex-shrink-0">{{ cmd.name }}</span>
        <span class="text-slate-400 dark:text-slate-600 text-xs">{{ cmd.description }}</span>
        <span v-if="cmd.usage" class="text-slate-400 dark:text-slate-500 text-[10px] ml-1">{{ cmd.usage }}</span>
        <span v-if="cmd.requiresTask && cmd.disabled" class="text-amber-600 text-[10px] ml-auto">requires linked task</span>
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
      <!-- UX-10: sr-only hint for keyboard shortcut; referenced via aria-describedby -->
      <span :id="hintId" class="sr-only">Press Enter to send, Shift+Enter for new line</span>
      <textarea
        v-if="variant === 'full'"
        ref="inputEl"
        v-model="promptInput"
        rows="1"
        placeholder="Enter prompt..."
        :disabled="isSending"
        :aria-describedby="hintId"
        :aria-controls="showSuggestions ? 'slash-listbox' : undefined"
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
        :aria-describedby="hintId"
        :aria-controls="showSuggestions ? 'slash-listbox' : undefined"
        class="flex-1 bg-transparent border-none text-slate-900 dark:text-slate-100 text-[13px] font-mono outline-none placeholder:text-slate-400 dark:placeholder:text-slate-600 disabled:opacity-50"
        @keydown="onKeydown"
      >
      <button
        type="button"
        aria-label="Send message"
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
