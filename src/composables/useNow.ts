import { onMounted, onUnmounted, ref } from 'vue'

const TICK_MS = 30_000

// Shared singleton: one interval drives all consumers.
let refCount = 0
let handle: ReturnType<typeof setInterval> | null = null
const nowMs = ref(Date.now())

function tick() {
  nowMs.value = Date.now()
}

function ensureInterval() {
  if (handle === null)
    handle = setInterval(tick, TICK_MS)
}

export function useNow() {
  onMounted(() => {
    ensureInterval()
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

// For module-scope / non-component consumers (e.g. the useAgents singleton store)
// where Vue lifecycle hooks don't run. Starts the shared interval and holds a
// permanent ref so component unmounts never tear it down.
export function startNowTicking() {
  ensureInterval()
  refCount++
  return { nowMs }
}
