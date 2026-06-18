import { SSE_FALLBACK_POLL_MS, SSE_RETRY_DELAY_MS } from '../utils/sse'

export interface CreateSseResourceOptions {
  /** SSE endpoint, e.g. '/api/tasks/stream'. */
  streamUrl: string
  /** Initial load + poll body. Owns its own state/loading/error. */
  fetchInitial: () => void | Promise<void>
  /** Raw SSE `event.data`; caller parses and applies. */
  onMessage: (data: string) => void
  /** Poll cadence after a permanent SSE drop. Default {@link SSE_FALLBACK_POLL_MS}. */
  fallbackPollMs?: number
  /** Stop the stream while the tab is hidden, resume on re-show. */
  pauseWhenHidden?: boolean
  /** Run an immediate fetch when polling starts (don't wait one interval). */
  pollLeading?: boolean
  /** Fires once per (re)connect, on the first frame — e.g. drain queued messages. */
  onConnected?: () => void
}

export interface SseResource {
  startStream: () => void
  stopStream: () => void
}

/**
 * Connection-lifecycle factory shared by SSE-first composables. Owns the
 * EventSource, ref-counted subscriber tracking, the CLOSED→poll→retry fallback,
 * and optional visibility pause. The caller keeps its own reactive state and
 * supplies `fetchInitial` (load/poll) + `onMessage` (apply a live frame).
 */
export function createSseResource(opts: CreateSseResourceOptions): SseResource {
  const {
    streamUrl,
    fetchInitial,
    onMessage,
    fallbackPollMs = SSE_FALLBACK_POLL_MS,
    pauseWhenHidden = false,
    pollLeading = false,
    onConnected,
  } = opts

  let eventSource: EventSource | null = null
  let pollTimer: ReturnType<typeof setInterval> | null = null
  let sseRetryTimer: ReturnType<typeof setTimeout> | null = null
  let subscriberCount = 0
  let visibilityListenerAttached = false

  function startSSE(): void {
    if (subscriberCount <= 0)
      return
    if (pauseWhenHidden && typeof document !== 'undefined' && document.hidden)
      return
    if (eventSource)
      stopSSE()
    eventSource = new EventSource(streamUrl)

    let connected = false
    eventSource.onmessage = (e) => {
      onMessage(e.data)
      // First frame after (re)connect — drain hook for offline-queued work.
      if (!connected) {
        connected = true
        onConnected?.()
      }
    }
    eventSource.onerror = () => {
      if (eventSource?.readyState === EventSource.CLOSED) {
        // Permanent failure — fall back to polling, retry SSE after a delay.
        stopSSE()
        startPolling()
        sseRetryTimer = setTimeout(() => {
          stopPolling()
          startSSE()
        }, SSE_RETRY_DELAY_MS)
      }
      // Transient error — EventSource reconnects automatically.
    }
  }

  function stopSSE(): void {
    if (eventSource) {
      eventSource.close()
      eventSource = null
    }
  }

  function startPolling(): void {
    if (pollTimer)
      return
    if (pollLeading)
      void fetchInitial()
    pollTimer = setInterval(() => {
      void fetchInitial()
    }, fallbackPollMs)
  }

  function stopPolling(): void {
    if (pollTimer) {
      clearInterval(pollTimer)
      pollTimer = null
    }
  }

  function clearRetry(): void {
    if (sseRetryTimer) {
      clearTimeout(sseRetryTimer)
      sseRetryTimer = null
    }
  }

  function handleVisibilityChange(): void {
    if (document.hidden) {
      stopSSE()
      stopPolling()
      clearRetry()
    }
    else {
      startSSE()
    }
  }

  function startStream(): void {
    subscriberCount++
    if (subscriberCount > 1)
      return
    void fetchInitial()
    startSSE()
    if (pauseWhenHidden && !visibilityListenerAttached && typeof document !== 'undefined') {
      document.addEventListener('visibilitychange', handleVisibilityChange)
      visibilityListenerAttached = true
    }
  }

  function stopStream(): void {
    subscriberCount--
    if (subscriberCount <= 0) {
      stopSSE()
      stopPolling()
      clearRetry()
      if (visibilityListenerAttached && typeof document !== 'undefined') {
        document.removeEventListener('visibilitychange', handleVisibilityChange)
        visibilityListenerAttached = false
      }
      subscriberCount = 0
    }
  }

  return { startStream, stopStream }
}
