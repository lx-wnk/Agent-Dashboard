import type { PermissionRequest, PipelineStage, PipelineTask, StageRun, TaskPermission } from '../types'
import { computed, onUnmounted, ref } from 'vue'

const tasks = ref<PipelineTask[]>([])
const selectedTask = ref<PipelineTask | null>(null)
const isLoading = ref(true)
const error = ref<string | null>(null)

let eventSource: EventSource | null = null
let subscriberCount = 0

export interface TaskEvent {
  type: 'task_created' | 'task_updated' | 'task_deleted' | 'stage_run_updated' | 'permission_request'
  taskId: string
  payload?: unknown
}

async function fetchTasks() {
  try {
    const res = await fetch('/api/tasks')
    if (!res.ok)
      throw new Error(`HTTP ${res.status}`)
    tasks.value = await res.json() as PipelineTask[]
    isLoading.value = false
    error.value = null
  }
  catch (err) {
    error.value = (err as Error).message
    isLoading.value = false
  }
}

function startSSE() {
  if (eventSource)
    return
  eventSource = new EventSource('/api/tasks/stream')
  eventSource.onmessage = (e) => {
    try {
      const event: TaskEvent = JSON.parse(e.data)
      applyEvent(event)
    }
    catch {
      // ignore malformed messages
    }
  }
  eventSource.onerror = () => {
    // browser auto-reconnects; nothing to do
  }
}

function stopSSE() {
  if (eventSource) {
    eventSource.close()
    eventSource = null
  }
}

function applyEvent(event: TaskEvent) {
  switch (event.type) {
    case 'task_created': {
      const task = event.payload as PipelineTask
      if (task && !tasks.value.some(t => t.id === task.id))
        tasks.value = [task, ...tasks.value]
      break
    }
    case 'task_updated': {
      const task = event.payload as PipelineTask
      if (task) {
        tasks.value = tasks.value.map(t => t.id === task.id ? task : t)
        if (selectedTask.value?.id === task.id)
          selectedTask.value = task
      }
      break
    }
    case 'task_deleted': {
      tasks.value = tasks.value.filter(t => t.id !== event.taskId)
      if (selectedTask.value?.id === event.taskId)
        selectedTask.value = null
      break
    }
    case 'permission_request':
    case 'stage_run_updated':
      // Refetch the task to get fresh data
      void fetchTasks()
      break
  }
}

export interface CreateTaskInput {
  slug: string
  title: string
  description?: string
  cwd: string
  sourceBranch?: string
  targetBranch?: string
  useWorktree?: boolean
  maxIterations?: number
  tokenBudget?: number
  parentTaskId?: string
}

export async function createTask(input: CreateTaskInput): Promise<PipelineTask> {
  const res = await fetch('/api/tasks', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      slug: input.slug,
      title: input.title,
      description: input.description,
      cwd: input.cwd,
      sourceBranch: input.sourceBranch,
      targetBranch: input.targetBranch,
      useWorktree: input.useWorktree,
      maxIterations: input.maxIterations,
      tokenBudget: input.tokenBudget,
      parentTaskId: input.parentTaskId,
    }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || 'Failed to create task')
  }
  return await res.json() as PipelineTask
}

export async function progressTask(taskId: string): Promise<void> {
  const res = await fetch(`/api/tasks/${taskId}/progress`, { method: 'POST' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || 'Failed to progress task')
  }
}

export async function approveTask(taskId: string): Promise<void> {
  const res = await fetch(`/api/tasks/${taskId}/approve`, { method: 'POST' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || 'Failed to approve task')
  }
}

export async function cancelTask(taskId: string): Promise<void> {
  const res = await fetch(`/api/tasks/${taskId}/cancel`, { method: 'POST' })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || 'Failed to cancel task')
  }
}

export async function fetchStageRuns(taskId: string): Promise<StageRun[]> {
  const res = await fetch(`/api/tasks/${taskId}/stage-runs`)
  if (!res.ok)
    return []
  return await res.json() as StageRun[]
}

export async function fetchTaskPermissions(taskId: string): Promise<TaskPermission[]> {
  const res = await fetch(`/api/tasks/${taskId}/permissions`)
  if (!res.ok)
    return []
  return await res.json() as TaskPermission[]
}

export async function fetchPendingPermissionRequests(taskId: string): Promise<PermissionRequest[]> {
  const res = await fetch(`/api/tasks/${taskId}/permission-requests`)
  if (!res.ok)
    return []
  return await res.json() as PermissionRequest[]
}

export async function resolvePermissionRequest(id: string, outcome: 'granted' | 'denied'): Promise<void> {
  const res = await fetch(`/api/permission-requests/${id}/resolve`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ outcome }),
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: `HTTP ${res.status}` }))
    throw new Error(err.error || 'Failed to resolve')
  }
}

export function useTasks() {
  subscriberCount++
  if (subscriberCount === 1) {
    void fetchTasks()
    startSSE()
  }

  onUnmounted(() => {
    subscriberCount--
    if (subscriberCount === 0)
      stopSSE()
  })

  function selectTask(task: PipelineTask | null) {
    selectedTask.value = task
  }

  function tasksByStage(stage: PipelineStage): PipelineTask[] {
    return tasks.value.filter(t => t.currentStage === stage)
  }

  const tasksByStageMap = computed(() => {
    const map: Partial<Record<PipelineStage, PipelineTask[]>> = {}
    for (const task of tasks.value) {
      if (!map[task.currentStage])
        map[task.currentStage] = []
      map[task.currentStage]!.push(task)
    }
    return map
  })

  return {
    tasks,
    selectedTask,
    isLoading,
    error,
    tasksByStage,
    tasksByStageMap,
    selectTask,
    refetch: fetchTasks,
  }
}
