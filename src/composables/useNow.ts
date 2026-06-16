import { onMounted, onUnmounted, ref } from 'vue'

const TICK_MS = 30_000

// Shared singleton: one interval drives all consumers.
let refCount = 0
let handle: ReturnType<typeof setInterval> | null = null
const nowMs = ref(Date.now())

function tick() {
  nowMs.value = Date.now()
}

export function useNow() {
  onMounted(() => {
    if (refCount === 0) {
      handle = setInterval(tick, TICK_MS)
    }
    refCount++
  })

  onUnmounted(() => {
    refCount--
    if (refCount === 0 && handle !== null) {
      clearInterval(handle)
      handle = null
    }
  })

  return { nowMs }
}
