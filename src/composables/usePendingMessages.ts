import { onMounted, onUnmounted, ref } from 'vue'
import { countPending, replayPending } from '../utils/pendingMessages'
import { SW_MSG_MESSAGES_REPLAYED } from '../utils/swConstants'

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

function onSwMessage(event: MessageEvent): void {
  if (event.data?.type === SW_MSG_MESSAGES_REPLAYED && event.data.count > 0) {
    window.dispatchEvent(new CustomEvent('drain-success'))
  }
}

export function usePendingMessages() {
  let interval: ReturnType<typeof setInterval> | null = null

  onMounted(async () => {
    await refreshCount()
    // Periodically re-check — catches cases where SW replay removed items
    interval = setInterval(refreshCount, 10_000)
    if ('serviceWorker' in navigator)
      navigator.serviceWorker.addEventListener('message', onSwMessage)
  })

  onUnmounted(() => {
    if (interval) {
      clearInterval(interval)
      interval = null
    }
    if ('serviceWorker' in navigator)
      navigator.serviceWorker.removeEventListener('message', onSwMessage)
  })

  return { pendingCount, refreshCount, drainPendingMessages }
}
