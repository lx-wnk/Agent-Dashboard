import { onMounted, onUnmounted, ref } from 'vue'
import { countPending, replayPending } from '../utils/pendingMessages'

const pendingCount = ref(0)

async function refreshCount() {
  try {
    pendingCount.value = await countPending()
  }
  catch {
    // IndexedDB unavailable in this context (e.g. private browsing)
  }
}

export async function drainPendingMessages(): Promise<void> {
  try {
    await replayPending()
    await refreshCount()
  }
  catch {
    // Ignore errors — next drain attempt will retry
  }
}

export function usePendingMessages() {
  let interval: ReturnType<typeof setInterval> | null = null

  onMounted(async () => {
    await refreshCount()
    // Periodically re-check — catches cases where SW replay removed items
    interval = setInterval(refreshCount, 10_000)
  })

  onUnmounted(() => {
    if (interval) {
      clearInterval(interval)
      interval = null
    }
  })

  return { pendingCount, refreshCount, drainPendingMessages }
}
