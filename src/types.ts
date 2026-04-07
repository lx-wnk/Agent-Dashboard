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
