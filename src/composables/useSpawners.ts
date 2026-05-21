import type { Spawner } from '../types'
import { onUnmounted, ref, shallowRef } from 'vue'
import { SSE_RETRY_DELAY_MS } from '../utils/sse'

const spawners = shallowRef<Spawner[]>([])
const isLoading = ref(true)
const error = ref<string | null>(null)

let eventSource: EventSource | null = null
let pollTimer: ReturnType<typeof setInterval> | null = null
let sseRetryTimer: ReturnType<typeof setTimeout> | null = null
let subscriberCount = 0

const FALLBACK_POLL_MS = 60_000

export interface SpawnerEvent {
  type: 'spawner_created' | 'spawner_updated' | 'spawner_deleted'
  spawnerId: string
  payload?: unknown
}

async function fetchSpawners(): Promise<void> {
  try {
    const res = await fetch('/api/spawners')
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    spawners.value = await res.json() as Spawner[]
    isLoading.value = false
    error.value = null
  }
  catch (err) {
    error.value = (err as Error).message
    isLoading.value = false
  }
}

function applyEvent(event: SpawnerEvent): void {
  switch (event.type) {
    case 'spawner_created': {
      const spawner = event.payload as Spawner
      if (spawner && !spawners.value.some(s => s.id === spawner.id))
        spawners.value = [spawner, ...spawners.value]
      break
    }
    case 'spawner_updated': {
      const spawner = event.payload as Spawner
      if (spawner)
        spawners.value = spawners.value.map(s => s.id === spawner.id ? spawner : s)
      break
    }
    case 'spawner_deleted': {
      spawners.value = spawners.value.filter(s => s.id !== event.spawnerId)
      break
    }
  }
}

function startSSE(): void {
  if (eventSource)
    return
  // NOTE: /api/spawners/stream SSE endpoint pending — polling fallback active by design.
  eventSource = new EventSource('/api/spawners/stream')
  eventSource.onmessage = (e) => {
    try {
      const event: SpawnerEvent = JSON.parse(e.data)
      applyEvent(event)
    }
    catch {
      // ignore malformed messages
    }
  }
  eventSource.onerror = () => {
    if (eventSource?.readyState === EventSource.CLOSED) {
      stopSSE()
      startPolling()
      sseRetryTimer = setTimeout(() => {
        stopPolling()
        startSSE()
      }, SSE_RETRY_DELAY_MS)
    }
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
  pollTimer = setInterval(() => {
    void fetchSpawners()
  }, FALLBACK_POLL_MS)
}

function stopPolling(): void {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

export interface CreateSpawnerInput {
  name: string
  slug: string
  command: string
  args?: string[]
  env?: Record<string, string>
  modelOverride?: string
  description?: string
}

export async function createSpawner(input: CreateSpawnerInput): Promise<Spawner> {
  const res = await fetch('/api/spawners', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error((err as { error: string }).error || 'Failed to create spawner')
  }
  return res.json() as Promise<Spawner>
}

export async function updateSpawner(id: string, input: Partial<CreateSpawnerInput>): Promise<Spawner> {
  const res = await fetch(`/api/spawners/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error((err as { error: string }).error || 'Failed to update spawner')
  }
  return res.json() as Promise<Spawner>
}

export async function deleteSpawner(id: string): Promise<void> {
  const res = await fetch(`/api/spawners/${id}`, { method: 'DELETE' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error((err as { error: string }).error || 'Failed to delete spawner')
  }
}

function startStream(): void {
  subscriberCount++
  if (subscriberCount === 1) {
    void fetchSpawners()
    startSSE()
  }
}

export function useSpawners(options?: { autoStart?: boolean }) {
  if (options?.autoStart !== false)
    startStream()

  onUnmounted(() => {
    subscriberCount--
    if (subscriberCount === 0) {
      stopSSE()
      stopPolling()
      if (sseRetryTimer) {
        clearTimeout(sseRetryTimer)
        sseRetryTimer = null
      }
    }
  })

  return {
    spawners,
    isLoading,
    error,
    refetch: fetchSpawners,
    startStream,
  }
}
