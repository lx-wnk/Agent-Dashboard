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

const FOCUSABLE_SELECTOR = 'a[href], button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [tabindex]:not([tabindex="-1"])'

// QuestionCard renders its conditional controls with v-if, so hidden options
// are absent from the DOM rather than merely invisible — no visibility filter
// is needed here.
function focusableElements(): HTMLElement[] {
  if (!dialogRef.value)
    return []
  return Array.from(dialogRef.value.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR))
}

// Overlay hides itself once the caller's `question` goes null (the modal left
// the terminal screen) — that disappearance doubles as the success confirmation.
function handleAnswer(intent: AnswerIntent) {
  const encoder = new TextEncoder()
  for (const token of encodeAnswer(intent))
    props.send(encoder.encode(token))
}

// Trap Tab / Shift+Tab within the modal so keyboard focus cannot escape back to
// the underlying terminal (which is inert while the overlay is up). Listening on
// the container catches Tab bubbling from any inner control.
function onKeydown(event: KeyboardEvent) {
  if (event.key !== 'Tab')
    return
  const focusables = focusableElements()
  if (focusables.length === 0) {
    event.preventDefault()
    return
  }
  const first = focusables[0]
  const last = focusables[focusables.length - 1]
  const active = document.activeElement
  if (event.shiftKey && active === first) {
    event.preventDefault()
    last.focus()
  }
  else if (!event.shiftKey && active === last) {
    event.preventDefault()
    first.focus()
  }
}

// Move focus into the overlay when a question appears so the underlying
// terminal (which stops accepting input while the overlay is up) doesn't
// keep keyboard focus.
watch(() => props.question, (question) => {
  if (!question)
    return
  nextTick(() => {
    focusableElements()[0]?.focus()
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
    @keydown="onKeydown"
  >
    <div class="max-w-md w-full rounded-lg bg-card border border-line p-4 shadow-lg">
      <QuestionCard :detected-question="question" @answer="handleAnswer" />
    </div>
  </div>
</template>
