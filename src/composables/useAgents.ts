import type { Agent } from '../types'
import { computed, onUnmounted, ref, watch } from 'vue'

export interface TrendPoint {
  t: number
  cost: number
  tokens: number
}

type ViewMode = 'list' | 'cards' | 'kanban'

const agents = ref<Agent[]>([])
const costTrend = ref<TrendPoint[]>([])
const selectedAgent = ref<Agent | null>(null)
const isLoading = ref(true)
const error = ref<string | null>(null)
const searchQuery = ref('')
const debouncedQuery = ref('')
const stored = localStorage.getItem('agent-view-mode')
const viewMode = ref<ViewMode>(stored === 'list' || stored === 'cards' || stored === 'kanban' ? stored : 'list')

let eventSource: EventSource | null = null
let intervalId: ReturnType<typeof setInterval> | null = null
let sseRetryTimer: ReturnType<typeof setTimeout> | null = null
let subscriberCount = 0
let debounceTimer: ReturnType<typeof setTimeout> | null = null

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

function startSSE() {
  if (subscriberCount <= 0) return
  eventSource = new EventSource('/api/agents/stream')

  eventSource.onmessage = (event) => {
    try {
      const payload = JSON.parse(event.data)
      handleAgentData(payload.agents, payload.trend)
    }
    catch { /* ignore parse errors */ }
  }

  eventSource.onerror = () => {
    if (eventSource?.readyState === EventSource.CLOSED) {
      // Permanent failure — fall back to polling, retry SSE after 30s
      stopSSE()
      startPolling()
      sseRetryTimer = setTimeout(() => {
        stopPolling()
        startSSE()
      }, 30000)
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
  if (!q)
    return agents.value
  return agents.value.filter(a =>
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

// Persist viewMode to localStorage
watch(viewMode, v => localStorage.setItem('agent-view-mode', v))

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

  // Try SSE first, with an initial fetch for immediate data
  fetchAgents()
  startSSE()
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
    subscriberCount = 0
  }
}

export function useAgents() {
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
    viewMode,
    selectAgent,
  }
}
