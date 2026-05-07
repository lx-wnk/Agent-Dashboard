export interface TokenUsage {
  inputTokens: number
  outputTokens: number
  cacheCreationTokens: number
  cacheReadTokens: number
}

export interface SessionMeta {
  inputTokens: number
  outputTokens: number
  linesAdded: number
  linesRemoved: number
  filesModified: number
  gitCommits: number
  toolErrors: number
  usesMcp: boolean
  firstPrompt: string | null
}

export interface Agent {
  pid: number
  sessionId: string
  projectPath: string
  projectName: string
  cwd: string
  entrypoint: 'cli' | 'desktop' | 'unknown'
  status: 'active' | 'waiting' | 'idle'
  uptime: number
  lastActivity: string
  currentAction: string | null
  lastTools: string[]
  tasks: TaskInfo[]
  subagents: SubAgent[]
  tokenUsage: TokenUsage
  costEstimate: number
  model: string | null
  codeVersion: string | null
  conversationTurns: number
  toolCounts: Record<string, number>
  meta: SessionMeta | null
  channelAvailable: boolean
  lastOutput: string | null
  lastBtw: { message: string, response: string | null } | null
  machine?: string
  /** Set when this agent session is running as part of a pipeline task stage. */
  pipelineTaskId?: string
  pipelineTaskTitle?: string
}

export interface ChannelReply {
  message: string
  timestamp: string
}

export interface SubAgent {
  id: string
  type: string
  status: 'active' | 'completed'
  currentAction: string | null
  sessionFile: string
}

export interface TaskInfo {
  id: string
  subject: string
  status: 'pending' | 'in_progress' | 'completed'
}

export interface OutputMessage {
  role: 'assistant' | 'tool_call' | 'tool_result' | 'human' | 'channel_reply' | 'task' | 'subagent'
  content: string
  timestamp?: string
  toolName?: string
  filePath?: string
  taskStatus?: 'pending' | 'in_progress' | 'completed'
  taskId?: string
  subagentType?: string
  queued?: boolean
}

// Task Pipeline Types
export type PipelineStage
  = | 'concept'
    | 'backlog'
    | 'implementation'
    | 'self_review'
    | 'finalization'
    | 'done'
    | 'on_hold'
    | 'cancelled'

// `failed` is NOT a pipeline stage — it lives only on stage_run.status.
// When a stage run fails, the task stays on its current stage; the UI
// derives "needs user" from latestStageRunStatus === 'failed' and offers
// Retry / Analyze actions via the task modal.

export type StageRunStatus
  = | 'pending'
    | 'running'
    | 'awaiting_user'
    | 'on_hold'
    | 'done'
    | 'failed'

export type TaskPriority = 'high' | 'medium' | 'low'

export interface TaskDependency {
  id: string
  taskId: string
  taskTitle: string
  dependsOnId: string
  dependsOnTitle: string
  dependsOnStage: PipelineStage
  requiredStage: 'done' | 'cancelled'
  onCancelAction: 'cancel' | 'start' | 'on_hold'
  createdAt: string
}

export interface PipelineTask {
  id: string
  slug: string
  title: string
  description: string | null
  cwd: string
  worktreePath: string | null
  sourceBranch: string | null
  targetBranch: string | null
  currentStage: PipelineStage
  parentTaskId: string | null
  maxIterations: number
  tokenBudget: number | null
  costBudgetCents: number | null
  stageTimeoutSeconds: number
  createdAt: string
  updatedAt: string
  metadata: Record<string, unknown> | null
  /**
   * Jump-the-queue flag — silver-bullet tasks win the picker against all
   *  other ordering criteria. Set at creation, editable later.
   */
  silverBullet: boolean
  /** Soft priority used after silver-bullet and stage-furthest-first. */
  priority: TaskPriority
  // Owning user (multi-user mode). Null for legacy/system tasks created
  // before multi-user was introduced; only admins see those.
  userId: string | null
  // Computed at read time by the API — not stored in DB.
  // True when the latest stage_run is paused waiting for user input,
  // regardless of what currentStage is.
  needsUser?: boolean
  latestStageRunStatus?: StageRunStatus | null
  currentIteration?: number
  // Session id of the most relevant stage_run (running > most recent with session).
  // Used by the task modal to mount a live "follow along" pane against the
  // same JSONL transcript + channel the session is streaming to.
  activeSessionId?: string | null
  // PID of the stage_run when it is currently running. Null between runs.
  activePid?: number | null
  // True when this task is blocked by unfulfilled dependencies.
  isBlocked?: boolean
  // True when blocked AND every blocking prereq is terminal (done/cancelled)
  // but reached the wrong stage — dependency can never be satisfied.
  isUnsatisfiable?: boolean
}

export interface StageRun {
  id: string
  taskId: string
  stage: PipelineStage
  sessionId: string | null
  sessionName: string | null
  pid: number | null
  status: StageRunStatus
  startedAt: string | null
  endedAt: string | null
  iteration: number
  output: Record<string, unknown> | null
  tokensUsed: number
  costCents: number
}

export interface TaskPermission {
  id: string
  taskId: string
  tool: string
  pattern: string | null
  granted: boolean
  preApproved: boolean
  requestedAt: string
  decidedAt: string | null
  decidedBy: 'user' | 'auto' | null
  /** ISO timestamp; null = never expires. */
  expiresAt: string | null
}

export interface PermissionRequest {
  id: string
  stageRunId: string
  tool: string
  pattern: string | null
  reason: string | null
  requestedAt: string
  resolvedAt: string | null
  outcome: 'granted' | 'denied' | 'timeout' | null
}

export type FeedbackStage = PipelineStage

export interface TaskFeedback {
  id: string
  taskId: string
  stage: FeedbackStage
  stageRunId: string | null
  iteration: number
  feedback: string
  createdAt: string
  resolvedAt: string | null
  resolvedByStageRunId: string | null
}

export interface AuditEntry {
  id: string
  taskId: string
  actor: 'user' | 'agent' | 'orchestrator' | 'system'
  action: string
  timestamp: string
  details: Record<string, unknown> | null
}

export type NotificationEventType
  = | 'on_hold'
    | 'approval_needed'
    | 'completed'
    | 'failed'
    | 'budget_exceeded'
    | 'iteration_warning'

export type NotificationChannel = 'email' | 'webhook' | 'browser' | 'system'

export interface NotificationPreference {
  eventType: NotificationEventType
  channels: NotificationChannel[]
  enabled: boolean
}

// MCP API Key types
export type McpScope = 'tasks:read' | 'tasks:write' | 'pipeline:control' | 'keys:manage'

export interface ApiKey {
  id: string
  name: string
  // key_hash is intentionally absent — never send the hash over the wire
  scopes: McpScope[]
  active: boolean
  userId: string | null
  createdAt: string
  lastUsedAt: string | null
}
