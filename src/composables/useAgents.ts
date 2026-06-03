import type { Agent } from '../types'
import { computed, onUnmounted, ref, shallowRef, watch } from 'vue'
import { SSE_RETRY_DELAY_MS } from '../utils/sse'
import { drainPendingMessages } from './usePendingMessages'

export interface TrendPoint {
  t: number
  cost: number
  tokens: number
}

const agents = shallowRef<Agent[]>([])
const costTrend = ref<TrendPoint[]>([])
const selectedAgent = ref<Agent | null>(null)
const isLoading = ref(true)
const error = ref<string | null>(null)
const searchQuery = ref('')
const debouncedQuery = ref('')
// Provider filter — append-only addition to the existing search/view UI.
// When true, only agents with provider === 'claude' (or unset, treated as
// claude) are shown. Persisted to localStorage so the preference survives
// page reloads.
const hideNonClaudeStored = typeof localStorage !== 'undefined' ? localStorage.getItem('agent-hide-non-claude') : null
const hideNonClaude = ref<boolean>(hideNonClaudeStored === 'true')

let eventSource: EventSource | null = null
let intervalId: ReturnType<typeof setInterval> | null = null
let sseRetryTimer: ReturnType<typeof setTimeout> | null = null
let subscriberCount = 0
let debounceTimer: ReturnType<typeof setTimeout> | null = null
let visibilityListenerAttached = false

function handleAgentData(data: Agent[], trend?: TrendPoint[]) {
  agents.value = data
  if (trend)
    costTrend.value = trend
  error.value = null
  isLoading.value = false

  if (selectedAgent.value) {
    const updated = data.find(a => a.sessionId === selectedAgent.value!.sessionId)
    selectedAgent.value = updated ?? null
  }
}

async function fetchAgents() {
  try {
    const res = await fetch('/api/agents')
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    handleAgentData(await res.json())
  }
  catch (e) {
    error.value = e instanceof Error ? e.message : 'Unknown error'
    isLoading.value = false
  }
}

function handleVisibilityChange() {
  if (document.hidden) {
    stopSSE()
    stopPolling()
    if (sseRetryTimer) {
      clearTimeout(sseRetryTimer)
      sseRetryTimer = null
    }
  }
  else {
    startSSE()
  }
}

function startSSE() {
  if (subscriberCount <= 0)
    return
  if (typeof document !== 'undefined' && document.hidden)
    return
  if (eventSource) stopSSE()
  eventSource = new EventSource('/api/agents/stream')

  let drainedOnReconnect = false
  eventSource.onmessage = (event) => {
    try {
      const payload = JSON.parse(event.data)
      handleAgentData(payload.agents, payload.trend)
    }
    catch { /* ignore parse errors */ }

    // On the first SSE message after (re)connecting, drain any queued offline messages.
    // This is the fallback for browsers that do not support the Background Sync API.
    if (!drainedOnReconnect) {
      drainedOnReconnect = true
      void drainPendingMessages()
    }
  }

  eventSource.onerror = () => {
    if (eventSource?.readyState === EventSource.CLOSED) {
      // Permanent failure — fall back to polling, retry SSE after 30s
      stopSSE()
      startPolling()
      sseRetryTimer = setTimeout(() => {
        stopPolling()
        startSSE()
      }, SSE_RETRY_DELAY_MS)
    }
    // Transient error — EventSource reconnects automatically
  }
}

function stopSSE() {
  if (eventSource) {
    eventSource.close()
    eventSource = null
  }
}

const filteredAgents = computed(() => {
  const q = debouncedQuery.value.toLowerCase().trim()
  let list = agents.value
  if (hideNonClaude.value)
    list = list.filter(a => !a.provider || a.provider === 'claude')
  if (!q)
    return list
  return list.filter(a =>
    a.projectName.toLowerCase().includes(q)
    || a.projectPath.toLowerCase().includes(q)
    || (a.lastOutput?.toLowerCase().includes(q) ?? false)
    || a.sessionId.toLowerCase().includes(q)
    || (a.currentAction?.toLowerCase().includes(q) ?? false)
    || (a.machine?.toLowerCase().includes(q) ?? false),
  )
})

// Debounce search query
watch(searchQuery, (q) => {
  if (debounceTimer)
    clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    debouncedQuery.value = q
  }, 200)
})

// Persist provider filter
watch(hideNonClaude, (v) => {
  if (typeof localStorage !== 'undefined')
    localStorage.setItem('agent-hide-non-claude', String(v))
})

function startPolling() {
  if (intervalId)
    return
  fetchAgents()
  intervalId = setInterval(fetchAgents, 3000)
}

function stopPolling() {
  if (intervalId) {
    clearInterval(intervalId)
    intervalId = null
  }
}

function startDataStream() {
  subscriberCount++
  if (subscriberCount > 1)
    return

  fetchAgents()
  startSSE()

  if (!visibilityListenerAttached && typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', handleVisibilityChange)
    visibilityListenerAttached = true
  }
}

function stopDataStream() {
  subscriberCount--
  if (subscriberCount <= 0) {
    stopSSE()
    stopPolling()
    if (sseRetryTimer) {
      clearTimeout(sseRetryTimer)
      sseRetryTimer = null
    }
    if (visibilityListenerAttached && typeof document !== 'undefined') {
      document.removeEventListener('visibilitychange', handleVisibilityChange)
      visibilityListenerAttached = false
    }
    subscriberCount = 0
  }
}

export function useAgents(options?: { autoStart?: boolean }) {
  if (options?.autoStart !== false)
    startDataStream()
  onUnmounted(stopDataStream)

  function selectAgent(agent: Agent | null) {
    selectedAgent.value = agent
  }

  return {
    agents,
    costTrend,
    filteredAgents,
    selectedAgent,
    isLoading,
    error,
    searchQuery,
    hideNonClaude,
    selectAgent,
    startStream: startDataStream,
  }
}
