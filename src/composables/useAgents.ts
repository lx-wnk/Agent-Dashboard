import { ref, onUnmounted } from 'vue'
import type { Agent } from '../types'

const agents = ref<Agent[]>([])
const selectedAgent = ref<Agent | null>(null)
const isLoading = ref(true)
const error = ref<string | null>(null)

let intervalId: ReturnType<typeof setInterval> | null = null
let subscriberCount = 0

async function fetchAgents() {
  try {
    const res = await fetch('/api/agents')
    if (!res.ok) throw new Error(`HTTP ${res.status}`)
    const data: Agent[] = await res.json()
    agents.value = data
    error.value = null

    // Keep selected agent in sync across refreshes (clear if agent is gone)
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
    selectedAgent,
    isLoading,
    error,
    selectAgent,
  }
}
