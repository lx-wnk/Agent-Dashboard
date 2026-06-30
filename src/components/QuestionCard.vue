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

// Reset selections and any confirmation state when a new question arrives.
watch(
  () => props.pendingQuestion.toolUseID,
  () => {
    selections.value = props.pendingQuestion.questions.map(() => [])
    clearConfirmTimer()
    awaitingConfirmToolUseID.value = null
    confirmFailed.value = false
  },
)

const { isSending, sendStatus, sendError, submit } = useAnswerQuestion()

const allAnswered = computed(() =>
  selections.value.every(sel => sel.length > 0),
)

function toggleOption(questionIdx: number, label: string, isMultiSelect: boolean) {
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

async function handleSubmit() {
  const answers = props.pendingQuestion.questions.map((q, i) => ({
    header: q.header,
    selected: selections.value[i],
  }))
  const ok = await submit(props.pid, props.pendingQuestion.toolUseID, answers)
  if (ok) {
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
  </div>
</template>
