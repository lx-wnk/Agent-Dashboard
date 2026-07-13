<script setup lang="ts">
import type { UseTerminalSocket } from '@/composables/useTerminalSocket'
import type { DetectedQuestion } from '@/utils/askQuestionScreen'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import QuestionOverlay from '@/components/QuestionOverlay.vue'
import { useTerminalSocket } from '@/composables/useTerminalSocket'
import { detectQuestion } from '@/utils/askQuestionScreen'
import '@xterm/xterm/css/xterm.css'

const props = defineProps<{
  pid: number
}>()

// How often the visible screen is re-parsed for an AskUserQuestion modal.
// Throttled rather than reactive to xterm's own render events, since those can
// fire many times per burst of output.
const QUESTION_POLL_MS = 200

const containerRef = ref<HTMLElement | null>(null)
const detectedQuestion = ref<DetectedQuestion | null>(null)
// Signature of the currently-shown modal. The poll replaces detectedQuestion
// only when this changes, so an unchanged screen never re-creates the object
// and QuestionCard's local answer state (selections, typed text) survives.
let currentSig: string | null = null
let term: Terminal | null = null
let socket: UseTerminalSocket | null = null
let resizeObserver: ResizeObserver | null = null
let questionPollTimer: ReturnType<typeof setInterval> | null = null

function readVisibleRows(t: Terminal): string[] {
  const buffer = t.buffer.active
  const rows: string[] = []
  for (let i = 0; i < t.rows; i++) {
    const line = buffer.getLine(buffer.viewportY + i)
    rows.push(line ? line.translateToString(true) : '')
  }
  return rows
}

// detectQuestion is stateless w.r.t. user input, so its output changes only
// when the SCREEN changes. Identify "the same modal" by structure alone.
function questionSignature(q: DetectedQuestion | null): string | null {
  return q === null
    ? null
    : JSON.stringify([q.header, q.question, q.multiSelect, q.options.map(o => [o.index, o.label])])
}

function pollForQuestion() {
  if (!term)
    return
  const detected = detectQuestion(readVisibleRows(term))
  const sig = questionSignature(detected)
  if (sig === currentSig)
    return
  currentSig = sig
  detectedQuestion.value = detected
}

function sendToTerminal(bytes: Uint8Array) {
  socket?.send(bytes)
}

// Keep keyboard focus out of the xterm textarea while the overlay owns
// input, and return it once the overlay clears.
watch(detectedQuestion, (question) => {
  if (question)
    term?.blur()
  else
    term?.focus()
})

onMounted(() => {
  const container = containerRef.value
  if (!container)
    return

  term = new Terminal({
    convertEol: false,
    cursorBlink: true,
    fontFamily: 'Menlo, Monaco, "Courier New", monospace',
    screenReaderMode: true,
  })
  const fit = new FitAddon()
  term.loadAddon(fit)
  term.open(container)
  fit.fit()

  socket = useTerminalSocket(props.pid, {
    onData: bytes => term?.write(bytes),
  })

  term.onData((data) => {
    // While a question overlay is showing, the user answers through the
    // overlay's controls — raw keystrokes must not race those encoded bytes
    // onto the same pty.
    if (detectedQuestion.value)
      return
    socket?.send(new TextEncoder().encode(data))
  })

  socket.resize(term.cols, term.rows)

  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(() => {
      fit.fit()
      if (term)
        socket?.resize(term.cols, term.rows)
    })
    resizeObserver.observe(container)
  }

  questionPollTimer = setInterval(pollForQuestion, QUESTION_POLL_MS)
})

onBeforeUnmount(() => {
  if (questionPollTimer !== null)
    clearInterval(questionPollTimer)
  resizeObserver?.disconnect()
  socket?.close()
  term?.dispose()
})
</script>

<template>
  <div class="agent-terminal-wrapper">
    <div ref="containerRef" class="agent-terminal" />
    <QuestionOverlay :question="detectedQuestion" :send="sendToTerminal" />
  </div>
</template>

<style scoped>
.agent-terminal-wrapper {
  position: relative;
  width: 100%;
  height: 100%;
}

.agent-terminal {
  width: 100%;
  height: 100%;
}
</style>
