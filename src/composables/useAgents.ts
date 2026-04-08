import { ref, computed, onUnmounted, watch } from 'vue'
import type { Agent } from '../types'

type ViewMode = 'list' | 'cards'

const agents = ref<Agent[]>([])
const selectedAgent = ref<Agent | null>(null)
const isLoading = ref(true)
const error = ref<string | null>(null)
const searchQuery = ref('')
const stored = localStorage.getItem('agent-view-mode')
const viewMode = ref<ViewMode>(stored === 'list' || stored === 'cards' ? stored : 'list')

let intervalId: ReturnType<typeof setInterval> | null = null
let subscriberCount = 0

async function fetchAgents() {
  try {
    const res = await fetch('/api/agents')
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const data: Agent[] = await res.json()
    agents.value = data
    error.value = null

    if (selectedAgent.value) {
      const updated = data.find(a => a.sessionId === selectedAgent.value!.sessionId)
      selectedAgent.value = updated ?? null
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Unknown error'
  } finally {
    isLoading.value = false
  }
}

const filteredAgents = computed(() => {
  const q = searchQuery.value.toLowerCase().trim()
  if (!q) return agents.value
  return agents.value.filter(a =>
    a.projectName.toLowerCase().includes(q) ||
    a.projectPath.toLowerCase().includes(q) ||
    (a.lastOutput?.toLowerCase().includes(q) ?? false) ||
    a.sessionId.toLowerCase().includes(q) ||
    (a.currentAction?.toLowerCase().includes(q) ?? false)
  )
})

// Persist viewMode to localStorage
watch(viewMode, (v) => localStorage.setItem('agent-view-mode', v))

function startPolling() {
  subscriberCount++
  if (intervalId) return
  fetchAgents()
  intervalId = setInterval(fetchAgents, 3000)
}

function stopPolling() {
  subscriberCount--
  if (subscriberCount <= 0 && intervalId) {
    clearInterval(intervalId)
    intervalId = null
    subscriberCount = 0
  }
}

export function useAgents() {
  startPolling()
  onUnmounted(stopPolling)

  function selectAgent(agent: Agent | null) {
    selectedAgent.value = agent
  }

  return {
    agents,
    filteredAgents,
    selectedAgent,
    isLoading,
    error,
    searchQuery,
    viewMode,
    selectAgent,
  }
}
