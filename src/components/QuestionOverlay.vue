<script setup lang="ts">
import type { AnswerIntent } from '../utils/answerKeys'
import type { DetectedQuestion } from '../utils/askQuestionScreen'
import { encodeAnswer } from '../utils/answerKeys'
import QuestionCard from './QuestionCard.vue'

const props = defineProps<{
  question: DetectedQuestion | null
  send: (bytes: Uint8Array) => void
}>()

// Overlay hides itself once the caller's `question` goes null (the modal left
// the terminal screen) — that disappearance doubles as the success confirmation.
function handleAnswer(intent: AnswerIntent) {
  const encoder = new TextEncoder()
  for (const token of encodeAnswer(intent))
    props.send(encoder.encode(token))
}
</script>

<template>
  <div
    v-if="question"
    data-testid="question-overlay"
    class="absolute inset-0 flex items-center justify-center bg-black/60 p-4 z-10"
  >
    <div class="max-w-md w-full rounded-lg bg-card border border-line p-4 shadow-lg">
      <QuestionCard :detected-question="question" @answer="handleAnswer" />
    </div>
  </div>
</template>
