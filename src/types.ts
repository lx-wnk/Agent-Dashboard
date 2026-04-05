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
