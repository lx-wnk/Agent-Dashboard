import type { Project } from '../types'
import { onUnmounted, ref, shallowRef } from 'vue'
import { SSE_RETRY_DELAY_MS } from '../utils/sse'

const projects = shallowRef<Project[]>([])
const isLoading = ref(true)
const error = ref<string | null>(null)

let eventSource: EventSource | null = null
let pollTimer: ReturnType<typeof setInterval> | null = null
let sseRetryTimer: ReturnType<typeof setTimeout> | null = null
let subscriberCount = 0

const FALLBACK_POLL_MS = 60_000

export interface ProjectEvent {
  type: 'project_created' | 'project_updated' | 'project_deleted'
  projectId: string
  payload?: unknown
}

async function fetchProjects(): Promise<void> {
  try {
    const res = await fetch('/api/projects')
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    projects.value = await res.json() as Project[]
    isLoading.value = false
    error.value = null
  }
  catch (err) {
    error.value = (err as Error).message
    isLoading.value = false
  }
}

function applyEvent(event: ProjectEvent): void {
  switch (event.type) {
    case 'project_created': {
      const project = event.payload as Project
      if (project && !projects.value.some(p => p.id === project.id))
        projects.value = [project, ...projects.value]
      break
    }
    case 'project_updated': {
      const project = event.payload as Project
      if (project)
        projects.value = projects.value.map(p => p.id === project.id ? project : p)
      break
    }
    case 'project_deleted': {
      projects.value = projects.value.filter(p => p.id !== event.projectId)
      break
    }
  }
}

function startSSE(): void {
  if (eventSource)
    return
  // NOTE: /api/projects/stream SSE endpoint pending — polling fallback active by design.
  eventSource = new EventSource('/api/projects/stream')
  eventSource.onmessage = (e) => {
    try {
      const event: ProjectEvent = JSON.parse(e.data)
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
    void fetchProjects()
  }, FALLBACK_POLL_MS)
}

function stopPolling(): void {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
}

export interface CreateProjectInput {
  slug: string
  name: string
  description?: string
  color?: string
  defaultSpawnerId?: string | null
}

export async function createProject(input: CreateProjectInput): Promise<Project> {
  const res = await fetch('/api/projects', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error((err as { error: string }).error || 'Failed to create project')
  }
  return res.json() as Promise<Project>
}

export async function updateProject(id: string, input: Partial<CreateProjectInput>): Promise<Project> {
  const res = await fetch(`/api/projects/${id}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error((err as { error: string }).error || 'Failed to update project')
  }
  return res.json() as Promise<Project>
}

export async function deleteProject(id: string): Promise<void> {
  const res = await fetch(`/api/projects/${id}`, { method: 'DELETE' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error((err as { error: string }).error || 'Failed to delete project')
  }
}

export async function fetchProject(id: string): Promise<Project> {
  const res = await fetch(`/api/projects/${id}`)
  if (!res.ok)
    throw new Error(`HTTP ${res.status}`)
  return res.json() as Promise<Project>
}

function startStream(): void {
  subscriberCount++
  if (subscriberCount === 1) {
    void fetchProjects()
    startSSE()
  }
}

export function useProjects(options?: { autoStart?: boolean }) {
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
    projects,
    isLoading,
    error,
    refetch: fetchProjects,
    startStream,
  }
}
