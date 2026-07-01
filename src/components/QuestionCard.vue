<script setup lang="ts">
import type { PendingQuestion } from '../types'
import { computed, onUnmounted, ref, watch } from 'vue'
import { useAnswerQuestion } from '../composables/useAnswerQuestion'
import { ANSWER_CONFIRM_MS } from '../utils/timing'
import AppButton from './ui/AppButton.vue'

const props = defineProps<{
  pid: number
  pendingQuestion: PendingQuestion
  liveInjectable: boolean
}>()

// One entry per question: array of selected labels.
const selections = ref<string[][]>(props.pendingQuestion.questions.map(() => []))
// One entry per question: typed "type your own answer" text (empty = unused).
const customTexts = ref<string[]>(props.pendingQuestion.questions.map(() => ''))
// One entry per question: whether the custom-answer textarea is expanded.
const customOpen = ref<boolean[]>(props.pendingQuestion.questions.map(() => false))
const chatOpen = ref(false)
const chatText = ref('')

// toolUseID of the in-flight confirmation window; null = idle or failed.
const awaitingConfirmToolUseID = ref<string | null>(null)
const confirmFailed = ref(false)
const confirmTimer = ref<ReturnType<typeof setTimeout> | null>(null)

function clearConfirmTimer() {
  if (confirmTimer.value !== null) {
    clearTimeout(confirmTimer.value)
    confirmTimer.value = null
  }
}

// Reset all local state and any confirmation state when a new question arrives.
watch(
  () => props.pendingQuestion.toolUseID,
  () => {
    selections.value = props.pendingQuestion.questions.map(() => [])
    customTexts.value = props.pendingQuestion.questions.map(() => '')
    customOpen.value = props.pendingQuestion.questions.map(() => false)
    chatOpen.value = false
    chatText.value = ''
    clearConfirmTimer()
    awaitingConfirmToolUseID.value = null
    confirmFailed.value = false
  },
)

const { isSending, sendStatus, sendError, submit } = useAnswerQuestion()

const allAnswered = computed(() =>
  selections.value.every((sel, i) => sel.length > 0 || customTexts.value[i].trim().length > 0),
)

const chatReady = computed(() => chatText.value.trim().length > 0)

function toggleOption(questionIdx: number, label: string, isMultiSelect: boolean) {
  // A picked option and a typed custom answer are mutually exclusive.
  customTexts.value[questionIdx] = ''
  if (isMultiSelect) {
    const current = selections.value[questionIdx]
    const idx = current.indexOf(label)
    selections.value[questionIdx] = idx === -1
      ? [...current, label]
      : current.filter((_, i) => i !== idx)
  }
  else {
    selections.value[questionIdx] = [label]
  }
}

function toggleCustomOpen(questionIdx: number) {
  customOpen.value[questionIdx] = !customOpen.value[questionIdx]
}

function onCustomTextInput(questionIdx: number, value: string) {
  customTexts.value[questionIdx] = value
  if (value.trim())
    selections.value[questionIdx] = []
}

function startConfirmWindow() {
  clearConfirmTimer()
  const toolUseID = props.pendingQuestion.toolUseID
  awaitingConfirmToolUseID.value = toolUseID
  confirmFailed.value = false
  // Card unmounting (parent v-if clears on resolution) = confirmed success.
  // If we're still mounted after ANSWER_CONFIRM_MS, the answer wasn't registered.
  confirmTimer.value = setTimeout(() => {
    if (awaitingConfirmToolUseID.value === toolUseID) {
      confirmFailed.value = true
      awaitingConfirmToolUseID.value = null
    }
  }, ANSWER_CONFIRM_MS)
}

async function handleSubmit() {
  const answers = props.pendingQuestion.questions.map((q, i) => {
    const customText = customTexts.value[i].trim()
    return {
      header: q.header,
      selected: customText ? [] : selections.value[i],
      customText: customText || undefined,
    }
  })
  const ok = await submit(props.pid, props.pendingQuestion.toolUseID, answers)
  if (ok)
    startConfirmWindow()
}

async function handleChatSubmit() {
  const ok = await submit(props.pid, props.pendingQuestion.toolUseID, [], chatText.value.trim())
  if (ok)
    startConfirmWindow()
}

onUnmounted(() => {
  clearConfirmTimer()
})
</script>

