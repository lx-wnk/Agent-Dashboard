<script setup lang="ts">
import type { UseTerminalSocket } from '../composables/useTerminalSocket'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useTerminalSocket } from '../composables/useTerminalSocket'
import '@xterm/xterm/css/xterm.css'

const props = defineProps<{
  pid: number
}>()

const containerRef = ref<HTMLElement | null>(null)
let term: Terminal | null = null
let socket: UseTerminalSocket | null = null
let resizeObserver: ResizeObserver | null = null

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
})

onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  socket?.close()
  term?.dispose()
})
</script>

<template>
  <div ref="containerRef" class="agent-terminal" />
</template>

<style scoped>
.agent-terminal {
  width: 100%;
  height: 100%;
}
</style>
