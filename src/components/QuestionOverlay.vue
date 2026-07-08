<script setup lang="ts">
import type { AnswerIntent } from '../utils/answerKeys'
import type { DetectedQuestion } from '../utils/askQuestionScreen'
import { nextTick, ref, watch } from 'vue'
import { encodeAnswer } from '../utils/answerKeys'
import QuestionCard from './QuestionCard.vue'

const props = defineProps<{
  question: DetectedQuestion | null
  send: (bytes: Uint8Array) => void
}>()

const dialogRef = ref<HTMLElement | null>(null)

// Overlay hides itself once the caller's `question` goes null (the modal left
// the terminal screen) — that disappearance doubles as the success confirmation.
function handleAnswer(intent: AnswerIntent) {
  const encoder = new TextEncoder()
  for (const token of encodeAnswer(intent))
    props.send(encoder.encode(token))
}

// Move focus into the overlay when a question appears so the underlying
// terminal (which stops accepting input while the overlay is up) doesn't
// keep keyboard focus.
watch(() => props.question, (question) => {
  if (!question)
    return
  nextTick(() => {
    const firstControl = dialogRef.value?.querySelector<HTMLElement>('input, textarea, button')
    firstControl?.focus()
  })
})
</script>

<template>
  <div
    v-if="question"
    ref="dialogRef"
    data-testid="question-overlay"
    role="dialog"
    aria-modal="true"
    :aria-label="question.header"
    class="absolute inset-0 flex items-center justify-center bg-black/60 p-4 z-10"
  >
    <div class="max-w-md w-full rounded-lg bg-card border border-line p-4 shadow-lg">
      <QuestionCard :detected-question="question" @answer="handleAnswer" />
    </div>
  </div>
</template>
