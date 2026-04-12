import type {
  AuditEntry,
  NotificationPreference,
  PermissionRequest,
  PipelineStage,
  PipelineTask,
  StageRun,
  StageRunStatus,
  TaskPermission,
} from '../../src/types.js'

// Row types reflect the SQLite schema exactly (snake_case, JSON as strings, bools as ints).

export interface TaskRow {
  id: string
  slug: string
  title: string
  description: string | null
  cwd: string
  worktree_path: string | null
  source_branch: string | null
  target_branch: string | null
  current_stage: string
  parent_task_id: string | null
  max_iterations: number
  token_budget: number | null
  cost_budget_cents: number | null
  stage_timeout_seconds: number
  created_at: string
  updated_at: string
  metadata: string | null
}

export interface StageRunRow {
  id: string
  task_id: string
  stage: string
  session_id: string | null
  session_name: string | null
  pid: number | null
  status: string
  started_at: string | null
  ended_at: string | null
  iteration: number
  output: string | null
  tokens_used: number
  cost_cents: number
}

export interface TaskPermissionRow {
  id: string
  task_id: string
  tool: string
  pattern: string | null
  granted: number
  pre_approved: number
  requested_at: string
  decided_at: string | null
  decided_by: string | null
}

export interface PermissionRequestRow {
  id: string
  stage_run_id: string
  tool: string
  pattern: string | null
  reason: string | null
  requested_at: string
  resolved_at: string | null
  outcome: string | null
}

export interface AuditRow {
  id: string
  task_id: string
  actor: string
  action: string
  timestamp: string
  details: string | null
}

export interface NotificationPreferenceRow {
  event_type: string
  channels: string
  enabled: number
}

function parseJson<T>(value: string | null): T | null {
  if (!value)
    return null
  try {
    return JSON.parse(value) as T
  }
  catch {
    return null
  }
}

export function rowToTask(row: TaskRow): PipelineTask {
  return {
    id: row.id,
    slug: row.slug,
    title: row.title,
    description: row.description,
    cwd: row.cwd,
    worktreePath: row.worktree_path,
    sourceBranch: row.source_branch,
    targetBranch: row.target_branch,
    currentStage: row.current_stage as PipelineStage,
    parentTaskId: row.parent_task_id,
    maxIterations: row.max_iterations,
    tokenBudget: row.token_budget,
    costBudgetCents: row.cost_budget_cents,
    stageTimeoutSeconds: row.stage_timeout_seconds,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
    metadata: parseJson<Record<string, unknown>>(row.metadata),
  }
}

export function rowToStageRun(row: StageRunRow): StageRun {
  return {
    id: row.id,
    taskId: row.task_id,
    stage: row.stage as PipelineStage,
    sessionId: row.session_id,
    sessionName: row.session_name,
    pid: row.pid,
    status: row.status as StageRunStatus,
    startedAt: row.started_at,
    endedAt: row.ended_at,
    iteration: row.iteration,
    output: parseJson<Record<string, unknown>>(row.output),
    tokensUsed: row.tokens_used,
    costCents: row.cost_cents,
  }
}

export function rowToTaskPermission(row: TaskPermissionRow): TaskPermission {
  return {
    id: row.id,
    taskId: row.task_id,
    tool: row.tool,
    pattern: row.pattern,
    granted: row.granted === 1,
    preApproved: row.pre_approved === 1,
    requestedAt: row.requested_at,
    decidedAt: row.decided_at,
    decidedBy: row.decided_by as 'user' | 'auto' | null,
  }
}

export function rowToPermissionRequest(row: PermissionRequestRow): PermissionRequest {
  return {
    id: row.id,
    stageRunId: row.stage_run_id,
    tool: row.tool,
    pattern: row.pattern,
    reason: row.reason,
    requestedAt: row.requested_at,
    resolvedAt: row.resolved_at,
    outcome: row.outcome as 'granted' | 'denied' | 'timeout' | null,
  }
}

export function rowToAuditEntry(row: AuditRow): AuditEntry {
  return {
    id: row.id,
    taskId: row.task_id,
    actor: row.actor as AuditEntry['actor'],
    action: row.action,
    timestamp: row.timestamp,
    details: parseJson<Record<string, unknown>>(row.details),
  }
}

export function rowToNotificationPreference(row: NotificationPreferenceRow): NotificationPreference {
  return {
    eventType: row.event_type as NotificationPreference['eventType'],
    channels: parseJson<NotificationPreference['channels']>(row.channels) || [],
    enabled: row.enabled === 1,
  }
}
