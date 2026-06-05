<script setup lang="ts">
import type { Agent, OutputMessage } from '../types'
import type { SlashCommandDef } from '../composables/useSlashCommands'
import { computed, nextTick, ref, useId, watch } from 'vue'
import { useAgentPrompt } from '../composables/useAgentPrompt'
import { SLASH_COMMAND_DEFS, fetchDynamicCommands } from '../composables/useSlashCommands'

const props = withDefaults(defineProps<{
  agent: Agent | null
  variant?: 'compact' | 'full'
}>(), {
  variant: 'compact',
})

const emit = defineEmits<{ messageSent: [msg: OutputMessage] }>()

// UX-10: unique ID per component instance so multiple PromptInput cards don't share the same DOM id
const hintId = useId()
const listboxId = useId()

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

// Prefer sessionId so suggestions reflect the running session's actual
// CLAUDE_CONFIG_DIR (spawner-dependent); fall back to cwd for project-local commands.
watch(() => [props.agent?.sessionId, props.agent?.cwd] as const, async ([sessionId, cwd]) => {
  if (sessionId || cwd)
    dynamicCommands.value = await fetchDynamicCommands({ sessionId: sessionId || undefined, cwd: cwd || undefined })
}, { immediate: true })

const slashSuggestions = computed(() => {
  const val = promptInput.value.trim()
  if (!val.startsWith('/'))
    return []
  if (val.includes(' '))
    return []
  const query = val.toLowerCase()
  const term = query.slice(1) // drop leading '/'
  // Match like Claude's slash menu: prefix OR substring on the command name
  // (so "/review" surfaces /branch-review, /security-review, …). Prefix hits
  // rank first.
  const matches = (name: string) => {
    const n = name.toLowerCase()
    return n.startsWith(query) || n.slice(1).includes(term)
  }
  const rank = (name: string) => (name.toLowerCase().startsWith(query) ? 0 : 1)

  const dashboardCmds = SLASH_COMMAND_DEFS
    .filter(c => matches(c.name))
    .map(c => ({
      ...c,
      disabled: !!c.requiresTask && !props.agent?.pipelineTaskId,
    }))

  const seen = new Set(dashboardCmds.map(c => c.name))
  const sessionCmds = dynamicCommands.value
    .filter(c => matches(c.name) && !seen.has(c.name))
    .map(c => ({ ...c, disabled: false }))

  return [...dashboardCmds, ...sessionCmds].sort((a, b) => rank(a.name) - rank(b.name))
})

const showSuggestions = computed(() => slashSuggestions.value.length > 0)

// Only a live-injectable session (pty broker or tmux) can receive a live prompt.
// Any other session resumes as a NEW session (claude --resume) on send — surface
// that so it's not mistaken for live injection.
const isResumeMode = computed(() => !!props.agent && !props.agent.liveInjectable)

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
    else if (e.key === 'Tab') {
      // Tab always completes the highlighted suggestion.
      e.preventDefault()
      const cmd = slashSuggestions.value[selectedIndex.value]
      if (cmd)
        selectSuggestion(cmd)
    }
    else if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      // If the input is already a complete command, send it; otherwise complete
      // the highlighted suggestion (so a fully-typed /command isn't stuck on the menu).
      const typed = promptInput.value.trim()
      const exact = slashSuggestions.value.find(c => c.name === typed)
      if (exact) {
        if (!exact.disabled)
          handleSend()
      }
      else {
        const cmd = slashSuggestions.value[selectedIndex.value]
        if (cmd)
          selectSuggestion(cmd)
      }
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
    <span v-if="variant === 'full'" :id="hintId" class="sr-only">Press Enter to send, Shift+Enter for new line</span>
    <span v-else :id="hintId" class="sr-only">Press Enter to send</span>
    <div
      v-if="showSuggestions"
      :id="listboxId"
      role="listbox"
      class="absolute bottom-full left-0 right-0 bg-app border border-line border-b-0 rounded-t-md max-h-60 overflow-y-auto z-10"
    >
      <button
        v-for="(cmd, i) in slashSuggestions"
        :key="cmd.name"
        type="button"
        role="option"
        :aria-selected="i === selectedIndex"
        :disabled="cmd.disabled"
        class="flex items-center gap-2.5 w-full px-4 py-2 bg-transparent border-none text-fg-mute text-[13px] font-mono cursor-pointer text-left hover:bg-raised disabled:opacity-40 disabled:cursor-not-allowed"
        :class="{ 'bg-raised': i === selectedIndex }"
        @mousedown.prevent="selectSuggestion(cmd)"
      >
        <span class="text-blue-600 dark:text-blue-400 font-semibold flex-shrink-0">{{ cmd.name }}</span>
        <span class="text-fg-mute text-xs">{{ cmd.description }}</span>
        <span v-if="cmd.usage" class="text-fg-faint text-[10px] ml-1">{{ cmd.usage }}</span>
        <span v-if="cmd.requiresTask && cmd.disabled" class="text-amber-600 text-[10px] ml-auto">requires linked task</span>
      </button>
    </div>
    <div
      class="border-t border-line flex items-end"
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
        :placeholder="isResumeMode ? 'Prompt… (resumes as a new session)' : 'Enter prompt...'"
        :disabled="isSending"
        role="combobox"
        :aria-expanded="showSuggestions"
        :aria-describedby="hintId"
        :aria-controls="showSuggestions ? listboxId : undefined"
        class="flex-1 bg-transparent border-none text-fg text-[13px] font-mono outline-none placeholder:text-fg-faint disabled:opacity-50 resize-none leading-snug min-h-[22px] max-h-36 overflow-y-auto"
        @keydown="onKeydown"
        @input="autoResize"
      />
      <input
        v-else
        ref="inputEl"
        v-model="promptInput"
        :placeholder="isResumeMode ? 'Prompt… (resumes as a new session)' : 'Enter prompt...'"
        :disabled="isSending"
        role="combobox"
        :aria-expanded="showSuggestions"
        :aria-describedby="hintId"
        :aria-controls="showSuggestions ? listboxId : undefined"
        class="flex-1 bg-transparent border-none text-fg text-[13px] font-mono outline-none placeholder:text-fg-faint disabled:opacity-50"
        @keydown="onKeydown"
      >
      <button
        type="button"
        :aria-label="isResumeMode ? 'Resume session with prompt (creates a new session)' : 'Send message'"
        :title="isResumeMode ? 'No live channel — resumes this session as a new session' : 'Send'"
        class="text-white border-none rounded font-bold cursor-pointer flex-shrink-0 hover:brightness-110 disabled:opacity-40 disabled:cursor-not-allowed"
        :class="[
          variant === 'full' ? 'px-3.5 py-1.5 text-[14px]' : 'px-2.5 py-1 text-[13px]',
          isResumeMode ? 'bg-amber-600' : 'bg-blue-600',
        ]"
        :disabled="isSending || promptInput.trim().length === 0"
        @click="handleSend"
      >
        {{ isSending ? '...' : (isResumeMode ? '⤳' : '↵') }}
      </button>
    </div>
    <p
      v-if="isResumeMode && !sendStatus"
      class="text-[11px] text-amber-700 dark:text-amber-400"
      :class="variant === 'full' ? 'px-4 pb-2' : 'px-3 pb-1.5 pt-0.5'"
    >
      ⤳ Not live-injectable — sending resumes this session as a <strong>new</strong> session. Start it via <code>agent-dashboard live</code> for live injection (uses tmux automatically if present, pty broker otherwise).
    </p>
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
