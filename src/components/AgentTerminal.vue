<script setup lang="ts">
import type { UseTerminalSocket } from '../composables/useTerminalSocket'
import type { DetectedQuestion } from '../utils/askQuestionScreen'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useTerminalSocket } from '../composables/useTerminalSocket'
import { detectQuestion } from '../utils/askQuestionScreen'
import QuestionOverlay from './QuestionOverlay.vue'
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

function pollForQuestion() {
  if (!term)
    return
  detectedQuestion.value = detectQuestion(readVisibleRows(term))
}

function sendToTerminal(bytes: Uint8Array) {
  socket?.send(bytes)
}

onMounted(() => {
  const container = containerRef.value
  if (!container)
    return

  term = new Terminal({
    convertEol: false,
    cursorBlink: true,
    fontFamily: 'Menlo, Monaco, "Courier New", monospace',
  })
  const fit = new FitAddon()
  term.loadAddon(fit)
  term.open(container)
  fit.fit()

  socket = useTerminalSocket(props.pid, {
    onData: bytes => term?.write(bytes),
  })

  term.onData((data) => {
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
