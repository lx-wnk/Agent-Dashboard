import { onMounted, onUnmounted } from 'vue'

export function useVisibilityPolling(fn: () => void | Promise<void>, intervalMs: number): void {
  let handle: ReturnType<typeof setInterval> | null = null

  function start() {
    if (handle)
      return
    handle = setInterval(fn, intervalMs)
  }

  function stop() {
    if (handle) {
      clearInterval(handle)
      handle = null
    }
  }

  function onVisibilityChange() {
    if (document.hidden) {
      stop()
    }
    else {
      void fn()
      start()
    }
  }

  onMounted(() => {
    void fn()
    start()
    document.addEventListener('visibilitychange', onVisibilityChange)
  })

  onUnmounted(() => {
    stop()
    document.removeEventListener('visibilitychange', onVisibilityChange)
  })
}