<template>
  <div class="flex flex-col gap-3">
    <div
      v-for="(question, qIdx) in pendingQuestion.questions"
      :key="question.header"
      class="flex flex-col gap-1.5"
    >
      <div class="text-[12px] font-semibold text-fg">
        {{ question.header }}
      </div>
      <div class="text-[11px] text-fg-mute leading-snug mb-0.5">
        {{ question.question }}
      </div>

      <fieldset class="border-none m-0 p-0 flex flex-col gap-1.5">
        <legend class="sr-only">
          {{ question.header }}
        </legend>
        <label
          v-for="option in question.options"
          :key="option.label"
          class="flex items-start gap-2 cursor-pointer"
          :class="{ 'opacity-60 cursor-default': !liveInjectable }"
        >
          <input
            v-if="question.multiSelect"
            type="checkbox"
            :name="`q-${pendingQuestion.toolUseID}-${qIdx}`"
            :checked="selections[qIdx].includes(option.label)"
            :disabled="!liveInjectable"
            class="mt-0.5 accent-accent shrink-0"
            @change="toggleOption(qIdx, option.label, true)"
          >
          <input
            v-else
            type="radio"
            :name="`q-${pendingQuestion.toolUseID}-${qIdx}`"
            :checked="selections[qIdx].includes(option.label)"
            :disabled="!liveInjectable"
            class="mt-0.5 accent-accent shrink-0"
            @change="toggleOption(qIdx, option.label, false)"
          >
          <span class="flex flex-col">
            <span class="text-[12px] font-medium text-fg leading-snug">{{ option.label }}</span>
            <span v-if="option.description" class="text-[11px] text-fg-faint leading-snug">{{ option.description }}</span>
          </span>
        </label>
      </fieldset>

      <div v-if="liveInjectable" class="flex flex-col gap-1">
        <button
          type="button"
          :data-testid="`custom-toggle-${qIdx}`"
          class="self-start text-[11px] text-accent underline decoration-dotted"
          @click="toggleCustomOpen(qIdx)"
        >
          {{ customOpen[qIdx] ? 'Hide custom answer' : 'Type your own answer' }}
        </button>
        <textarea
          v-if="customOpen[qIdx] || customTexts[qIdx]"
          :aria-label="`Custom answer for ${question.header}`"
          :data-testid="`custom-textarea-${qIdx}`"
          :value="customTexts[qIdx]"
          rows="2"
          class="text-[12px] rounded border border-line bg-raised px-2 py-1 text-fg"
          @input="onCustomTextInput(qIdx, ($event.target as HTMLTextAreaElement).value)"
        />
      </div>
    </div>

    <p v-if="!liveInjectable" data-testid="terminal-note" class="m-0 text-[11px] text-fg-faint italic">
      Answer in your terminal — this session can't be driven from the dashboard.
    </p>

    <div v-if="liveInjectable" class="flex items-center gap-2 flex-wrap">
      <AppButton
        variant="primary"
        size="sm"
        data-testid="send-answer-btn"
        :disabled="!allAnswered || isSending || awaitingConfirmToolUseID !== null"
        @click="handleSubmit"
      >
        {{ isSending ? 'Sending…' : awaitingConfirmToolUseID !== null ? 'Waiting…' : 'Send answer' }}
      </AppButton>
      <span v-if="sendStatus === 'sent'" class="text-[11px] text-success-text">Sent</span>
      <span v-if="sendError" class="text-[11px] text-danger-text">{{ sendError }}</span>
      <span
        v-if="confirmFailed"
        role="status"
        data-testid="confirm-failed-msg"
        class="text-[11px] text-danger-text"
      >
        Answer sent, but the session hasn't registered it — check your terminal.
      </span>
    </div>

    <div v-if="liveInjectable" class="flex flex-col gap-1.5 pt-1 border-t border-line">
      <button
        type="button"
        data-testid="chat-toggle"
        class="self-start text-[11px] text-accent underline decoration-dotted"
        @click="chatOpen = !chatOpen"
      >
        {{ chatOpen ? 'Hide chat' : 'Chat about this instead' }}
      </button>
      <template v-if="chatOpen">
        <textarea
          v-model="chatText"
          aria-label="Chat message instead of answering"
          data-testid="chat-textarea"
          rows="2"
          class="text-[12px] rounded border border-line bg-raised px-2 py-1 text-fg"
        />
        <AppButton
          variant="secondary"
          size="sm"
          data-testid="chat-send-btn"
          :disabled="!chatReady || isSending || awaitingConfirmToolUseID !== null"
          @click="handleChatSubmit"
        >
          {{ isSending ? 'Sending…' : 'Send as chat' }}
        </AppButton>
      </template>
    </div>
  </div>
</template>
