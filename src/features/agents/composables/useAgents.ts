import type { PendingCapabilityDecision } from '@/sdk.generated'
import type { Agent } from '@/types'
import { computed, onUnmounted, ref, shallowRef, watch } from 'vue'
import { startNowTicking } from '@/composables/useNow'
import { drainPendingMessages } from '@/composables/usePendingMessages'
import { createSseResource } from '@/composables/useSseResource'
import { needsAttention, sortByTriage } from '@/utils/attention'
import { errorMessage } from '@/utils/errorMessage'
import { secondsSince, totalTokenCount } from '@/utils/format'
import { AGENTS_POLL_MS } from '@/utils/sse'

export interface TrendPoint {
  t: number
  cost: number
  tokens: number
}

// How long the client-side cost-trend ring buffer retains samples. The status
// bar's burn-rate reads a 5-minute window from it, so we keep a little extra.
const TREND_RETENTION_MS = 10 * 60 * 1000

const agents = shallowRef<Agent[]>([])
const pendingCapabilityDecisions = shallowRef<PendingCapabilityDecision[]>([])
const costTrend = ref<TrendPoint[]>([])
const selectedAgent = ref<Agent | null>(null)
const isLoading = ref(true)
const error = ref<string | null>(null)
const searchQuery = ref('')
const debouncedQuery = ref('')

let debounceTimer: ReturnType<typeof setTimeout> | null = null

function handleAgentData(data: Agent[], _trend?: TrendPoint[], decisions?: PendingCapabilityDecision[]) {
  agents.value = data
  // The server sends the full set every frame, so an absent field means none pending — not "keep the last known list".
  pendingCapabilityDecisions.value = decisions ?? []

  // Build the cost trend client-side: the backend streams an empty trend, but
  // every frame carries the running agents, so we sample total cost/tokens here.
  // A time-based ring buffer (TREND_RETENTION_MS) lets consumers compute a delta
  // over any window regardless of how sparsely frames arrive (frames are
  // de-duplicated server-side, so cost is flat between them anyway).
  const now = Date.now()
  let cost = 0
  let tokens = 0
  for (const a of data) {
    cost += a.costEstimate
    tokens += totalTokenCount(a.tokenUsage)
  }
  const cutoff = now - TREND_RETENTION_MS
  costTrend.value = [...costTrend.value.filter(p => p.t >= cutoff), { t: now, cost, tokens }]

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
    error.value = errorMessage(e)
    isLoading.value = false
  }
}

// SSE frame carries { agents, trend, pendingCapabilityDecisions }; handleAgentData rebuilds
// the cost trend client-side so the streamed trend is intentionally ignored, but
// pendingCapabilityDecisions is authoritative server state and is passed through as-is.
function handleSseMessage(data: string) {
  try {
    const payload = JSON.parse(data)
    handleAgentData(payload.agents, payload.trend, payload.pendingCapabilityDecisions)
  }
  catch { /* ignore parse errors */ }
}

// Agents update frequently → faster fallback cadence + leading poll. The tab is
// paused while hidden, and the first frame after each (re)connect drains any
// offline-queued messages (Background Sync fallback).
const sse = createSseResource({
  streamUrl: '/api/agents/stream',
  fetchInitial: fetchAgents,
  onMessage: handleSseMessage,
  fallbackPollMs: AGENTS_POLL_MS,
  pauseWhenHidden: true,
  pollLeading: true,
  onConnected: () => void drainPendingMessages(),
})

const filteredAgents = computed(() => {
  const q = debouncedQuery.value.toLowerCase().trim()
  const list = agents.value
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

const { nowMs } = startNowTicking()

const attentionAgents = computed(() => {
  const secsOf = (a: Agent) => secondsSince(a.lastActivity, nowMs.value)
  return sortByTriage(
    agents.value.filter(a => needsAttention(a, secsOf(a))),
    secsOf,
  )
})

const attentionCount = computed(() => attentionAgents.value.length)

// Debounce search query
watch(searchQuery, (q) => {
  if (debounceTimer)
    clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    debouncedQuery.value = q
  }, 200)
})

export function useAgents(options?: { autoStart?: boolean }) {
  if (options?.autoStart !== false)
    sse.startStream()
  onUnmounted(sse.stopStream)

  function selectAgent(agent: Agent | null) {
    selectedAgent.value = agent
  }

  // Optimistic removal of a dismissed finished card. The next SSE frame
  // reflects server truth (the discovery file is deleted, so the agent is no
  // longer emitted); if the dismiss DELETE failed, that frame re-adds it.
  function dismissAgent(pid: number) {
    agents.value = agents.value.filter(a => a.pid !== pid)
  }

  // After a spawn we have the new process pid but the scanner/SSE takes a few
  // seconds to surface the agent. Open its modal as soon as it appears, giving
  // up silently after timeoutMs so a never-appearing pid leaks no watcher/timer.
  function selectAgentWhenAvailable(pid: number, timeoutMs = 30000) {
    const existing = agents.value.find(a => a.pid === pid)
    if (existing) {
      selectAgent(existing)
      return
    }

    let stop: (() => void) | null = null
    let timer: ReturnType<typeof setTimeout> | null = null

    const cleanup = () => {
      if (timer) {
        clearTimeout(timer)
        timer = null
      }
      stop?.()
      stop = null
    }

    stop = watch(agents, (list) => {
      const match = list.find(a => a.pid === pid)
      if (match) {
        selectAgent(match)
        cleanup()
      }
    })

    timer = setTimeout(cleanup, timeoutMs)
  }

  return {
    agents,
    pendingCapabilityDecisions,
    costTrend,
    filteredAgents,
    attentionAgents,
    attentionCount,
    selectedAgent,
    isLoading,
    error,
    searchQuery,
    selectAgent,
    dismissAgent,
    selectAgentWhenAvailable,
    startStream: sse.startStream,
  }
}
