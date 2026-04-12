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
  = | 'backlog'
    | 'pruefung'
    | 'refinement'
    | 'planning'
    | 'approval1'
    | 'umsetzungskonzept'
    | 'approval2'
    | 'umsetzung'
    | 'selbstreview'
    | 'finalisierung'
    | 'done'
    | 'on_hold'
    | 'cancelled'
    | 'failed'

export type StageRunStatus
  = | 'pending'
    | 'running'
    | 'awaiting_user'
    | 'on_hold'
    | 'done'
    | 'failed'

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
